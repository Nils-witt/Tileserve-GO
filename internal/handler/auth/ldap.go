// Package auth adapts tileserve-go's own application configuration into the
// lower-level LDAP and OIDC authenticators (see internal/ldapauth and
// internal/handler/auth/oidc), and constructs them at startup.
package auth

import (
	"errors"

	"nilswitt.dev/tileserve-go/internal/ldapauth"
)

// ApplicationLDAPConfig is tileserve-go's own configuration for an optional
// LDAP login fallback, translated into ldapauth.Config by
// NewLDAPAuthenticator.
type ApplicationLDAPConfig struct {
	URL                string
	BindDN             string
	BindPassword       string
	BaseDN             string
	UserFilter         string
	StartTLS           bool
	InsecureSkipVerify bool
	CACertFile         string
	Debug              bool
}

// NewLDAPAuthenticator builds an LDAP authenticator from config, or returns
// a nil authenticator (and nil error) if config.URL is empty, meaning LDAP
// login isn't configured.
func NewLDAPAuthenticator(config ApplicationLDAPConfig) (*ldapauth.Authenticator, error) {
	if config.URL == "" {
		return nil, nil
	}

	if config.BaseDN == "" {
		return nil, errors.New("ldap-base-dn is required when ldap-url is set")
	}

	return ldapauth.NewAuthenticator(ldapauth.Config{
		URL:                config.URL,
		BindDN:             config.BindDN,
		BindPassword:       config.BindPassword,
		BaseDN:             config.BaseDN,
		UserFilter:         config.UserFilter,
		StartTLS:           config.StartTLS,
		InsecureSkipVerify: config.InsecureSkipVerify,
		CACertFile:         config.CACertFile,
		Debug:              config.Debug,
	})
}
