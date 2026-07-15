package jtoken

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/quangdangfit/gocommon/logger"

	"goshop/pkg/config"
)

const (
	AccessTokenExpiredTime  = 5 * 60
	RefreshTokenExpiredTime = 30 * 24 * 3600
	AccessTokenType         = "x-access"  // 5 minutes
	RefreshTokenType        = "x-refresh" // 30 days
	TokenVersionClaim       = "token_version"
	bearerPrefix            = "Bearer "
)

func GenerateAccessToken(payload map[string]interface{}) string {
	return generateToken(payload, AccessTokenType, AccessTokenExpiredTime)
}

func GenerateRefreshToken(payload map[string]interface{}) string {
	return generateToken(payload, RefreshTokenType, RefreshTokenExpiredTime)
}

func generateToken(payload map[string]interface{}, tokenType string, lifetimeSeconds int) string {
	cfg := config.GetConfig()
	tokenPayload := make(map[string]interface{}, len(payload)+2)
	for key, value := range payload {
		tokenPayload[key] = value
	}
	if _, ok := tokenPayload[TokenVersionClaim]; !ok {
		tokenPayload[TokenVersionClaim] = uint64(0)
	}
	tokenPayload["type"] = tokenType
	issuedAt := time.Now()
	tokenContent := jwt.MapClaims{
		"payload": tokenPayload,
		"iat":     issuedAt.Unix(),
		"exp":     issuedAt.Add(time.Second * time.Duration(lifetimeSeconds)).Unix(),
	}
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenContent)
	token, err := jwtToken.SignedString([]byte(cfg.AuthSecret))
	if err != nil {
		logger.Error("Failed to generate token: ", err)
		return ""
	}

	return token
}

func ValidateToken(jwtToken string) (map[string]interface{}, error) {
	cfg := config.GetConfig()
	cleanJWT := strings.TrimSpace(jwtToken)
	cleanJWT = strings.TrimPrefix(cleanJWT, bearerPrefix)
	tokenData := jwt.MapClaims{}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
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
		return nil, jwt.ErrTokenInvalidClaims
	}

	payload, ok := tokenData["payload"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid jwt payload")
	}
	return payload, nil
}

func TokenVersion(payload map[string]interface{}) (uint64, error) {
	value, ok := payload[TokenVersionClaim]
	if !ok {
		return 0, fmt.Errorf("missing %s claim", TokenVersionClaim)
	}

	switch typed := value.(type) {
	case uint64:
		return typed, nil
	case uint:
		return uint64(typed), nil
	case int:
		if typed < 0 {
			return 0, fmt.Errorf("invalid %s claim", TokenVersionClaim)
		}
		return uint64(typed), nil
	case float64:
		if typed < 0 || typed >= float64(uint64(1)<<63) || math.Trunc(typed) != typed {
			return 0, fmt.Errorf("invalid %s claim", TokenVersionClaim)
		}
		return uint64(typed), nil
	default:
		return 0, fmt.Errorf("invalid %s claim", TokenVersionClaim)
	}
}
