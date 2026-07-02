package jtoken

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt"
	"github.com/quangdangfit/gocommon/logger"

	"goshop/pkg/config"
	"goshop/pkg/utils"
)

const (
	AccessTokenExpiredTime  = 5 * 60 * 60 // 5 hours
	RefreshTokenExpiredTime = 30 * 24 * 3600
	AccessTokenType         = "x-access"  // 5 minutes
	RefreshTokenType        = "x-refresh" // 30 days
	bearerPrefix            = "Bearer "
)

func GenerateAccessToken(payload map[string]interface{}) string {
	cfg := config.GetConfig()
	payload["type"] = AccessTokenType
	tokenContent := jwt.MapClaims{
		"payload": payload,
		"exp":     time.Now().Add(time.Second * AccessTokenExpiredTime).Unix(),
	}
	jwtToken := jwt.NewWithClaims(jwt.GetSigningMethod("HS256"), tokenContent)
	token, err := jwtToken.SignedString([]byte(cfg.AuthSecret))
	if err != nil {
		logger.Error("Failed to generate access token: ", err)
		return ""
	}

	return token
}

func GenerateRefreshToken(payload map[string]interface{}) string {
	cfg := config.GetConfig()
	payload["type"] = RefreshTokenType
	tokenContent := jwt.MapClaims{
		"payload": payload,
		"exp":     time.Now().Add(time.Second * RefreshTokenExpiredTime).Unix(),
	}
	jwtToken := jwt.NewWithClaims(jwt.GetSigningMethod("HS256"), tokenContent)
	token, err := jwtToken.SignedString([]byte(cfg.AuthSecret))
	if err != nil {
		logger.Error("Failed to generate refresh token: ", err)
		return ""
	}

	return token
}

func ValidateToken(jwtToken string) (map[string]interface{}, error) {
	cfg := config.GetConfig()
	cleanJWT := strings.TrimSpace(jwtToken)
	cleanJWT = strings.TrimPrefix(cleanJWT, bearerPrefix)
	tokenData := jwt.MapClaims{}
	parser := jwt.Parser{ValidMethods: []string{jwt.SigningMethodHS256.Alg()}}
	token, err := parser.ParseWithClaims(cleanJWT, tokenData, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected jwt signing method %q", token.Header["alg"])
		}
		return []byte(cfg.AuthSecret), nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, jwt.ErrInvalidKey
	}

	var data map[string]interface{}
	utils.Copy(&data, tokenData["payload"])

	return data, nil
}
