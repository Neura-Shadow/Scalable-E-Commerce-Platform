package service

import (
	"context"
	"errors"

	"github.com/quangdangfit/gocommon/logger"
	"github.com/quangdangfit/gocommon/validation"
	"golang.org/x/crypto/bcrypt"

	"goshop/internal/user/dto"
	"goshop/internal/user/model"
	"goshop/internal/user/repository"
	"goshop/pkg/jtoken"
	"goshop/pkg/utils"
)

//go:generate mockery --name=IUserService
type IUserService interface {
	Login(ctx context.Context, req *dto.LoginReq) (*model.User, string, string, error)
	Register(ctx context.Context, req *dto.RegisterReq) (*model.User, error)
	GetUserByID(ctx context.Context, id string) (*model.User, error)
	RefreshToken(ctx context.Context, userID string, tokenVersion uint64) (string, error)
	ChangePassword(ctx context.Context, id string, req *dto.ChangePasswordReq) error
}

type UserService struct {
	validator validation.Validation
	repo      repository.IUserRepository
}

func NewUserService(
	validator validation.Validation,
	repo repository.IUserRepository) *UserService {
	return &UserService{
		validator: validator,
		repo:      repo,
	}
}

func (s *UserService) Login(ctx context.Context, req *dto.LoginReq) (*model.User, string, string, error) {
	if err := s.validator.ValidateStruct(req); err != nil {
		return nil, "", "", err
	}

	user, err := s.repo.GetUserByEmail(ctx, req.Email)
	if err != nil {
		logger.Errorf("Login.GetUserByEmail failed: %s", err)
		return nil, "", "", err
	}

	if err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return nil, "", "", errors.New("wrong password")
	}
	if err := s.upgradePasswordHash(ctx, user, req.Password); err != nil {
		return nil, "", "", err
	}

	tokenData := map[string]interface{}{
		"id":                     user.ID,
		"role":                   user.Role,
		jtoken.TokenVersionClaim: user.TokenVersion,
	}
	accessToken := jtoken.GenerateAccessToken(tokenData)
	refreshToken := jtoken.GenerateRefreshToken(tokenData)
	return user, accessToken, refreshToken, nil
}

func (s *UserService) Register(ctx context.Context, req *dto.RegisterReq) (*model.User, error) {
	if err := s.validator.ValidateStruct(req); err != nil {
		return nil, err
	}

	user := model.User{
		Email:    req.Email,
		Password: req.Password,
	}
	err := s.repo.Create(ctx, &user)
	if err != nil {
		logger.Errorf("Register.Create failed: %s", err)
		return nil, err
	}
	return &user, nil
}

func (s *UserService) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	user, err := s.repo.GetUserByID(ctx, id)
	if err != nil {
		logger.Errorf("GetUserByID fail, id: %s, error: %s", id, err)
		return nil, err
	}

	return user, nil
}

func (s *UserService) RefreshToken(ctx context.Context, userID string, tokenVersion uint64) (string, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		logger.Errorf("RefreshToken.GetUserByID fail, id: %s, error: %s", userID, err)
		return "", err
	}
	if user.TokenVersion != tokenVersion {
		return "", model.ErrRefreshTokenRevoked
	}

	tokenData := map[string]interface{}{
		"id":                     user.ID,
		"role":                   user.Role,
		jtoken.TokenVersionClaim: user.TokenVersion,
	}
	accessToken := jtoken.GenerateAccessToken(tokenData)
	return accessToken, nil
}

func (s *UserService) ChangePassword(ctx context.Context, id string, req *dto.ChangePasswordReq) error {
	if err := s.validator.ValidateStruct(req); err != nil {
		return err
	}
	user, err := s.repo.GetUserByID(ctx, id)
	if err != nil {
		logger.Errorf("ChangePassword.GetUserByID fail, id: %s, error: %s", id, err)
		return err
	}

	if err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return errors.New("wrong password")
	}

	hashedPassword, err := utils.HashAndSalt([]byte(req.NewPassword))
	if err != nil {
		return err
	}
	user.Password = hashedPassword
	user.TokenVersion++
	err = s.repo.Update(ctx, user)
	if err != nil {
		logger.Errorf("ChangePassword.Update fail, id: %s, error: %s", id, err)
		return err
	}

	return nil
}

func (s *UserService) upgradePasswordHash(ctx context.Context, user *model.User, password string) error {
	cost, err := bcrypt.Cost([]byte(user.Password))
	if err != nil {
		return err
	}
	if cost >= bcrypt.DefaultCost {
		return nil
	}

	hashedPassword, err := utils.HashAndSalt([]byte(password))
	if err != nil {
		return err
	}
	user.Password = hashedPassword
	if err := s.repo.Update(ctx, user); err != nil {
		logger.Errorf("Login.UpdatePasswordHash failed: %s", err)
		return err
	}
	return nil
}
