package ldapauth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewAuthenticator(t *testing.T) {
	t.Parallel()

	validCfg := Config{
		URL:        "ldap://ldap.example.com:389",
		BaseDN:     "ou=people,dc=example,dc=com",
		UserFilter: "(uid=%s)",
	}

	tests := []struct {
		name    string
		mutate  func(cfg Config) Config
		wantErr bool
	}{
		{name: "valid config", mutate: func(cfg Config) Config { return cfg }, wantErr: false},
		{name: "missing url", mutate: func(cfg Config) Config { cfg.URL = ""; return cfg }, wantErr: true},
		{name: "missing base dn", mutate: func(cfg Config) Config { cfg.BaseDN = ""; return cfg }, wantErr: true},
		{name: "missing user filter", mutate: func(cfg Config) Config { cfg.UserFilter = ""; return cfg }, wantErr: true},
		{name: "user filter without placeholder", mutate: func(cfg Config) Config { cfg.UserFilter = "(uid=admin)"; return cfg }, wantErr: true},
		{
			name: "bind dn/password optional",
			mutate: func(cfg Config) Config {
				cfg.BindDN = "cn=svc,dc=example,dc=com"
				cfg.BindPassword = "s3cret"

				return cfg
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewAuthenticator(tc.mutate(validCfg))
			if (err != nil) != tc.wantErr {
				t.Errorf("NewAuthenticator() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestAuthenticateRejectsEmptyCredentials(t *testing.T) {
	t.Parallel()

	auth, err := NewAuthenticator(Config{
		URL:        "ldap://ldap.example.invalid:389",
		BaseDN:     "ou=people,dc=example,dc=com",
		UserFilter: "(uid=%s)",
	})
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	tests := []struct {
		name     string
		username string
		password string
	}{
		{name: "empty username", username: "", password: "s3cret"},
		{name: "empty password", username: "alice", password: ""},
		{name: "both empty", username: "", password: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			_, err := auth.Authenticate(ctx, tc.username, tc.password)
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Errorf("Authenticate() error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}
