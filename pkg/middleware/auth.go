package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"goshop/pkg/jtoken"
	"goshop/pkg/response"
)

const tokenVersionGinKey = "tokenVersion"

func JWTAuth() gin.HandlerFunc {
	return JWT(jtoken.AccessTokenType)
}

func JWTRefresh() gin.HandlerFunc {
	return JWT(jtoken.RefreshTokenType)
}

func RequireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString("role") != role {
			response.Error(c, http.StatusForbidden, errors.New("permission denied"), "Permission denied")
			c.Abort()
			return
		}

		c.Next()
	}
}

func JWT(tokenType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token == "" {
			c.JSON(http.StatusUnauthorized, nil)
			c.Abort()
			return
		}

		payload, err := jtoken.ValidateToken(token)
		if err != nil || payload == nil || payload["type"] != tokenType {
			c.JSON(http.StatusUnauthorized, nil)
			c.Abort()
			return
		}
		userID, ok := payload["id"].(string)
		if !ok || strings.TrimSpace(userID) == "" {
			c.JSON(http.StatusUnauthorized, nil)
			c.Abort()
			return
		}
		tokenVersion, err := jtoken.TokenVersion(payload)
		if err != nil {
			c.JSON(http.StatusUnauthorized, nil)
			c.Abort()
			return
		}
		c.Set("userId", userID)
		c.Set("role", payload["role"])
		c.Set(tokenVersionGinKey, tokenVersion)
		c.Next()
	}
}

func TokenVersionFromGinContext(c *gin.Context) (uint64, bool) {
	tokenVersion, ok := c.Get(tokenVersionGinKey)
	if !ok {
		return 0, false
	}
	value, ok := tokenVersion.(uint64)
	return value, ok
}
