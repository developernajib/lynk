package application

import (
	"context"

	"github.com/developernajib/lynk/services/core/internal/modules/example/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/example/domain/vo"
)

// GetNote is the single-read use case.
type GetNote struct {
	notes domain.NoteRepository
}

// NewGetNote wires the use case.
func NewGetNote(notes domain.NoteRepository) *GetNote {
	return &GetNote{notes: notes}
}

// Execute loads one note scoped to its owner.
func (uc *GetNote) Execute(ctx context.Context, ownerID, id string) (*domain.Note, error) {
	noteID, err := vo.NewNoteID(id)
	if err != nil {
		return nil, err
	}
	return uc.notes.Get(ctx, ownerID, noteID)
}
