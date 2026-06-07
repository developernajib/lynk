package application

import (
	"context"

	"github.com/developernajib/lynk/services/core/internal/modules/example/domain"
)

// ListNotes is the paged-read use case.
type ListNotes struct {
	notes domain.NoteRepository
}

// NewListNotes wires the use case.
func NewListNotes(notes domain.NoteRepository) *ListNotes {
	return &ListNotes{notes: notes}
}

// Execute pages the owner's notes, newest first. Limits are clamped here as
// defense in depth even though protovalidate already bounds them at the edge.
func (uc *ListNotes) Execute(ctx context.Context, ownerID string, limit, offset int32) ([]*domain.Note, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return uc.notes.List(ctx, ownerID, limit, offset)
}
