// Package grpc adapts the identity use cases to the proto contract.
package grpc

import (
	"context"
	"errors"

	identityv1 "github.com/developernajib/lynk/services/core/internal/gen/proto/identity/v1"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/application"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain/vo"
	"github.com/developernajib/lynk/services/core/internal/platform/apperror"
	"github.com/developernajib/lynk/services/core/internal/platform/auth"
)

// Handler implements identity.v1.IdentityService.
type Handler struct {
	identityv1.UnimplementedIdentityServiceServer

	register       *application.Register
	login          *application.Login
	refresh        *application.Refresh
	logout         *application.Logout
	getProfile     *application.GetProfile
	changePassword *application.ChangePassword
	setUserRole    *application.SetUserRole
}

// NewHandler wires the handler.
func NewHandler(
	register *application.Register,
	login *application.Login,
	refresh *application.Refresh,
	logout *application.Logout,
	getProfile *application.GetProfile,
	changePassword *application.ChangePassword,
	setUserRole *application.SetUserRole,
) *Handler {
	return &Handler{
		register:       register,
		login:          login,
		refresh:        refresh,
		logout:         logout,
		getProfile:     getProfile,
		changePassword: changePassword,
		setUserRole:    setUserRole,
	}
}

// Register creates a user account (public).
func (h *Handler) Register(ctx context.Context, req *identityv1.RegisterRequest) (*identityv1.RegisterResponse, error) {
	user, err := h.register.Execute(ctx, req.GetEmail(), req.GetPassword(), req.GetFullName())
	if err != nil {
		return nil, classify(err)
	}
	return &identityv1.RegisterResponse{User: toProtoUser(user)}, nil
}

// Login exchanges credentials for a token pair (public).
func (h *Handler) Login(ctx context.Context, req *identityv1.LoginRequest) (*identityv1.LoginResponse, error) {
	user, pair, err := h.login.Execute(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		return nil, classify(err)
	}
	return &identityv1.LoginResponse{
		AccessToken:          pair.Access.Token,
		AccessTokenExpiresAt: pair.Access.ExpiresAt.Unix(),
		RefreshToken:         pair.RefreshRaw,
		User:                 toProtoUser(user),
	}, nil
}

// Refresh rotates a refresh token (public; the token IS the credential).
func (h *Handler) Refresh(ctx context.Context, req *identityv1.RefreshRequest) (*identityv1.RefreshResponse, error) {
	pair, err := h.refresh.Execute(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, classify(err)
	}
	return &identityv1.RefreshResponse{
		AccessToken:          pair.Access.Token,
		AccessTokenExpiresAt: pair.Access.ExpiresAt.Unix(),
		RefreshToken:         pair.RefreshRaw,
	}, nil
}

// Logout revokes the session (public and forgiving by design).
func (h *Handler) Logout(ctx context.Context, req *identityv1.LogoutRequest) (*identityv1.LogoutResponse, error) {
	if err := h.logout.Execute(ctx, req.GetRefreshToken(), req.GetAccessTokenId()); err != nil {
		return nil, classify(err)
	}
	return &identityv1.LogoutResponse{}, nil
}

// GetProfile returns the authenticated caller's profile.
func (h *Handler) GetProfile(ctx context.Context, _ *identityv1.GetProfileRequest) (*identityv1.GetProfileResponse, error) {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	user, err := h.getProfile.Execute(ctx, principal.UserID)
	if err != nil {
		return nil, classify(err)
	}
	return &identityv1.GetProfileResponse{User: toProtoUser(user)}, nil
}

// ChangePassword re-authenticates and swaps the credential.
func (h *Handler) ChangePassword(ctx context.Context, req *identityv1.ChangePasswordRequest) (*identityv1.ChangePasswordResponse, error) {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.changePassword.Execute(ctx, principal.UserID, req.GetCurrentPassword(), req.GetNewPassword()); err != nil {
		return nil, classify(err)
	}
	return &identityv1.ChangePasswordResponse{}, nil
}

// SetUserRole reassigns a user's role. Admin-only in code: role assignment
// IS granting access, so it must not depend on editable policies.
func (h *Handler) SetUserRole(ctx context.Context, req *identityv1.SetUserRoleRequest) (*identityv1.SetUserRoleResponse, error) {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if principal.Role != "admin" && principal.TokenType != "admin" {
		return nil, apperror.New(apperror.KindPermissionDenied, "admin_required", "admin access required")
	}

	user, err := h.setUserRole.Execute(ctx, req.GetUserId(), req.GetRole())
	if err != nil {
		return nil, classify(err)
	}
	return &identityv1.SetUserRoleResponse{User: toProtoUser(user)}, nil
}

func requirePrincipal(ctx context.Context) (auth.Principal, error) {
	principal, ok := auth.FromContext(ctx)
	if !ok {
		return auth.Principal{}, apperror.New(apperror.KindUnauthenticated, "unauthenticated", "authentication required")
	}
	return principal, nil
}

// classify maps domain sentinels onto transport-agnostic kinds in one place.
func classify(err error) error {
	switch {
	case errors.Is(err, domain.ErrEmailTaken):
		return apperror.Wrap(err, apperror.KindAlreadyExists, "email_taken", "email already registered")
	case errors.Is(err, domain.ErrInvalidCredentials), errors.Is(err, domain.ErrRefreshTokenInvalid):
		// One generic message for every credential failure: no enumeration.
		return apperror.Wrap(err, apperror.KindUnauthenticated, "invalid_credentials", "invalid credentials")
	case errors.Is(err, domain.ErrAccountLocked):
		return apperror.Wrap(err, apperror.KindRateLimited, "account_locked", "too many attempts, try again later")
	case errors.Is(err, domain.ErrAccountDisabled):
		return apperror.Wrap(err, apperror.KindPermissionDenied, "account_disabled", "account disabled")
	case errors.Is(err, domain.ErrUserNotFound):
		return apperror.Wrap(err, apperror.KindNotFound, "user_not_found", "user not found")
	case errors.Is(err, domain.ErrConcurrentUpdate):
		return apperror.Wrap(err, apperror.KindConflict, "user_conflict", "account was modified concurrently, retry")
	case errors.Is(err, vo.ErrInvalidEmail), errors.Is(err, vo.ErrInvalidUserID), errors.Is(err, domain.ErrMissingName), errors.Is(err, domain.ErrInvalidRole):
		return apperror.Wrap(err, apperror.KindInvalidInput, "invalid_input", err.Error())
	default:
		return apperror.Wrap(err, apperror.KindInternal, "internal", "internal error")
	}
}

func toProtoUser(user *domain.User) *identityv1.User {
	return &identityv1.User{
		Id:       user.ID().String(),
		Email:    user.Email().String(),
		FullName: user.FullName(),
		Role:     user.Role(),
	}
}
