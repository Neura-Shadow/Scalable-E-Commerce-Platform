package service

import (
	"context"
	"errors"
	"testing"

	"github.com/quangdangfit/gocommon/logger"
	"github.com/quangdangfit/gocommon/validation"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"golang.org/x/crypto/bcrypt"

	"goshop/internal/user/dto"
	"goshop/internal/user/model"
	"goshop/internal/user/repository/mocks"
	"goshop/pkg/config"
	"goshop/pkg/utils"
)

type UserServiceTestSuite struct {
	suite.Suite
	mockRepo *mocks.IUserRepository
	service  IUserService
}

func (suite *UserServiceTestSuite) SetupTest() {
	logger.Initialize(config.ProductionEnv)

	validator := validation.New()
	suite.mockRepo = mocks.NewIUserRepository(suite.T())
	suite.service = NewUserService(validator, suite.mockRepo)
}

func TestUserServiceTestSuite(t *testing.T) {
	suite.Run(t, new(UserServiceTestSuite))
}

func mustHashPassword(t *testing.T, password string) string {
	t.Helper()
	hashed, err := utils.HashAndSalt([]byte(password))
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	return hashed
}

// Login
// =================================================================

func (suite *UserServiceTestSuite) TestLoginGetUserByEmailFail() {
	req := &dto.LoginReq{
		Email:    "test@test.com",
		Password: "test123456",
	}
	suite.mockRepo.On("GetUserByEmail", mock.Anything, req.Email).
		Return(nil, errors.New("error")).Times(1)

	user, accessToken, refreshToken, err := suite.service.Login(context.Background(), req)
	suite.Nil(user)
	suite.Empty(accessToken)
	suite.Empty(refreshToken)
	suite.NotNil(err)
}

func (suite *UserServiceTestSuite) TestLoginInvalidEmailFormat() {
	req := &dto.LoginReq{
		Email:    "email",
		Password: "test123456",
	}

	user, accessToken, refreshToken, err := suite.service.Login(context.Background(), req)
	suite.Nil(user)
	suite.Empty(accessToken)
	suite.Empty(refreshToken)
	suite.NotNil(err)
}

func (suite *UserServiceTestSuite) TestLoginWrongPassword() {
	req := &dto.LoginReq{
		Email:    "test@test.com",
		Password: "test123456",
	}

	suite.mockRepo.On("GetUserByEmail", mock.Anything, req.Email).
		Return(&model.User{
			Email:    "test@test.com",
			Password: "password",
		}, nil).Times(1)

	user, accessToken, refreshToken, err := suite.service.Login(context.Background(), req)
	suite.Nil(user)
	suite.Empty(accessToken)
	suite.Empty(refreshToken)
	suite.NotNil(err)
}

func (suite *UserServiceTestSuite) TestLoginSuccess() {
	req := &dto.LoginReq{
		Email:    "test@test.com",
		Password: "test123456",
	}
	suite.mockRepo.On("GetUserByEmail", mock.Anything, req.Email).
		Return(
			&model.User{
				Email:    "test@test.com",
				Password: mustHashPassword(suite.T(), "test123456"),
			},
			nil,
		).Times(1)

	user, accessToken, refreshToken, err := suite.service.Login(context.Background(), req)
	suite.NotNil(user)
	suite.Equal(req.Email, user.Email)
	suite.NotEmpty(accessToken)
	suite.NotEmpty(refreshToken)
	suite.Nil(err)
}

func (suite *UserServiceTestSuite) TestLoginUpgradesLegacyBcryptCost() {
	req := &dto.LoginReq{Email: "legacy@test.com", Password: "test123456"}
	legacyHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.MinCost)
	suite.Require().NoError(err)
	user := &model.User{Email: req.Email, Password: string(legacyHash)}

	suite.mockRepo.On("GetUserByEmail", mock.Anything, req.Email).Return(user, nil).Once()
	suite.mockRepo.On("Update", mock.Anything, mock.MatchedBy(func(updated *model.User) bool {
		cost, costErr := bcrypt.Cost([]byte(updated.Password))
		return costErr == nil && cost == bcrypt.DefaultCost
	})).Return(nil).Once()

	loggedIn, accessToken, refreshToken, err := suite.service.Login(context.Background(), req)

	suite.Require().NoError(err)
	suite.Same(user, loggedIn)
	suite.NotEmpty(accessToken)
	suite.NotEmpty(refreshToken)
}

// Register
// =================================================================

func (suite *UserServiceTestSuite) TestRegisterSuccess() {
	req := &dto.RegisterReq{
		Email:    "test@test.com",
		Password: "test123456",
	}
	suite.mockRepo.On("Create", mock.Anything, mock.MatchedBy(func(user *model.User) bool {
		return user.Email == req.Email && user.Password == req.Password
	})).
		Return(nil).Times(1)

	user, err := suite.service.Register(context.Background(), req)
	suite.NotNil(user)
	suite.Nil(err)
}

func (suite *UserServiceTestSuite) TestRegisterCreateUserFail() {
	req := &dto.RegisterReq{
		Email:    "test@test.com",
		Password: "test123456",
	}
	suite.mockRepo.On("Create", mock.Anything, mock.Anything).
		Return(errors.New("error")).Times(1)

	user, err := suite.service.Register(context.Background(), req)
	suite.Nil(user)
	suite.NotNil(err)
}

func (suite *UserServiceTestSuite) TestRegisterInvalidEmailFormat() {
	req := &dto.RegisterReq{
		Email:    "email",
		Password: "test123456",
	}
	user, err := suite.service.Register(context.Background(), req)
	suite.Nil(user)
	suite.NotNil(err)
}

// GetUserByID
// =================================================================

func (suite *UserServiceTestSuite) TestGetUserByIDSuccess() {
	userID := "userID"

	suite.mockRepo.On("GetUserByID", mock.Anything, userID).
		Return(
			&model.User{
				ID:    userID,
				Email: "test@test.com",
			},
			nil,
		).Times(1)

	user, err := suite.service.GetUserByID(context.Background(), userID)
	suite.NotNil(user)
	suite.Equal(userID, user.ID)
	suite.Equal("test@test.com", user.Email)
	suite.Nil(err)
}

func (suite *UserServiceTestSuite) TestGetUserByIDFail() {
	userID := "userID"
	suite.mockRepo.On("GetUserByID", mock.Anything, userID).
		Return(nil, errors.New("error")).Times(1)

	user, err := suite.service.GetUserByID(context.Background(), userID)
	suite.Nil(user)
	suite.NotNil(err)
}

// RefreshToken
// =================================================================

func (suite *UserServiceTestSuite) TestRefreshTokenSuccess() {
	userID := "userID"
	suite.mockRepo.On("GetUserByID", mock.Anything, userID).
		Return(
			&model.User{
				ID:           userID,
				Email:        "test@test.com",
				TokenVersion: 7,
			}, nil,
		).Times(1)

	refreshToken, err := suite.service.RefreshToken(context.Background(), userID, 7)
	suite.NotEmpty(refreshToken)
	suite.Nil(err)
}

func (suite *UserServiceTestSuite) TestRefreshTokenGetUserByIDFail() {
	userID := "userID"
	suite.mockRepo.On("GetUserByID", mock.Anything, userID).
		Return(nil, errors.New("error")).Times(1)

	refreshToken, err := suite.service.RefreshToken(context.Background(), userID, 0)
	suite.Empty(refreshToken)
	suite.NotNil(err)
}

func (suite *UserServiceTestSuite) TestRefreshTokenRejectsRevokedVersion() {
	userID := "userID"
	suite.mockRepo.On("GetUserByID", mock.Anything, userID).
		Return(&model.User{ID: userID, TokenVersion: 2}, nil).Once()

	accessToken, err := suite.service.RefreshToken(context.Background(), userID, 1)

	suite.Empty(accessToken)
	suite.ErrorIs(err, model.ErrRefreshTokenRevoked)
}

// ChangePassword
// =================================================================

func (suite *UserServiceTestSuite) TestChangePasswordSuccess() {
	userID := "userID"
	req := &dto.ChangePasswordReq{
		Password:    "password",
		NewPassword: "newPassword",
	}

	suite.mockRepo.On("GetUserByID", mock.Anything, userID).
		Return(
			&model.User{
				ID:       userID,
				Email:    "test@test.com",
				Password: mustHashPassword(suite.T(), "password"),
			}, nil,
		).Times(1)
	suite.mockRepo.On("Update", mock.Anything, mock.MatchedBy(func(user *model.User) bool {
		return user.TokenVersion == 1
	})).
		Return(nil).Times(1)

	err := suite.service.ChangePassword(context.Background(), userID, req)
	suite.Nil(err)
}

func (suite *UserServiceTestSuite) TestChangePasswordGetUserByIDFail() {
	userID := "userID"
	req := &dto.ChangePasswordReq{
		Password:    "password",
		NewPassword: "newPassword",
	}

	suite.mockRepo.On("GetUserByID", mock.Anything, userID).
		Return(nil, errors.New("error")).Times(1)

	err := suite.service.ChangePassword(context.Background(), userID, req)
	suite.NotNil(err)
}

func (suite *UserServiceTestSuite) TestChangePasswordMissRequiredField() {
	userID := "userID"
	req := &dto.ChangePasswordReq{
		Password:    "password",
		NewPassword: "",
	}

	err := suite.service.ChangePassword(context.Background(), userID, req)
	suite.NotNil(err)
}

func (suite *UserServiceTestSuite) TestChangePasswordWrongCurrentPassword() {
	userID := "userID"
	req := &dto.ChangePasswordReq{
		Password:    "password1",
		NewPassword: "newPassword",
	}

	suite.mockRepo.On("GetUserByID", mock.Anything, userID).
		Return(
			&model.User{
				ID:       userID,
				Email:    "test@test.com",
				Password: mustHashPassword(suite.T(), "password"),
			}, nil,
		).Times(1)

	err := suite.service.ChangePassword(context.Background(), userID, req)
	suite.NotNil(err)
}

func (suite *UserServiceTestSuite) TestChangePasswordUpdateUserFail() {
	userID := "userID"
	req := &dto.ChangePasswordReq{
		Password:    "password",
		NewPassword: "newPassword",
	}

	suite.mockRepo.On("GetUserByID", mock.Anything, userID).
		Return(
			&model.User{
				ID:       userID,
				Email:    "test@test.com",
				Password: mustHashPassword(suite.T(), "password"),
			}, nil,
		).Times(1)
	suite.mockRepo.On("Update", mock.Anything, mock.Anything).
		Return(errors.New("error")).Times(1)

	err := suite.service.ChangePassword(context.Background(), userID, req)
	suite.NotNil(err)
}
