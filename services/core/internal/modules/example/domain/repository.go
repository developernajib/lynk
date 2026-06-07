package domain

import (
	"context"

	"github.com/developernajib/lynk/services/core/internal/modules/example/domain/vo"
)

// NoteRepository is the persistence port, declared in the domain so the
// dependency points inward: infrastructure implements it, the domain never
// sees SQL. One repository per aggregate root; sub-entities (none here)
// would persist through it in the same transaction.
//
// tenantID is an explicit parameter on reads so tenancy scoping can never be
// forgotten silently.
type NoteRepository interface {
	// Create inserts a new note.
	Create(ctx context.Context, note *Note) error
	// Get loads one note within a tenant, or ErrNoteNotFound.
	Get(ctx context.Context, tenantID string, id vo.NoteID) (*Note, error)
	// Update persists changes with an optimistic-lock guard, returning
	// ErrConcurrentUpdate when another writer won.
	Update(ctx context.Context, note *Note) error
	// List pages an owner's notes, newest first.
	List(ctx context.Context, tenantID, ownerID string, limit, offset int32) ([]*Note, error)
}
