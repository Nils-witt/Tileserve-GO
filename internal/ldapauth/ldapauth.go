// Package ldapauth authenticates a username/password pair against an LDAP
// (or Active Directory) directory: it binds a service account to search for
// the user's entry by username, then rebinds on the same connection as that
// entry's DN with the caller-supplied password to actually verify it. See
// NewAuthenticator's doc comment for the required configuration.
package ldapauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
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
	// InsecureSkipVerify disables verification of the server's TLS
	// certificate for both an "ldaps://" connection and a StartTLS upgrade —
	// e.g. for a self-signed or internal-CA certificate the host doesn't
	// trust. This leaves the connection vulnerable to a man-in-the-middle,
	// so it should only be used when the alternative (installing the CA
	// certificate) genuinely isn't available.
	InsecureSkipVerify bool
	// CACertFile is the path to a PEM-encoded CA certificate (or bundle)
	// used, instead of the host's system trust store, to verify the
	// server's TLS certificate for both an "ldaps://" connection and a
	// StartTLS upgrade — e.g. for a self-signed or internal-CA certificate
	// the host doesn't otherwise trust. Ignored when InsecureSkipVerify is
	// set. Leave empty to use the system trust store.
	CACertFile string
	// Debug logs each step of Authenticate (bind/search attempts, the
	// resolved DN, success/failure) via the standard logger, including the
	// caller-supplied username. Off by default since that's per-login-attempt
	// volume and identifies who tried to log in; enable it while
	// troubleshooting a directory connection or filter.
	Debug bool
}

// Authenticator authenticates username/password pairs against the directory
// described by a Config. Construct one with NewAuthenticator.
type Authenticator struct {
	cfg    Config
	caPool *x509.CertPool // nil unless Config.CACertFile is set
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

	var caPool *x509.CertPool

	if cfg.CACertFile != "" {
		pem, err := os.ReadFile(cfg.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("ldap: read ca-cert-file: %w", err)
		}

		caPool = x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ldap: ca-cert-file %q contains no valid PEM certificates", cfg.CACertFile)
		}
	}

	return &Authenticator{cfg: cfg, caPool: caPool}, nil
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

	a.debugf("ldapauth: authenticating %q against %s", username, a.cfg.URL)

	conn, err := a.dial()
	if err != nil {
		a.debugf("ldapauth: %q: connect to %s failed: %v", username, a.cfg.URL, err)
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
			a.debugf("ldapauth: %q: service bind as %s failed: %v", username, a.cfg.BindDN, err)
			return Identity{}, fmt.Errorf("%w: service bind: %w", ErrInvalidCredentials, err)
		}

		a.debugf("ldapauth: %q: service bind as %s ok", username, a.cfg.BindDN)
	} else {
		a.debugf("ldapauth: %q: searching anonymously (no bind-dn configured)", username)
	}

	filter := fmt.Sprintf(a.cfg.UserFilter, ldap.EscapeFilter(username))
	req := ldap.NewSearchRequest(
		a.cfg.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 0, false,
		filter,
		[]string{"cn", "mail"},
		nil,
	)

	result, err := conn.Search(req)
	if err != nil {
		a.debugf("ldapauth: %q: search base=%q filter=%q failed: %v", username, a.cfg.BaseDN, filter, err)
		return Identity{}, fmt.Errorf("%w: search: %w", ErrInvalidCredentials, err)
	}

	if len(result.Entries) != 1 {
		a.debugf("ldapauth: %q: search base=%q filter=%q matched %d entries, want 1", username, a.cfg.BaseDN, filter, len(result.Entries))
		return Identity{}, ErrInvalidCredentials
	}

	entry := result.Entries[0]
	a.debugf("ldapauth: %q: resolved to dn=%q, verifying password", username, entry.DN)

	// Rebind on the same connection as the resolved entry to actually verify
	// the password; this is the only step in the whole flow that proves the
	// caller knows it.
	if err := conn.Bind(entry.DN, password); err != nil {
		a.debugf("ldapauth: %q: password verify bind as dn=%q failed: %v", username, entry.DN, err)
		return Identity{}, ErrInvalidCredentials
	}

	a.debugf("ldapauth: %q: authenticated successfully as dn=%q", username, entry.DN)

	return Identity{
		DN:    entry.DN,
		CN:    entry.GetAttributeValue("cn"),
		Email: entry.GetAttributeValue("mail"),
	}, nil
}

// dial connects to the directory, applying dialTimeout as both the connect
// timeout and every subsequent operation's read/write deadline, and upgrades
// to TLS via StartTLS when configured. tlsConfig governs both an "ldaps://"
// connection and a StartTLS upgrade, so InsecureSkipVerify applies to
// whichever of the two is in play.
func (a *Authenticator) dial() (*ldap.Conn, error) {
	tlsConfig := &tls.Config{ServerName: a.tlsServerName(), MinVersion: tls.VersionTLS12, InsecureSkipVerify: a.cfg.InsecureSkipVerify, RootCAs: a.caPool} //nolint:gosec // opt-in via Config.InsecureSkipVerify

	conn, err := ldap.DialURL(a.cfg.URL, ldap.DialWithDialer(&net.Dialer{Timeout: dialTimeout}), ldap.DialWithTLSConfig(tlsConfig))
	if err != nil {
		return nil, err
	}

	conn.SetTimeout(dialTimeout)

	if a.cfg.StartTLS && strings.HasPrefix(strings.ToLower(a.cfg.URL), "ldap://") {
		if err := conn.StartTLS(tlsConfig); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}

	return conn, nil
}

// debugf logs via the standard logger when Config.Debug is set, a no-op
// otherwise.
func (a *Authenticator) debugf(format string, args ...any) {
	if !a.cfg.Debug {
		return
	}

	log.Printf(format, args...)
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
