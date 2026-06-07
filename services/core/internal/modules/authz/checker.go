// Package authz is the ABAC decision engine: policies live in the database
// as CEL expressions over subject/resource/action attributes, are compiled
// once and cached in-process, and evaluate deny-by-default with
// deny-overrides. This module is an ENGINE, not an aggregate: it has no
// domain lifecycle, so it deliberately skips the hexagon layering the
// entity modules use.
package authz

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/rs/zerolog"

	db "github.com/developernajib/lynk/services/core/internal/gen/db"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
	"github.com/developernajib/lynk/services/core/internal/platform/safe"
)

// cacheTTL bounds policy staleness across instances: an Upsert on one
// instance reaches the others within this window.
const cacheTTL = 60 * time.Second

// Subject is the caller's attributes, always taken from the verified
// principal, never from the request.
type Subject struct {
	ID        string
	Role      string
	TokenType string
}

// Decision is a verdict plus the policy that produced it ("" = default deny).
type Decision struct {
	Allowed   bool
	DecidedBy string
}

type compiledPolicy struct {
	name         string
	effect       string
	resourceType string
	action       string
	program      cel.Program
}

// Checker evaluates access decisions against the compiled policy cache.
type Checker struct {
	pools *postgres.Pools
	log   zerolog.Logger
	env   *cel.Env

	mu         sync.RWMutex
	policies   []compiledPolicy
	loadedAt   atomic.Int64
	refreshing atomic.Bool
}

// NewChecker builds the engine and its CEL environment. The environment
// declares exactly four variables; a policy referencing anything else fails
// compilation at upsert time, not evaluation time.
func NewChecker(pools *postgres.Pools, log zerolog.Logger) (*Checker, error) {
	env, err := cel.NewEnv(
		cel.Variable("subject", cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable("resource", cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable("action", cel.StringType),
		cel.Variable("resource_type", cel.StringType),
	)
	if err != nil {
		return nil, fmt.Errorf("authz: build cel env: %w", err)
	}
	return &Checker{pools: pools, log: log, env: env}, nil
}

// Compile validates a condition expression; used by the admin API so an
// unparseable policy can never be saved.
func (c *Checker) Compile(condition string) (cel.Program, error) {
	ast, issues := c.env.Compile(condition)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("authz: compile condition: %w", issues.Err())
	}
	program, err := c.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("authz: build program: %w", err)
	}
	return program, nil
}

// Refresh reloads and recompiles enabled policies. A row whose condition no
// longer compiles is skipped with a log instead of poisoning the whole set.
func (c *Checker) Refresh(ctx context.Context) error {
	rows, err := db.New(c.pools.Write).ListEnabledPolicies(ctx)
	if err != nil {
		return fmt.Errorf("authz: load policies: %w", err)
	}

	compiled := make([]compiledPolicy, 0, len(rows))
	for _, row := range rows {
		program, err := c.Compile(row.Condition)
		if err != nil {
			c.log.Error().Err(err).Str("policy", row.Name).Msg("authz: skipping uncompilable policy")
			continue
		}
		compiled = append(compiled, compiledPolicy{
			name:         row.Name,
			effect:       row.Effect,
			resourceType: row.ResourceType,
			action:       row.Action,
			program:      program,
		})
	}

	c.mu.Lock()
	c.policies = compiled
	c.mu.Unlock()
	c.loadedAt.Store(time.Now().UnixMilli())
	return nil
}

// Decide evaluates the policy set: any matching deny wins, otherwise the
// first matching allow wins, otherwise deny. An evaluation error (e.g. a
// missing map key) makes that policy NOT match, which is always the
// deny-safe direction.
func (c *Checker) Decide(ctx context.Context, subject Subject, resourceType, action string, resource map[string]string) Decision {
	c.maybeRefresh(ctx)

	if resource == nil {
		resource = map[string]string{}
	}
	vars := map[string]any{
		"subject": map[string]string{
			"id":         subject.ID,
			"role":       subject.Role,
			"token_type": subject.TokenType,
		},
		"resource":      resource,
		"action":        action,
		"resource_type": resourceType,
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	var allowedBy string
	for _, p := range c.policies {
		if !matches(p.resourceType, resourceType) || !matches(p.action, action) {
			continue
		}
		out, _, err := p.program.Eval(vars)
		if err != nil {
			continue
		}
		if truth, ok := out.Value().(bool); !ok || !truth {
			continue
		}
		if p.effect == "deny" {
			return Decision{Allowed: false, DecidedBy: p.name}
		}
		if allowedBy == "" {
			allowedBy = p.name
		}
	}

	if allowedBy != "" {
		return Decision{Allowed: true, DecidedBy: allowedBy}
	}
	return Decision{}
}

// maybeRefresh kicks ONE background reload when the cache has gone stale;
// callers keep deciding on the previous set meanwhile. Decisions never block
// on the database.
func (c *Checker) maybeRefresh(ctx context.Context) {
	if time.Since(time.UnixMilli(c.loadedAt.Load())) < cacheTTL {
		return
	}
	if !c.refreshing.CompareAndSwap(false, true) {
		return
	}
	safe.GoCtx(context.WithoutCancel(ctx), c.log, "authz-refresh", func(ctx context.Context) error {
		defer c.refreshing.Store(false)
		return c.Refresh(ctx)
	})
}

func matches(pattern, value string) bool {
	return pattern == "*" || pattern == value
}
