package middleware

import "context"

type authContextKey uint8

const (
	userIDContextKey authContextKey = iota
	tokenVersionContextKey
)

func ContextWithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey, userID)
}

func UserIDFromContext(ctx context.Context) string {
	userID, _ := ctx.Value(userIDContextKey).(string)
	return userID
}

func ContextWithTokenVersion(ctx context.Context, tokenVersion uint64) context.Context {
	return context.WithValue(ctx, tokenVersionContextKey, tokenVersion)
}

func TokenVersionFromContext(ctx context.Context) (uint64, bool) {
	tokenVersion, ok := ctx.Value(tokenVersionContextKey).(uint64)
	return tokenVersion, ok
}
