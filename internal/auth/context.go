package auth

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

const (
	usernameContextKey contextKey = "username"
	apiKeyIDContextKey contextKey = "apiKeyID"
)

// UsernameFromContext returns the username stored in ctx by
// ContextWithUsername, or "" if none is present.
func UsernameFromContext(ctx context.Context) string {
	username, _ := ctx.Value(usernameContextKey).(string)
	return username
}

// APIKeyIDFromContext returns the API key ID stored in ctx by
// ContextWithAPIKeyID.
func APIKeyIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(apiKeyIDContextKey).(uuid.UUID)
	return id, ok
}

// ContextWithUsername returns a copy of ctx carrying username, retrievable
// via UsernameFromContext.
func ContextWithUsername(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, usernameContextKey, username)
}

// ContextWithAPIKeyID returns a copy of ctx carrying id, retrievable via
// APIKeyIDFromContext.
func ContextWithAPIKeyID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, apiKeyIDContextKey, id)
}
