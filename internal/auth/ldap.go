package auth

import (
	"errors"

	"nilswitt.dev/tileserve-go/internal/auth/ldap"
)

// ApplicationLDAPConfig is tileserve-go's own configuration for an optional
// LDAP login fallback, translated into ldap.Config by NewLDAPAuthenticator.
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

// NewLDAPAuthenticator adapts tileserve-go's own application configuration
// into the lower-level LDAP authenticator (see internal/auth/ldap),
// building it from config, or returning a nil authenticator (and nil error)
// if config.URL is empty, meaning LDAP login isn't configured.
func NewLDAPAuthenticator(config ApplicationLDAPConfig) (*ldap.Authenticator, error) {
	if config.URL == "" {
		return nil, nil
	}

	if config.BaseDN == "" {
		return nil, errors.New("ldap-base-dn is required when ldap-url is set")
	}

	return ldap.NewAuthenticator(ldap.Config{
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
