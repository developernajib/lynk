package application

import (
	"context"

	"github.com/developernajib/lynk/services/core/internal/modules/example/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/example/domain/vo"
)

// CreateNote is the create use case. One struct per use case keeps each
// file's dependency list honest: this one needs everything, reads need less.
type CreateNote struct {
	notes  domain.NoteRepository
	events EventPublisher
	uow    UnitOfWork
	clock  Clock
	ids    IDGenerator
}

// NewCreateNote wires the use case.
func NewCreateNote(notes domain.NoteRepository, events EventPublisher, uow UnitOfWork, clock Clock, ids IDGenerator) *CreateNote {
	return &CreateNote{notes: notes, events: events, uow: uow, clock: clock, ids: ids}
}

// Execute validates input into value objects, builds the aggregate, and
// persists state + events in ONE transaction (the outbox pattern).
func (uc *CreateNote) Execute(ctx context.Context, ownerID, title, body string) (*domain.Note, error) {
	rawID, err := uc.ids.NewID()
	if err != nil {
		return nil, err
	}
	noteID, err := vo.NewNoteID(rawID)
	if err != nil {
		return nil, err
	}
	noteTitle, err := vo.NewTitle(title)
	if err != nil {
		return nil, err
	}

	note, err := domain.NewNote(noteID, ownerID, noteTitle, body, uc.clock.Now())
	if err != nil {
		return nil, err
	}

	err = uc.uow.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.notes.Create(ctx, note); err != nil {
			return err
		}
		return uc.events.Publish(ctx, note.PullEvents())
	})
	if err != nil {
		return nil, err
	}
	return note, nil
}
