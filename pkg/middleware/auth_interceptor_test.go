package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"goshop/pkg/config"
	"goshop/pkg/jtoken"
	userPB "goshop/proto/gen/go/user"
)

const (
	protectedMethod = userPB.UserService_GetMe_FullMethodName
	refreshMethod   = userPB.UserService_RefreshToken_FullMethodName
)

func TestAuthInterceptorAllowsAccessTokenOnProtectedMethod(t *testing.T) {
	withInterceptorAuthSecret(t)
	interceptor := NewAuthInterceptor(nil, WithRefreshTokenMethods([]string{refreshMethod}))
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": "user-1"})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("token", token))

	result, err := interceptor.Unary()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: protectedMethod}, userIDHandler)

	require.NoError(t, err)
	assert.Equal(t, "user-1", result)
}

func TestAuthInterceptorAllowsRefreshTokenOnlyOnRefreshMethod(t *testing.T) {
	withInterceptorAuthSecret(t)
	interceptor := NewAuthInterceptor(nil, WithRefreshTokenMethods([]string{refreshMethod}))
	refreshToken := jtoken.GenerateRefreshToken(map[string]interface{}{"id": "user-1"})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("token", refreshToken))

	result, err := interceptor.Unary()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: refreshMethod}, userIDHandler)

	require.NoError(t, err)
	assert.Equal(t, "user-1", result)
}

func TestAuthInterceptorPropagatesTokenVersion(t *testing.T) {
	withInterceptorAuthSecret(t)
	interceptor := NewAuthInterceptor(nil, WithRefreshTokenMethods([]string{refreshMethod}))
	token := jtoken.GenerateRefreshToken(map[string]interface{}{
		"id":                     "user-1",
		jtoken.TokenVersionClaim: uint64(9),
	})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("token", token))

	result, err := interceptor.Unary()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: refreshMethod}, tokenVersionHandler)

	require.NoError(t, err)
	assert.Equal(t, uint64(9), result)
}

func TestAuthInterceptorRejectsAccessTokenOnRefreshMethod(t *testing.T) {
	withInterceptorAuthSecret(t)
	interceptor := NewAuthInterceptor(nil, WithRefreshTokenMethods([]string{refreshMethod}))
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": "user-1"})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("token", token))

	_, err := interceptor.Unary()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: refreshMethod}, userIDHandler)

	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptorRejectsRefreshTokenOnProtectedMethod(t *testing.T) {
	withInterceptorAuthSecret(t)
	interceptor := NewAuthInterceptor(nil, WithRefreshTokenMethods([]string{refreshMethod}))
	token := jtoken.GenerateRefreshToken(map[string]interface{}{"id": "user-1"})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("token", token))

	_, err := interceptor.Unary()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: protectedMethod}, userIDHandler)

	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptorRejectsMalformedIdentityClaimWithoutPanicking(t *testing.T) {
	secret := withInterceptorAuthSecret(t)
	interceptor := NewAuthInterceptor(nil)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"payload": map[string]interface{}{
			"id":   123,
			"type": jtoken.AccessTokenType,
		},
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(secret))
	require.NoError(t, err)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("token", signed))

	assert.NotPanics(t, func() {
		_, err = interceptor.Unary()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: protectedMethod}, userIDHandler)
	})
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptorPreservesUnauthenticatedStatus(t *testing.T) {
	interceptor := NewAuthInterceptor(nil)

	_, err := interceptor.Unary()(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: protectedMethod}, userIDHandler)

	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func userIDHandler(ctx context.Context, _ interface{}) (interface{}, error) {
	return UserIDFromContext(ctx), nil
}

func tokenVersionHandler(ctx context.Context, _ interface{}) (interface{}, error) {
	tokenVersion, _ := TokenVersionFromContext(ctx)
	return tokenVersion, nil
}

func withInterceptorAuthSecret(t *testing.T) string {
	t.Helper()
	const secret = "interceptor-test-secret"
	cfg := config.GetConfig()
	previous := cfg.AuthSecret
	cfg.AuthSecret = secret
	t.Cleanup(func() { cfg.AuthSecret = previous })
	return secret
}
