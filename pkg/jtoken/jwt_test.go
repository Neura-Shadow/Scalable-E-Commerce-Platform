package jtoken

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goshop/pkg/config"
)

func TestValidateTokenAcceptsRawAndBearerTokens(t *testing.T) {
	withAuthSecret(t, "test-secret")
	source := map[string]interface{}{
		"id":   "user-1",
		"role": "admin",
	}
	token := GenerateAccessToken(source)

	rawPayload, err := ValidateToken(token)
	require.NoError(t, err)
	bearerPayload, err := ValidateToken("Bearer " + token)
	require.NoError(t, err)

	assert.Equal(t, "user-1", rawPayload["id"])
	assert.Equal(t, "admin", rawPayload["role"])
	assert.Equal(t, AccessTokenType, rawPayload["type"])
	assert.Equal(t, uint64(0), mustTokenVersion(t, rawPayload))
	assert.Equal(t, rawPayload, bearerPayload)
	assert.NotContains(t, source, "type")
	assert.NotContains(t, source, TokenVersionClaim)
}

func TestValidateTokenRejectsUnexpectedSigningMethod(t *testing.T) {
	secret := "test-secret"
	withAuthSecret(t, secret)
	token := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.MapClaims{
		"payload": map[string]interface{}{
			"id":   "user-1",
			"type": AccessTokenType,
		},
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	payload, err := ValidateToken(signed)

	assert.Nil(t, payload)
	assert.Error(t, err)
}

func TestAccessTokenLifetimeIsFiveMinutes(t *testing.T) {
	assert.Equal(t, 5*60, AccessTokenExpiredTime)
}

func TestTokenVersionRejectsFractionalOrNegativeClaims(t *testing.T) {
	_, err := TokenVersion(map[string]interface{}{TokenVersionClaim: 1.5})
	assert.Error(t, err)
	_, err = TokenVersion(map[string]interface{}{TokenVersionClaim: -1.0})
	assert.Error(t, err)
}

func mustTokenVersion(t *testing.T, payload map[string]interface{}) uint64 {
	t.Helper()
	version, err := TokenVersion(payload)
	require.NoError(t, err)
	return version
}

func withAuthSecret(t *testing.T, secret string) {
	t.Helper()
	cfg := config.GetConfig()
	previous := cfg.AuthSecret
	cfg.AuthSecret = secret
	t.Cleanup(func() {
		cfg.AuthSecret = previous
	})
}
