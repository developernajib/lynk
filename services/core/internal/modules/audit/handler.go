package audit

import (
	"context"
	"encoding/hex"

	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"

	auditv1 "github.com/developernajib/lynk/services/core/internal/gen/proto/audit/v1"
	db "github.com/developernajib/lynk/services/core/internal/gen/db"
	"github.com/developernajib/lynk/services/core/internal/platform/apperror"
	"github.com/developernajib/lynk/services/core/internal/platform/auth"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
)

// handler implements audit.v1.AuditService.
type handler struct {
	auditv1.UnimplementedAuditServiceServer

	pools *postgres.Pools
}

// ListAuditLog pages the ledger, newest first. Admin-only: the ledger spans
// every module and every user.
func (h *handler) ListAuditLog(ctx context.Context, req *auditv1.ListAuditLogRequest) (*auditv1.ListAuditLogResponse, error) {
	principal, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apperror.New(apperror.KindUnauthenticated, "unauthenticated", "authentication required")
	}
	if principal.Role != "admin" && principal.TokenType != "admin" {
		return nil, apperror.New(apperror.KindPermissionDenied, "admin_required", "admin access required")
	}

	limit := req.GetLimit()
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := db.New(h.pools.Read()).ListAuditEntries(ctx, db.ListAuditEntriesParams{
		Limit:         limit,
		Offset:        req.GetOffset(),
		SubjectPrefix: req.GetSubjectPrefix(),
	})
	if err != nil {
		return nil, apperror.Wrap(err, apperror.KindInternal, "internal", "internal error")
	}

	entries := make([]*auditv1.AuditEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, &auditv1.AuditEntry{
			Id:         uuidToString(row.ID),
			Subject:    row.Subject,
			Payload:    string(row.Payload),
			OccurredAt: timestamppb.New(row.OccurredAt.Time),
			RecordedAt: timestamppb.New(row.RecordedAt.Time),
		})
	}
	return &auditv1.ListAuditLogResponse{Entries: entries}, nil
}

func uuidToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	var s [36]byte
	hex.Encode(s[:8], u.Bytes[:4])
	s[8] = '-'
	hex.Encode(s[9:13], u.Bytes[4:6])
	s[13] = '-'
	hex.Encode(s[14:18], u.Bytes[6:8])
	s[18] = '-'
	hex.Encode(s[19:23], u.Bytes[8:10])
	s[23] = '-'
	hex.Encode(s[24:], u.Bytes[10:])
	return string(s[:])
}
