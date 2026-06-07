package application

import (
	"context"
	"strings"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain/vo"
)

// apiKeyPrefix marks lynk keys so logs and scanners can recognize them.
const apiKeyPrefix = "lynk_"

// APIKeyService owns the machine-credential flows. Grouped like OTPService:
// four small flows over one dependency set.
type APIKeyService struct {
	users  domain.UserRepository
	keys   domain.APIKeyRepository
	opaque OpaqueTokens
	cache  APIKeyCache
	events EventPublisher
	uow    UnitOfWork
	clock  Clock
	ids    IDGenerator
}

// NewAPIKeyService wires the flows.
func NewAPIKeyService(
	users domain.UserRepository,
	keys domain.APIKeyRepository,
	opaque OpaqueTokens,
	cache APIKeyCache,
	events EventPublisher,
	uow UnitOfWork,
	clock Clock,
	ids IDGenerator,
) *APIKeyService {
	return &APIKeyService{
		users: users, keys: keys, opaque: opaque, cache: cache,
		events: events, uow: uow, clock: clock, ids: ids,
	}
}

// Create mints a key and returns the full secret exactly once.
func (s *APIKeyService) Create(ctx context.Context, userID, name string) (*domain.APIKey, string, error) {
	raw, _, err := s.opaque.Generate()
	if err != nil {
		return nil, "", err
	}
	// The hash covers the FULL presented string, prefix included, so
	// validation hashes exactly what the client sends.
	full := apiKeyPrefix + raw
	hash := s.opaque.Hash(full)
	prefix := full[:len(apiKeyPrefix)+4]

	keyID, err := s.ids.NewID()
	if err != nil {
		return nil, "", err
	}

	now := s.clock.Now()
	key := domain.NewAPIKey(keyID, userID, name, hash, prefix, now)

	err = s.uow.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.keys.Create(ctx, key); err != nil {
			return err
		}
		return s.events.Publish(ctx, []domain.Event{
			domain.APIKeyCreated{UserID: userID, KeyID: keyID, Name: name, OccurredAt: now},
		})
	})
	if err != nil {
		return nil, "", err
	}
	return key, full, nil
}

// List returns the caller's keys, metadata only.
func (s *APIKeyService) List(ctx context.Context, userID string) ([]*domain.APIKey, error) {
	return s.keys.ListForUser(ctx, userID)
}

// Revoke disables one of the caller's keys and drops it from the cache so
// revocation takes effect immediately, not at cache expiry.
func (s *APIKeyService) Revoke(ctx context.Context, userID, keyID string) error {
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.keys.Revoke(ctx, keyID, userID, s.clock.Now()); err != nil {
			return err
		}
		return s.events.Publish(ctx, []domain.Event{
			domain.APIKeyRevoked{UserID: userID, KeyID: keyID, OccurredAt: s.clock.Now()},
		})
	})
	if err != nil {
		return err
	}
	s.cache.Drop(ctx, keyID)
	return nil
}

// Validate resolves a presented key to its owner, serving repeats from the
// cache. Revoked keys, unknown keys, and disabled owners all collapse into
// ErrAPIKeyNotFound.
func (s *APIKeyService) Validate(ctx context.Context, presented string) (userID, role string, err error) {
	if !strings.HasPrefix(presented, apiKeyPrefix) {
		return "", "", domain.ErrAPIKeyNotFound
	}
	hash := s.opaque.Hash(presented)

	if cachedUser, cachedRole, ok := s.cache.Lookup(ctx, hash); ok {
		return cachedUser, cachedRole, nil
	}

	key, err := s.keys.GetByHash(ctx, hash)
	if err != nil || !key.IsActive() {
		return "", "", domain.ErrAPIKeyNotFound
	}

	ownerID, err := vo.NewUserID(key.UserID())
	if err != nil {
		return "", "", domain.ErrAPIKeyNotFound
	}
	owner, err := s.users.GetByID(ctx, ownerID)
	if err != nil || !owner.CanSignIn() {
		return "", "", domain.ErrAPIKeyNotFound
	}

	// Positive results only: a missing key must stay a database question so
	// a just-created key works immediately.
	s.cache.Store(ctx, hash, key.ID(), owner.ID().String(), owner.Role())
	return owner.ID().String(), owner.Role(), nil
}
