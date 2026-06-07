package application

import (
	"context"

	"github.com/developernajib/lynk/services/core/internal/modules/example/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/example/domain/vo"
)

// UpdateNote is the update use case with optimistic-lock handling.
type UpdateNote struct {
	notes  domain.NoteRepository
	events EventPublisher
	uow    UnitOfWork
	clock  Clock
}

// NewUpdateNote wires the use case.
func NewUpdateNote(notes domain.NoteRepository, events EventPublisher, uow UnitOfWork, clock Clock) *UpdateNote {
	return &UpdateNote{notes: notes, events: events, uow: uow, clock: clock}
}

// Execute re-reads the aggregate, applies the change, and saves guarded by
// the version the CALLER last saw: if another writer bumped it since, the
// repository reports domain.ErrConcurrentUpdate instead of overwriting.
func (uc *UpdateNote) Execute(ctx context.Context, ownerID, id, title, body string, version int64) (*domain.Note, error) {
	noteID, err := vo.NewNoteID(id)
	if err != nil {
		return nil, err
	}
	noteTitle, err := vo.NewTitle(title)
	if err != nil {
		return nil, err
	}

	var updated *domain.Note
	err = uc.uow.WithinTransaction(ctx, func(ctx context.Context) error {
		note, err := uc.notes.Get(ctx, ownerID, noteID)
		if err != nil {
			return err
		}
		// The caller's version, not the freshly-read one, guards the save:
		// a stale read in the client surfaces as a conflict, never a silent
		// last-writer-wins overwrite.
		if note.Version() != version {
			return domain.ErrConcurrentUpdate
		}

		note.Update(noteTitle, body, uc.clock.Now())
		if err := uc.notes.Update(ctx, note); err != nil {
			return err
		}
		if err := uc.events.Publish(ctx, note.PullEvents()); err != nil {
			return err
		}
		updated = note
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}
