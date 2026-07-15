package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"goshop/internal/user/dto"
	"goshop/internal/user/model"
	"goshop/pkg/jtoken"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Login
// =================================================================================================

func TestUserAPI_LoginSuccess(t *testing.T) {
	dbTest.Create(context.Background(), &model.User{
		Email:    "login@test.com",
		Password: "test123456",
	})

	user := &dto.LoginReq{
		Email:    "login@test.com",
		Password: "test123456",
	}
	writer := makeRequest("POST", "/api/v1/auth/login", user, "")
	assert.Equal(t, http.StatusOK, writer.Code)
}

func TestUserAPI_LoginInvalidFieldType(t *testing.T) {
	user := map[string]interface{}{
		"email":    1,
		"password": "test123456",
	}
	writer := makeRequest("POST", "/api/v1/auth/login", user, "")
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusBadRequest, writer.Code)
	assert.Equal(t, "Invalid parameters", response["error"]["message"])
}

func TestUserAPI_LoginInvalidEmailFormat(t *testing.T) {
	user := &dto.LoginReq{
		Email:    "invalid",
		Password: "test123456",
	}
	writer := makeRequest("POST", "/api/v1/auth/login", user, "")
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestUserAPI_LoginInvalidPassword(t *testing.T) {
	user := &dto.LoginReq{
		Email:    "test@test.com",
		Password: "test",
	}
	writer := makeRequest("POST", "/api/v1/auth/login", user, "")
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestUserAPI_LoginUserNotFound(t *testing.T) {
	user := &dto.LoginReq{
		Email:    "notfound@test.com",
		Password: "test123456",
	}
	writer := makeRequest("POST", "/api/v1/auth/login", user, "")
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestUserAPI_LoginUserWrongPassword(t *testing.T) {
	user := &dto.LoginReq{
		Email:    "test@test.com",
		Password: "test1234567",
	}
	writer := makeRequest("POST", "/api/v1/auth/login", user, "")
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

// Register
// =================================================================================================

func TestUserAPI_RegisterSuccess(t *testing.T) {
	user := &dto.RegisterReq{
		Email:    "register@test.com",
		Password: "test123456",
	}
	register := makeRequest("POST", "/api/v1/auth/register", user, "")
	require.Equal(t, http.StatusOK, register.Code)
	var registered struct {
		Result dto.RegisterRes `json:"result"`
	}
	require.NoError(t, json.Unmarshal(register.Body.Bytes(), &registered))
	defer cleanData(&model.User{ID: registered.Result.User.ID})

	login := makeRequest("POST", "/api/v1/auth/login", &dto.LoginReq{
		Email:    user.Email,
		Password: user.Password,
	}, "")
	require.Equal(t, http.StatusOK, login.Code)

	var response struct {
		Result dto.LoginRes `json:"result"`
	}
	require.NoError(t, json.Unmarshal(login.Body.Bytes(), &response))
	assert.Equal(t, user.Email, response.Result.User.Email)
	assert.NotEmpty(t, response.Result.AccessToken)
	assert.NotEmpty(t, response.Result.RefreshToken)
}

func TestUserAPI_RegisterInvalidFieldType(t *testing.T) {
	user := map[string]interface{}{
		"email":    1,
		"password": "test123456",
	}
	writer := makeRequest("POST", "/api/v1/auth/register", user, "")
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusBadRequest, writer.Code)
	assert.Equal(t, "Invalid parameters", response["error"]["message"])
}

func TestUserAPI_RegisterInvalidEmail(t *testing.T) {
	user := map[string]interface{}{
		"email":    "invalid",
		"password": "test123456",
	}
	writer := makeRequest("POST", "/api/v1/auth/register", user, "")
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestUserAPI_RegisterInvalidPassword(t *testing.T) {
	user := map[string]interface{}{
		"email":    "register@test.com",
		"password": "test",
	}
	writer := makeRequest("POST", "/api/v1/auth/register", user, "")
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestUserAPI_RegisterEmailExist(t *testing.T) {
	defer cleanData()

	dbTest.Create(context.Background(), &model.User{
		Email:    "emailexist@test.com",
		Password: "password",
	})

	user := map[string]interface{}{
		"email":    "emailexist@test.com",
		"password": "test123456",
	}
	writer := makeRequest("POST", "/api/v1/auth/register", user, "")
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

// GetMe
// =================================================================================================

func TestUserAPI_GetMeSuccess(t *testing.T) {
	writer := makeRequest("GET", "/api/v1/auth/me", nil, accessToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Equal(t, "test@test.com", response["result"]["email"])
	assert.Equal(t, "", response["result"]["password"])
}

func TestUserAPI_GetMeUnauthorized(t *testing.T) {
	writer := makeRequest("GET", "/api/v1/auth/me", nil, "")
	assert.Equal(t, http.StatusUnauthorized, writer.Code)
}

func TestUserAPI_GetMeUserNotFound(t *testing.T) {
	token := jtoken.GenerateAccessToken(map[string]interface{}{
		"id": "user-not-found",
	})

	writer := makeRequest("GET", "/api/v1/auth/me", nil, token)
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestUserAPI_GetMeInvalidTokenType(t *testing.T) {
	writer := makeRequest("GET", "/api/v1/auth/me", nil, refreshToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusUnauthorized, writer.Code)
}

// Refresh Token
// =================================================================================================

func TestUserAPI_RefreshTokenSuccess(t *testing.T) {
	writer := makeRequest("POST", "/api/v1/auth/refresh", nil, refreshToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.NotNil(t, response["result"]["access_token"])
}

func TestUserAPI_RefreshTokenUnauthorized(t *testing.T) {
	writer := makeRequest("POST", "/api/v1/auth/refresh", nil, "")
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusUnauthorized, writer.Code)
}

func TestUserAPI_RefreshTokenInvalidTokenType(t *testing.T) {
	writer := makeRequest("POST", "/api/v1/auth/refresh", nil, accessToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusUnauthorized, writer.Code)
}

func TestUserAPI_RefreshTokenUserNotFound(t *testing.T) {
	token := jtoken.GenerateRefreshToken(map[string]interface{}{
		"id": "user-not-found",
	})

	writer := makeRequest("POST", "/api/v1/auth/refresh", nil, token)
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

// Change Password
// =================================================================================================

func TestUserAPI_ChangePasswordSuccess(t *testing.T) {
	defer cleanData()

	user := model.User{Email: "changepassword1@gmail.com", Password: "123456"}
	dbTest.Create(context.Background(), &user)

	token := jtoken.GenerateAccessToken(map[string]interface{}{
		"id": user.ID,
	})

	req := &dto.ChangePasswordReq{
		Password:    "123456",
		NewPassword: "new123456",
	}

	writer := makeRequest("PUT", "/api/v1/auth/change-password", req, token)
	assert.Equal(t, http.StatusOK, writer.Code)
}

func TestUserAPI_ChangePasswordRevokesExistingRefreshToken(t *testing.T) {
	user := model.User{Email: "refresh-revocation@example.com", Password: "123456"}
	require.NoError(t, dbTest.Create(context.Background(), &user))
	defer cleanData(&user)

	login := makeRequest("POST", "/api/v1/auth/login", &dto.LoginReq{
		Email: user.Email, Password: "123456",
	}, "")
	require.Equal(t, http.StatusOK, login.Code)
	var loginResponse struct {
		Result dto.LoginRes `json:"result"`
	}
	require.NoError(t, json.Unmarshal(login.Body.Bytes(), &loginResponse))
	access := loginResponse.Result.AccessToken
	refresh := loginResponse.Result.RefreshToken
	require.NotEmpty(t, access)
	require.NotEmpty(t, refresh)

	changed := makeRequest("PUT", "/api/v1/auth/change-password", &dto.ChangePasswordReq{
		Password: "123456", NewPassword: "new123456",
	}, access)
	require.Equal(t, http.StatusOK, changed.Code)

	revoked := makeRequest("POST", "/api/v1/auth/refresh", nil, refresh)
	assert.Equal(t, http.StatusUnauthorized, revoked.Code)

	newLogin := makeRequest("POST", "/api/v1/auth/login", &dto.LoginReq{
		Email: user.Email, Password: "new123456",
	}, "")
	require.Equal(t, http.StatusOK, newLogin.Code)
	require.NoError(t, json.Unmarshal(newLogin.Body.Bytes(), &loginResponse))
	newRefresh := loginResponse.Result.RefreshToken
	assert.NotEqual(t, refresh, newRefresh)
	assert.Equal(t, http.StatusOK, makeRequest("POST", "/api/v1/auth/refresh", nil, newRefresh).Code)
}

func TestUserAPI_ChangePasswordUnauthorized(t *testing.T) {
	req := &dto.ChangePasswordReq{
		Password:    "123456",
		NewPassword: "new123456",
	}

	writer := makeRequest("PUT", "/api/v1/auth/change-password", req, "")
	assert.Equal(t, http.StatusUnauthorized, writer.Code)
}

func TestUserAPI_ChangePasswordIsWrong(t *testing.T) {
	req := &dto.ChangePasswordReq{
		Password:    "wrong123456",
		NewPassword: "new123456",
	}

	writer := makeRequest("PUT", "/api/v1/auth/change-password", req, accessToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestUserAPI_ChangePasswordInvalidNewPassword(t *testing.T) {
	req := &dto.ChangePasswordReq{
		Password:    "test123456",
		NewPassword: "new",
	}

	writer := makeRequest("PUT", "/api/v1/auth/change-password", req, accessToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusInternalServerError, writer.Code)
	assert.Equal(t, "Something went wrong", response["error"]["message"])
}

func TestUserAPI_ChangePasswordInvalidFieldType(t *testing.T) {
	req := map[string]interface{}{
		"password":     1,
		"new_password": "new",
	}

	writer := makeRequest("PUT", "/api/v1/auth/change-password", req, accessToken())
	var response map[string]map[string]string
	_ = json.Unmarshal(writer.Body.Bytes(), &response)
	assert.Equal(t, http.StatusBadRequest, writer.Code)
	assert.Equal(t, "Invalid parameters", response["error"]["message"])
}
