package auth

import (
	"context"
	"errors"

	"nilswitt.dev/tileserve-go/internal/handler/auth/oidc"
)

// ApplicationOIDCConfig is tileserve-go's own configuration for an optional
// OpenID Connect login, translated into oidc.NewAuthenticator's arguments by
// NewOIDCAuthenticator.
type ApplicationOIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// NewOIDCAuthenticator builds an OIDC authenticator from config, or returns
// a nil authenticator (and nil error) if config is entirely empty, meaning
// OIDC login isn't configured. A config with only some fields set is
// rejected, since a partial configuration can never work.
func NewOIDCAuthenticator(ctx context.Context, config ApplicationOIDCConfig) (*oidc.Authenticator, error) {
	if config.IssuerURL == "" && config.ClientID == "" && config.ClientSecret == "" && config.RedirectURL == "" {
		return nil, nil
	}

	if config.IssuerURL == "" || config.ClientID == "" || config.ClientSecret == "" || config.RedirectURL == "" {
		return nil, errors.New("oidc-issuer-url, oidc-client-id, oidc-client-secret, and oidc-redirect-url must all be set together to enable OpenID Connect login")
	}

	return oidc.NewAuthenticator(ctx, config.IssuerURL, config.ClientID, config.ClientSecret, config.RedirectURL)
}
