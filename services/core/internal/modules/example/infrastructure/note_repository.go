// Package infrastructure implements the example module's ports: sqlc-backed
// persistence, the transactional outbox, and the worker relay.
package infrastructure

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/developernajib/lynk/services/core/internal/gen/db"
	"github.com/developernajib/lynk/services/core/internal/modules/example/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/example/domain/vo"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
)

// NoteRepository persists Note aggregates through sqlc-generated queries.
type NoteRepository struct {
	pools *postgres.Pools
}

// NewNoteRepository builds the repository on the shared pools.
func NewNoteRepository(pools *postgres.Pools) *NoteRepository {
	return &NoteRepository{pools: pools}
}

// writeQuerier joins the active transaction when one is in the context (the
// unit-of-work pattern), otherwise it uses the primary pool. Repositories
// resolve their handle per call so their signatures never change.
func (r *NoteRepository) writeQuerier(ctx context.Context) *db.Queries {
	if tx, ok := postgres.TxFromContext(ctx); ok {
		return db.New(tx)
	}
	return db.New(r.pools.Write)
}

// readQuerier serves staleness-tolerant reads from a replica. Reads inside a
// transaction join it instead: read-your-writes within the unit of work.
func (r *NoteRepository) readQuerier(ctx context.Context) *db.Queries {
	if tx, ok := postgres.TxFromContext(ctx); ok {
		return db.New(tx)
	}
	return db.New(r.pools.Read())
}

// Create inserts the aggregate.
func (r *NoteRepository) Create(ctx context.Context, note *domain.Note) error {
	id, err := uuidFromString(note.ID().String())
	if err != nil {
		return err
	}
	err = r.writeQuerier(ctx).CreateNote(ctx, db.CreateNoteParams{
		ID:        id,
		OwnerID:   note.OwnerID(),
		Title:     note.Title().String(),
		Body:      note.Body(),
		Version:   note.Version(),
		CreatedAt: pgtype.Timestamptz{Time: note.CreatedAt(), Valid: true},
		UpdatedAt: pgtype.Timestamptz{Time: note.UpdatedAt(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("example: create note: %w", err)
	}
	return nil
}

// Get loads one of the owner's notes.
func (r *NoteRepository) Get(ctx context.Context, ownerID string, id vo.NoteID) (*domain.Note, error) {
	pgID, err := uuidFromString(id.String())
	if err != nil {
		return nil, err
	}
	row, err := r.readQuerier(ctx).GetNote(ctx, db.GetNoteParams{ID: pgID, OwnerID: ownerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNoteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("example: get note: %w", err)
	}
	return noteFromRow(row)
}

// Update saves with the optimistic-lock guard: the generated :execrows query
// reports affected rows, and zero means another writer bumped the version.
func (r *NoteRepository) Update(ctx context.Context, note *domain.Note) error {
	id, err := uuidFromString(note.ID().String())
	if err != nil {
		return err
	}
	affected, err := r.writeQuerier(ctx).UpdateNote(ctx, db.UpdateNoteParams{
		ID:        id,
		OwnerID:   note.OwnerID(),
		Title:     note.Title().String(),
		Body:      note.Body(),
		UpdatedAt: pgtype.Timestamptz{Time: note.UpdatedAt(), Valid: true},
		Version:   note.Version(),
	})
	if err != nil {
		return fmt.Errorf("example: update note: %w", err)
	}
	if affected == 0 {
		return domain.ErrConcurrentUpdate
	}
	// The guarded UPDATE bumped the row's version; mirror it in memory so
	// the caller returns the version the next update must present.
	note.BumpVersion()
	return nil
}

// List pages an owner's notes, newest first, from a replica.
func (r *NoteRepository) List(ctx context.Context, ownerID string, limit, offset int32) ([]*domain.Note, error) {
	rows, err := r.readQuerier(ctx).ListNotes(ctx, db.ListNotesParams{
		OwnerID: ownerID,
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		return nil, fmt.Errorf("example: list notes: %w", err)
	}

	notes := make([]*domain.Note, 0, len(rows))
	for _, row := range rows {
		note, err := noteFromRow(row)
		if err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	return notes, nil
}

// noteFromRow rehydrates the aggregate via FromState (no validation, no
// events: stored state was validated when written).
func noteFromRow(row db.ExampleNote) (*domain.Note, error) {
	id, err := vo.NewNoteID(uuidToString(row.ID))
	if err != nil {
		return nil, fmt.Errorf("example: corrupt note id: %w", err)
	}
	title, err := vo.NewTitle(row.Title)
	if err != nil {
		return nil, fmt.Errorf("example: corrupt note title: %w", err)
	}
	return domain.NoteFromState(
		id, row.OwnerID, title, row.Body,
		row.Version, row.CreatedAt.Time, row.UpdatedAt.Time,
	), nil
}
