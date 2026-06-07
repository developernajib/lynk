package authz

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	authzv1 "github.com/developernajib/lynk/services/core/internal/gen/proto/authz/v1"
	db "github.com/developernajib/lynk/services/core/internal/gen/db"
	"github.com/developernajib/lynk/services/core/internal/platform/apperror"
	"github.com/developernajib/lynk/services/core/internal/platform/auth"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
	"github.com/developernajib/lynk/services/core/internal/platform/secure"
)

// handler implements authz.v1.AuthzService. Policy CRUD reads and writes go
// straight through sqlc (the engine-module pattern); the engine itself is
// the Checker.
type handler struct {
	authzv1.UnimplementedAuthzServiceServer

	pools   *postgres.Pools
	checker *Checker
}

// Check decides for the CALLER: the subject comes from the verified
// principal, so a client can never ask "could someone else do this".
func (h *handler) Check(ctx context.Context, req *authzv1.CheckRequest) (*authzv1.CheckResponse, error) {
	principal, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apperror.New(apperror.KindUnauthenticated, "unauthenticated", "authentication required")
	}

	decision := h.checker.Decide(ctx, Subject{
		ID:        principal.UserID,
		Role:      principal.Role,
		TokenType: principal.TokenType,
	}, req.GetResourceType(), req.GetAction(), req.GetResource())

	return &authzv1.CheckResponse{Allowed: decision.Allowed, DecidedBy: decision.DecidedBy}, nil
}

// ListPolicies returns the full rule set (admin only).
func (h *handler) ListPolicies(ctx context.Context, _ *authzv1.ListPoliciesRequest) (*authzv1.ListPoliciesResponse, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}

	rows, err := db.New(h.pools.Read()).ListAllPolicies(ctx)
	if err != nil {
		return nil, apperror.Wrap(err, apperror.KindInternal, "internal", "internal error")
	}

	policies := make([]*authzv1.Policy, 0, len(rows))
	for _, row := range rows {
		policies = append(policies, &authzv1.Policy{
			Name:         row.Name,
			Effect:       row.Effect,
			ResourceType: row.ResourceType,
			Action:       row.Action,
			Condition:    row.Condition,
			Enabled:      row.Enabled,
		})
	}
	return &authzv1.ListPoliciesResponse{Policies: policies}, nil
}

// UpsertPolicy validates (the condition must compile, the effect must be
// allow or deny) and saves, then refreshes the local cache immediately.
func (h *handler) UpsertPolicy(ctx context.Context, req *authzv1.UpsertPolicyRequest) (*authzv1.UpsertPolicyResponse, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}

	policy := req.GetPolicy()
	if policy.GetName() == "" {
		return nil, apperror.New(apperror.KindInvalidInput, "invalid_input", "policy name required")
	}
	if policy.GetEffect() != "allow" && policy.GetEffect() != "deny" {
		return nil, apperror.New(apperror.KindInvalidInput, "invalid_input", "effect must be allow or deny")
	}
	if _, err := h.checker.Compile(policy.GetCondition()); err != nil {
		return nil, apperror.Wrap(err, apperror.KindInvalidInput, "invalid_condition", fmt.Sprintf("condition does not compile: %v", err))
	}

	rawID, err := secure.UUIDv7()
	if err != nil {
		return nil, apperror.Wrap(err, apperror.KindInternal, "internal", "internal error")
	}
	var id pgtype.UUID
	if err := id.Scan(rawID); err != nil {
		return nil, apperror.Wrap(err, apperror.KindInternal, "internal", "internal error")
	}

	err = db.New(h.pools.Write).UpsertPolicy(ctx, db.UpsertPolicyParams{
		ID:           id,
		Name:         policy.GetName(),
		Effect:       policy.GetEffect(),
		ResourceType: policy.GetResourceType(),
		Action:       policy.GetAction(),
		Condition:    policy.GetCondition(),
		Enabled:      policy.GetEnabled(),
		CreatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		return nil, apperror.Wrap(err, apperror.KindInternal, "internal", "internal error")
	}

	if err := h.checker.Refresh(ctx); err != nil {
		return nil, apperror.Wrap(err, apperror.KindInternal, "internal", "internal error")
	}
	return &authzv1.UpsertPolicyResponse{Policy: policy}, nil
}

// DeletePolicy removes a rule by name (admin only).
func (h *handler) DeletePolicy(ctx context.Context, req *authzv1.DeletePolicyRequest) (*authzv1.DeletePolicyResponse, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}

	affected, err := db.New(h.pools.Write).DeletePolicy(ctx, req.GetName())
	if err != nil {
		return nil, apperror.Wrap(err, apperror.KindInternal, "internal", "internal error")
	}
	if affected == 0 {
		return nil, apperror.New(apperror.KindNotFound, "policy_not_found", "policy not found")
	}

	if err := h.checker.Refresh(ctx); err != nil {
		return nil, apperror.Wrap(err, apperror.KindInternal, "internal", "internal error")
	}
	return &authzv1.DeletePolicyResponse{}, nil
}

// requireAdmin guards the policy CRUD: editing policies IS granting access,
// so only admins may do it. This one check is intentionally code, not a
// policy: a broken policy set must never lock admins out of fixing it.
func requireAdmin(ctx context.Context) error {
	principal, ok := auth.FromContext(ctx)
	if !ok {
		return apperror.New(apperror.KindUnauthenticated, "unauthenticated", "authentication required")
	}
	if principal.Role != "admin" && principal.TokenType != "admin" {
		return apperror.New(apperror.KindPermissionDenied, "admin_required", "admin access required")
	}
	return nil
}
