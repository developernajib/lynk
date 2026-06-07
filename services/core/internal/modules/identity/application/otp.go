package application

import (
	"context"
	"crypto/subtle"
	"time"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain/vo"
)

// otpTTL bounds how long an emailed code stays valid.
const otpTTL = 15 * time.Minute

// OTPService owns the one-time-code flows: password reset and email
// verification. Grouped in one use-case struct because the four flows share
// every dependency and the issue/verify mechanics.
type OTPService struct {
	users    domain.UserRepository
	otps     domain.OTPRepository
	sessions domain.RefreshTokenRepository
	hasher   PasswordHasher
	codes    OTPCodes
	notifier Notifier
	throttle LoginThrottle
	events   EventPublisher
	uow      UnitOfWork
	clock    Clock
	ids      IDGenerator
}

// NewOTPService wires the flows.
func NewOTPService(
	users domain.UserRepository,
	otps domain.OTPRepository,
	sessions domain.RefreshTokenRepository,
	hasher PasswordHasher,
	codes OTPCodes,
	notifier Notifier,
	throttle LoginThrottle,
	events EventPublisher,
	uow UnitOfWork,
	clock Clock,
	ids IDGenerator,
) *OTPService {
	return &OTPService{
		users: users, otps: otps, sessions: sessions, hasher: hasher,
		codes: codes, notifier: notifier, throttle: throttle,
		events: events, uow: uow, clock: clock, ids: ids,
	}
}

// RequestPasswordReset issues a reset code. It returns nil for unknown
// emails too: the response must not reveal whether an account exists.
func (s *OTPService) RequestPasswordReset(ctx context.Context, email string) error {
	address, err := vo.NewEmail(email)
	if err != nil {
		return nil
	}
	user, err := s.users.GetByEmail(ctx, address)
	if err != nil {
		return nil
	}

	code, err := s.issue(ctx, user, domain.OTPPasswordReset)
	if err != nil {
		return err
	}
	if err := s.notifier.SendOTP(ctx, address.String(), domain.OTPPasswordReset, code); err != nil {
		return err
	}
	return s.events.Publish(ctx, []domain.Event{
		domain.PasswordResetRequested{UserID: user.ID().String(), OccurredAt: s.clock.Now()},
	})
}

// ResetPassword consumes a valid code, sets the password, and revokes every
// session. Verification attempts share the lockout throttle (keyed
// separately from login) so codes cannot be brute-forced.
func (s *OTPService) ResetPassword(ctx context.Context, email, code, newPassword string) error {
	throttleKey := "otp:" + email
	if !s.throttle.Allowed(ctx, throttleKey) {
		return domain.ErrAccountLocked
	}

	address, err := vo.NewEmail(email)
	if err != nil {
		s.throttle.RecordFailure(ctx, throttleKey)
		return domain.ErrInvalidOTP
	}
	user, err := s.users.GetByEmail(ctx, address)
	if err != nil {
		s.throttle.RecordFailure(ctx, throttleKey)
		return domain.ErrInvalidOTP
	}

	otp, err := s.verify(ctx, user.ID().String(), domain.OTPPasswordReset, code)
	if err != nil {
		s.throttle.RecordFailure(ctx, throttleKey)
		return err
	}

	newHash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	user.ChangePassword(newHash, s.clock.Now())

	err = s.uow.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.otps.Consume(ctx, otp.ID(), s.clock.Now()); err != nil {
			return err
		}
		if err := s.users.Update(ctx, user); err != nil {
			return err
		}
		if err := s.sessions.RevokeAllForUser(ctx, user.ID().String(), s.clock.Now()); err != nil {
			return err
		}
		return s.events.Publish(ctx, user.PullEvents())
	})
	if err != nil {
		return err
	}

	s.throttle.Reset(ctx, throttleKey)
	return nil
}

// RequestEmailVerification sends the authenticated caller a code.
func (s *OTPService) RequestEmailVerification(ctx context.Context, userID string) error {
	id, err := vo.NewUserID(userID)
	if err != nil {
		return domain.ErrUserNotFound
	}
	user, err := s.users.GetByID(ctx, id)
	if err != nil {
		return err
	}

	code, err := s.issue(ctx, user, domain.OTPEmailVerify)
	if err != nil {
		return err
	}
	return s.notifier.SendOTP(ctx, user.Email().String(), domain.OTPEmailVerify, code)
}

// VerifyEmail consumes a valid code and marks the caller's email verified.
func (s *OTPService) VerifyEmail(ctx context.Context, userID, code string) error {
	id, err := vo.NewUserID(userID)
	if err != nil {
		return domain.ErrUserNotFound
	}
	user, err := s.users.GetByID(ctx, id)
	if err != nil {
		return err
	}

	otp, err := s.verify(ctx, user.ID().String(), domain.OTPEmailVerify, code)
	if err != nil {
		return err
	}

	return s.uow.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.otps.Consume(ctx, otp.ID(), s.clock.Now()); err != nil {
			return err
		}
		if err := s.users.MarkEmailVerified(ctx, user.ID(), s.clock.Now()); err != nil {
			return err
		}
		return s.events.Publish(ctx, []domain.Event{
			domain.EmailVerified{UserID: user.ID().String(), OccurredAt: s.clock.Now()},
		})
	})
}

// issue mints, stores, and returns a raw code for one purpose.
func (s *OTPService) issue(ctx context.Context, user *domain.User, purpose domain.OTPPurpose) (string, error) {
	raw, hash, err := s.codes.NewCode()
	if err != nil {
		return "", err
	}
	otpID, err := s.ids.NewID()
	if err != nil {
		return "", err
	}
	now := s.clock.Now()
	if err := s.otps.Create(ctx, domain.NewOTP(otpID, user.ID().String(), purpose, hash, now.Add(otpTTL), now)); err != nil {
		return "", err
	}
	return raw, nil
}

// verify loads the newest live code and compares hashes in constant time.
func (s *OTPService) verify(ctx context.Context, userID string, purpose domain.OTPPurpose, code string) (*domain.OTP, error) {
	otp, err := s.otps.GetActive(ctx, userID, purpose, s.clock.Now())
	if err != nil {
		return nil, domain.ErrInvalidOTP
	}
	if subtle.ConstantTimeCompare([]byte(s.codes.Hash(code)), []byte(otp.CodeHash())) != 1 {
		return nil, domain.ErrInvalidOTP
	}
	return otp, nil
}
