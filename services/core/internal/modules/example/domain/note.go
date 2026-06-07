package domain

import (
	"errors"
	"time"

	"github.com/developernajib/lynk/services/core/internal/modules/example/domain/vo"
)

// ErrMissingOwner guards the aggregate invariant that every note belongs to
// a tenant and an owner.
var ErrMissingOwner = errors.New("note requires tenant and owner")

// Note is the aggregate root. Fields are private so every mutation goes
// through a method that maintains invariants and records events; state can
// never be poked into an invalid shape from outside.
type Note struct {
	id        vo.NoteID
	tenantID  string
	ownerID   string
	title     vo.Title
	body      string
	version   int64
	createdAt time.Time
	updatedAt time.Time

	events []Event
}

// NewNote is the validating factory for a brand-new note. It records the
// NoteCreated event; persistence publishes it through the outbox in the same
// transaction.
func NewNote(id vo.NoteID, tenantID, ownerID string, title vo.Title, body string, now time.Time) (*Note, error) {
	if tenantID == "" || ownerID == "" {
		return nil, ErrMissingOwner
	}

	note := &Note{
		id:        id,
		tenantID:  tenantID,
		ownerID:   ownerID,
		title:     title,
		body:      body,
		version:   1,
		createdAt: now,
		updatedAt: now,
	}
	note.events = append(note.events, NoteCreated{
		NoteID:     id.String(),
		TenantID:   tenantID,
		OwnerID:    ownerID,
		Title:      title.String(),
		Body:       body,
		OccurredAt: now,
	})
	return note, nil
}

// NoteFromState rehydrates a note loaded from storage. It deliberately skips
// validation and raises no events: the stored state was validated when it
// was written, and loading is not a domain fact.
func NoteFromState(id vo.NoteID, tenantID, ownerID string, title vo.Title, body string, version int64, createdAt, updatedAt time.Time) *Note {
	return &Note{
		id:        id,
		tenantID:  tenantID,
		ownerID:   ownerID,
		title:     title,
		body:      body,
		version:   version,
		createdAt: createdAt,
		updatedAt: updatedAt,
	}
}

// Update applies a new title and body, bumping updatedAt and recording the
// event. The version is bumped by the repository's guarded UPDATE, not here,
// so the in-memory value always mirrors what the database proved.
func (n *Note) Update(title vo.Title, body string, now time.Time) {
	n.title = title
	n.body = body
	n.updatedAt = now
	n.events = append(n.events, NoteUpdated{
		NoteID:     n.id.String(),
		TenantID:   n.tenantID,
		Title:      title.String(),
		Body:       body,
		Version:    n.version + 1,
		OccurredAt: now,
	})
}

// PullEvents returns and clears the recorded events; called by the use case
// after a successful save so each event publishes exactly once.
func (n *Note) PullEvents() []Event {
	events := n.events
	n.events = nil
	return events
}

// ID returns the note id.
func (n *Note) ID() vo.NoteID { return n.id }

// TenantID returns the owning tenant.
func (n *Note) TenantID() string { return n.tenantID }

// OwnerID returns the owning user.
func (n *Note) OwnerID() string { return n.ownerID }

// Title returns the current title.
func (n *Note) Title() vo.Title { return n.title }

// Body returns the current body.
func (n *Note) Body() string { return n.body }

// Version returns the optimistic-locking version.
func (n *Note) Version() int64 { return n.version }

// CreatedAt returns the creation time.
func (n *Note) CreatedAt() time.Time { return n.createdAt }

// UpdatedAt returns the last-modified time.
func (n *Note) UpdatedAt() time.Time { return n.updatedAt }
