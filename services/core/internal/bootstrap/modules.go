// Package bootstrap is the service's composition root: it builds the
// platform pieces, assembles modules, and owns the two process lifecycles
// (server and worker). Adding a module touches exactly this file plus the
// stream subject list.
package bootstrap

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	"github.com/developernajib/lynk/services/core/internal/modules/authz"
	"github.com/developernajib/lynk/services/core/internal/modules/example"
	"github.com/developernajib/lynk/services/core/internal/modules/identity"
	"github.com/developernajib/lynk/services/core/internal/platform/config"
	"github.com/developernajib/lynk/services/core/internal/platform/jwt"
	"github.com/developernajib/lynk/services/core/internal/platform/nats"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
)

// coreStream is the JetStream stream this service OWNS: only this service's
// worker declares it (EnsureStream); every consumer binds.
const coreStream = "CORE_EVENTS"

// coreStreamSubjects must list every module's subject prefix. The worker
// converges the stream to exactly this list on boot.
var coreStreamSubjects = []string{
	"example.>",
	"identity.>",
}

// Modules holds every assembled module so the server can register handlers
// and the worker can collect relays from one place.
type Modules struct {
	Example  *example.Module
	Identity *identity.Module
	Authz    *authz.Module
}

// buildModules assembles all modules.
func buildModules(
	cfg *config.Config,
	log zerolog.Logger,
	pools *postgres.Pools,
	redisClient *redis.Client,
	bus *nats.Connection,
) (*Modules, error) {
	signer, err := buildSigner(cfg, log)
	if err != nil {
		return nil, err
	}

	authzModule, err := authz.New(authz.Dependencies{Pools: pools, Log: log})
	if err != nil {
		return nil, err
	}

	return &Modules{
		Example: example.New(example.Dependencies{Pools: pools, Bus: bus, Log: log}),
		Identity: identity.New(identity.Dependencies{
			Pools:      pools,
			Bus:        bus,
			Redis:      redisClient,
			Signer:     signer,
			AccessTTL:  cfg.JWT.AccessTokenTTL,
			RefreshTTL: cfg.JWT.RefreshTokenTTL,
			Log:        log,
		}),
		Authz: authzModule,
	}, nil
}

// buildSigner parses the configured RS256 key, or, in development only,
// generates an EPHEMERAL keypair so the service boots without setup. Tokens
// signed with an ephemeral key die on restart and verify nowhere else: the
// gateway needs the matching JWT_PUBLIC_KEY_PEM to accept them. Production
// config validation already required a real key.
func buildSigner(cfg *config.Config, log zerolog.Logger) (*jwt.Signer, error) {
	keyPEM := cfg.JWT.PrivateKeyPEM
	if keyPEM == "" {
		if cfg.IsProduction() {
			return nil, fmt.Errorf("bootstrap: JWT_PRIVATE_KEY_PEM required in production")
		}
		generated, err := generateEphemeralKeyPEM()
		if err != nil {
			return nil, err
		}
		keyPEM = generated
		log.Warn().Msg("JWT_PRIVATE_KEY_PEM not set: using an EPHEMERAL dev key (tokens die on restart and the gateway cannot verify them)")
	}

	return jwt.NewSigner(jwt.Config{
		PrivateKeyPEM:  keyPEM,
		Issuer:         cfg.JWT.Issuer,
		AccessTokenTTL: cfg.JWT.AccessTokenTTL,
	})
}

func generateEphemeralKeyPEM() (string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", fmt.Errorf("bootstrap: generate ephemeral key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", fmt.Errorf("bootstrap: marshal ephemeral key: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

// RegisterAll mounts every module's gRPC service onto the server.
func (m *Modules) RegisterAll(server *grpc.Server) {
	m.Example.Register(server)
	m.Identity.Register(server)
	m.Authz.Register(server)
}

// Runners returns every background loop the worker must run: outbox relays,
// consumers, and scheduled jobs.
func (m *Modules) Runners() []runner {
	return []runner{
		m.Example.Relay(),
		m.Identity.Relay(),
	}
}

// runner is anything that blocks in Run until its context is cancelled.
type runner interface {
	Run(ctx context.Context) error
}
