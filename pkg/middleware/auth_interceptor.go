package middleware

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"goshop/pkg/jtoken"
)

type AuthInterceptor struct {
	ignoredMethods      map[string]struct{}
	refreshTokenMethods map[string]struct{}
}

type AuthInterceptorOption func(*AuthInterceptor)

func WithRefreshTokenMethods(methods []string) AuthInterceptorOption {
	return func(interceptor *AuthInterceptor) {
		for _, method := range methods {
			interceptor.refreshTokenMethods[method] = struct{}{}
		}
	}
}

func NewAuthInterceptor(ignoredMethods []string, options ...AuthInterceptorOption) *AuthInterceptor {
	interceptor := &AuthInterceptor{
		ignoredMethods:      make(map[string]struct{}, len(ignoredMethods)),
		refreshTokenMethods: make(map[string]struct{}),
	}
	for _, method := range ignoredMethods {
		interceptor.ignoredMethods[method] = struct{}{}
	}
	for _, option := range options {
		option(interceptor)
	}
	return interceptor
}

func (ai *AuthInterceptor) Unary() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		if _, ignored := ai.ignoredMethods[info.FullMethod]; ignored {
			return handler(ctx, req)
		}

		userID, tokenVersion, err := ai.authorize(ctx, ai.expectedTokenType(info.FullMethod))
		if err != nil {
			return nil, err
		}

		ctx = ContextWithUserID(ctx, userID)
		ctx = ContextWithTokenVersion(ctx, tokenVersion)

		return handler(ctx, req)
	}
}

func (ai *AuthInterceptor) expectedTokenType(method string) string {
	if _, ok := ai.refreshTokenMethods[method]; ok {
		return jtoken.RefreshTokenType
	}
	return jtoken.AccessTokenType
}

func (ai *AuthInterceptor) authorize(ctx context.Context, expectedTokenType string) (string, uint64, error) {
	m, ok := metadata.FromIncomingContext(ctx)
	if !ok || len(m["token"]) == 0 {
		return "", 0, status.Error(codes.Unauthenticated, "missing token")
	}

	payload, err := jtoken.ValidateToken(m["token"][0])
	if err != nil {
		return "", 0, status.Error(codes.Unauthenticated, "unauthorized")
	}
	tokenType, ok := payload["type"].(string)
	if !ok || tokenType != expectedTokenType {
		return "", 0, status.Error(codes.Unauthenticated, "unauthorized")
	}
	userID, ok := payload["id"].(string)
	if !ok || strings.TrimSpace(userID) == "" {
		return "", 0, status.Error(codes.Unauthenticated, "unauthorized")
	}
	tokenVersion, err := jtoken.TokenVersion(payload)
	if err != nil {
		return "", 0, status.Error(codes.Unauthenticated, "unauthorized")
	}
	return userID, tokenVersion, nil
}
