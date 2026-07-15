package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goshop/pkg/config"
	"goshop/pkg/jtoken"
)

func TestJWTRefreshPropagatesTypedIdentityAndTokenVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.GetConfig()
	previous := cfg.AuthSecret
	cfg.AuthSecret = "http-middleware-test-secret"
	t.Cleanup(func() { cfg.AuthSecret = previous })

	router := gin.New()
	router.POST("/refresh", JWTRefresh(), func(c *gin.Context) {
		tokenVersion, ok := TokenVersionFromGinContext(c)
		require.True(t, ok)
		c.JSON(http.StatusOK, gin.H{"user_id": c.GetString("userId"), "token_version": tokenVersion})
	})
	token := jtoken.GenerateRefreshToken(map[string]interface{}{
		"id":                     "user-1",
		jtoken.TokenVersionClaim: uint64(11),
	})
	request := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{"token_version":11,"user_id":"user-1"}`, response.Body.String())
}

func TestJWTRejectsMalformedIdentityClaim(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.GetConfig()
	previous := cfg.AuthSecret
	cfg.AuthSecret = "http-middleware-test-secret"
	t.Cleanup(func() { cfg.AuthSecret = previous })

	router := gin.New()
	router.GET("/protected", JWTAuth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	token := jtoken.GenerateAccessToken(map[string]interface{}{"id": 123})
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", token)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusUnauthorized, response.Code)
}
