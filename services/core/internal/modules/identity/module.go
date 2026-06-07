// Package identity is the working-auth module: registration, login with
// account lockout, rotating opaque refresh tokens, logout with access-token
// blacklisting, profile, and password change. OTP flows and API keys are
// deliberate follow-up seams: add them as new use cases without touching the
// existing ones.
package identity

import (
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	identityv1 "github.com/developernajib/lynk/services/core/internal/gen/proto/identity/v1"
	identityadapter "github.com/developernajib/lynk/services/core/internal/modules/identity/adapter/grpc"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/application"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/infrastructure"
	"github.com/developernajib/lynk/services/core/internal/platform/clock"
	"github.com/developernajib/lynk/services/core/internal/platform/jwt"
	"github.com/developernajib/lynk/services/core/internal/platform/nats"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
)

// Dependencies is everything the module needs from the platform.
type Dependencies struct {
	Pools      *postgres.Pools
	Bus        *nats.Connection
	Redis      *redis.Client
	Signer     *jwt.Signer
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Log        zerolog.Logger
}

// Module is the assembled identity module.
type Module struct {
	handler *identityadapter.Handler
	relay   *infrastructure.OutboxRelay
}

// New assembles the module's composition root.
func New(deps Dependencies) *Module {
	users := infrastructure.NewUserRepository(deps.Pools)
	sessions := infrastructure.NewRefreshTokenRepository(deps.Pools)
	otps := infrastructure.NewOTPRepository(deps.Pools)
	apiKeys := infrastructure.NewAPIKeyRepository(deps.Pools)
	events := infrastructure.NewOutboxPublisher(deps.Pools)
	uow := postgres.NewTxManager(deps.Pools.Write)
	systemClock := clock.System{}
	ids := infrastructure.UUIDGenerator{}
	hasher := infrastructure.Argon2idHasher{}
	opaque := infrastructure.OpaqueTokens{}
	codes := infrastructure.OTPCodes{}
	signer := infrastructure.NewAccessTokenSigner(deps.Signer)
	blacklist := infrastructure.NewRedisBlacklist(deps.Redis)
	throttle := infrastructure.NewRedisLoginThrottle(deps.Redis)
	notifier := infrastructure.NewLogNotifier(deps.Log)
	keyCache := infrastructure.NewRedisAPIKeyCache(deps.Redis)

	tokens := application.NewTokenService(signer, opaque, sessions, ids, systemClock, deps.RefreshTTL)

	handler := identityadapter.NewHandler(
		application.NewRegister(users, hasher, events, uow, systemClock, ids),
		application.NewLogin(users, hasher, throttle, tokens),
		application.NewRefresh(users, sessions, opaque, tokens, systemClock, uow),
		application.NewLogout(sessions, opaque, blacklist, systemClock, deps.AccessTTL),
		application.NewGetProfile(users),
		application.NewChangePassword(users, sessions, hasher, events, uow, systemClock),
		application.NewSetUserRole(users, events, uow, systemClock),
		application.NewOTPService(users, otps, sessions, hasher, codes, notifier, throttle, events, uow, systemClock, ids),
		application.NewAPIKeyService(users, apiKeys, opaque, keyCache, events, uow, systemClock, ids),
	)

	return &Module{
		handler: handler,
		relay:   infrastructure.NewOutboxRelay(uow, deps.Bus, deps.Log),
	}
}

// Register mounts the gRPC service.
func (m *Module) Register(server *grpc.Server) {
	identityv1.RegisterIdentityServiceServer(server, m.handler)
}

// Relay returns the outbox relay for the worker.
func (m *Module) Relay() *infrastructure.OutboxRelay {
	return m.relay
}
