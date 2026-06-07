package domain

import "time"

// Event is a fact the aggregate records for the outside world. Events are
// FAT (they carry the data consumers need) so downstream projections never
// call back to this module.
type Event interface {
	// Subject is the bus routing key, e.g. "example.note.created".
	Subject() string
}

// NoteCreated is raised once per new note.
type NoteCreated struct {
	NoteID     string
	TenantID   string
	OwnerID    string
	Title      string
	Body       string
	OccurredAt time.Time
}

// Subject implements Event.
func (NoteCreated) Subject() string { return "example.note.created" }

// NoteUpdated is raised on every successful update.
type NoteUpdated struct {
	NoteID     string
	TenantID   string
	Title      string
	Body       string
	Version    int64
	OccurredAt time.Time
}

// Subject implements Event.
func (NoteUpdated) Subject() string { return "example.note.updated" }
