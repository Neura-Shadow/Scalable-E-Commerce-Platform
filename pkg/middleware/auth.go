package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"goshop/pkg/jtoken"
	"goshop/pkg/response"
)

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
		c.Set("userId", payload["id"])
		c.Set("role", payload["role"])
		c.Next()
	}
}
