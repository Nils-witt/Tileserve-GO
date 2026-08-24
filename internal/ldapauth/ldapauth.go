// Package ldapauth authenticates a username/password pair against an LDAP
// (or Active Directory) directory: it binds a service account to search for
// the user's entry by username, then rebinds on the same connection as that
// entry's DN with the caller-supplied password to actually verify it. See
// NewAuthenticator's doc comment for the required configuration.
package ldapauth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// ErrInvalidCredentials is returned by Authenticate for an unknown username,
// a username/filter combination matching zero or more than one directory
// entry, or a wrong password — deliberately indistinguishable from each
// other to an unauthenticated caller, same as store.ErrInvalidCredentials
// for a local account.
var ErrInvalidCredentials = errors.New("ldap: invalid credentials")

// dialTimeout bounds how long connecting to, and every subsequent operation
// against, the LDAP server may take, so a slow or unreachable directory
// can't hang a login request indefinitely.
const dialTimeout = 10 * time.Second

// Config holds the settings needed to authenticate against one LDAP
// directory. URL, BaseDN, and UserFilter are required; BindDN/BindPassword
// may both be left empty to search anonymously (some directories allow this
// for read-only lookups, though most require a service account).
type Config struct {
	// URL is the server to connect to, e.g. "ldaps://ldap.example.com:636"
	// or "ldap://ldap.example.com:389".
	URL string
	// BindDN and BindPassword authenticate the search for the user's entry
	// below. Leave both empty to search anonymously.
	BindDN       string
	BindPassword string
	// BaseDN is the search base, e.g. "ou=people,dc=example,dc=com".
	BaseDN string
	// UserFilter is an LDAP filter with exactly one "%s" placeholder for the
	// (escaped) username, e.g. "(uid=%s)" or "(sAMAccountName=%s)".
	UserFilter string
	// StartTLS upgrades a plain "ldap://" connection with StartTLS before
	// binding. Ignored for an "ldaps://" URL, which is already encrypted.
	StartTLS bool
}

// Authenticator authenticates username/password pairs against the directory
// described by a Config. Construct one with NewAuthenticator.
type Authenticator struct {
	cfg Config
}

// NewAuthenticator validates cfg and returns an Authenticator for it.
func NewAuthenticator(cfg Config) (*Authenticator, error) {
	if cfg.URL == "" || cfg.BaseDN == "" || cfg.UserFilter == "" {
		return nil, errors.New("ldap: url, base-dn, and user-filter are all required")
	}

	if !strings.Contains(cfg.UserFilter, "%s") {
		return nil, errors.New(`ldap: user-filter must contain a "%s" placeholder for the username`)
	}

	if _, err := url.Parse(cfg.URL); err != nil {
		return nil, fmt.Errorf("ldap: invalid url: %w", err)
	}

	return &Authenticator{cfg: cfg}, nil
}

// Identity is the directory entry an Authenticate call resolved the caller
// to.
type Identity struct {
	// DN is the entry's distinguished name — the stable identifier a local
	// account gets linked to (see store.FindUserByLDAPIdentity/
	// CreateLDAPUser), since a directory's username/cn attributes can be
	// reassigned across entries over time but its DN is not.
	DN    string
	CN    string
	Email string
}

// Authenticate verifies username/password against the directory: it binds
// the configured service account (if any) and searches BaseDN for the one
// entry matching UserFilter, then rebinds on the same connection as that
// entry's DN with password to confirm it. Every failure — connection error,
// service bind failure, no/multiple matching entries, wrong password — is
// reported as ErrInvalidCredentials (wrapped, for connection/service-bind
// failures, so a caller inspecting logs can still tell a misconfigured
// server apart from an actual bad login), since none of those should be
// distinguishable to an unauthenticated caller.
func (a *Authenticator) Authenticate(ctx context.Context, username, password string) (Identity, error) {
	// An empty password is an LDAP "unauthenticated bind" — many servers
	// treat it as trivially successful regardless of whether username
	// exists, so it must be rejected before ever reaching Bind.
	if username == "" || password == "" {
		return Identity{}, ErrInvalidCredentials
	}

	conn, err := a.dial()
	if err != nil {
		return Identity{}, fmt.Errorf("%w: connect: %w", ErrInvalidCredentials, err)
	}
	defer func() { _ = conn.Close() }()

	// go-ldap's Bind/Search calls block on the underlying connection with no
	// context support of their own; closing the connection out from under
	// them is what makes ctx's cancellation/deadline actually abort an
	// in-flight call, rather than relying on dialTimeout's read deadline
	// alone.
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	if a.cfg.BindDN != "" {
		if err := conn.Bind(a.cfg.BindDN, a.cfg.BindPassword); err != nil {
			return Identity{}, fmt.Errorf("%w: service bind: %w", ErrInvalidCredentials, err)
		}
	}

	req := ldap.NewSearchRequest(
		a.cfg.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 0, false,
		fmt.Sprintf(a.cfg.UserFilter, ldap.EscapeFilter(username)),
		[]string{"cn", "mail"},
		nil,
	)

	result, err := conn.Search(req)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: search: %w", ErrInvalidCredentials, err)
	}

	if len(result.Entries) != 1 {
		return Identity{}, ErrInvalidCredentials
	}

	entry := result.Entries[0]

	// Rebind on the same connection as the resolved entry to actually verify
	// the password; this is the only step in the whole flow that proves the
	// caller knows it.
	if err := conn.Bind(entry.DN, password); err != nil {
		return Identity{}, ErrInvalidCredentials
	}

	return Identity{
		DN:    entry.DN,
		CN:    entry.GetAttributeValue("cn"),
		Email: entry.GetAttributeValue("mail"),
	}, nil
}

// dial connects to the directory, applying dialTimeout as both the connect
// timeout and every subsequent operation's read/write deadline, and upgrades
// to TLS via StartTLS when configured.
func (a *Authenticator) dial() (*ldap.Conn, error) {
	conn, err := ldap.DialURL(a.cfg.URL, ldap.DialWithDialer(&net.Dialer{Timeout: dialTimeout}))
	if err != nil {
		return nil, err
	}

	conn.SetTimeout(dialTimeout)

	if a.cfg.StartTLS && strings.HasPrefix(strings.ToLower(a.cfg.URL), "ldap://") {
		if err := conn.StartTLS(&tls.Config{ServerName: a.tlsServerName(), MinVersion: tls.VersionTLS12}); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}

	return conn, nil
}

// tlsServerName returns the hostname StartTLS should verify the server's
// certificate against, derived from Config.URL.
func (a *Authenticator) tlsServerName() string {
	u, err := url.Parse(a.cfg.URL)
	if err != nil {
		return ""
	}

	return u.Hostname()
}
