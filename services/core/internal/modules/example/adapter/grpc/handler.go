// Package grpc adapts the example module's use cases to its proto contract.
// The handler's shape is uniform: resolve the principal, call the use case
// with primitives, classify domain errors into apperror, map to proto. The
// platform's error-mapping interceptor does the rest.
package grpc

import (
	"context"
	"errors"

	"google.golang.org/protobuf/types/known/timestamppb"

	examplev1 "github.com/developernajib/lynk/services/core/internal/gen/proto/example/v1"
	"github.com/developernajib/lynk/services/core/internal/modules/example/application"
	"github.com/developernajib/lynk/services/core/internal/modules/example/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/example/domain/vo"
	"github.com/developernajib/lynk/services/core/internal/platform/apperror"
	"github.com/developernajib/lynk/services/core/internal/platform/auth"
)

// Handler implements example.v1.ExampleService.
type Handler struct {
	examplev1.UnimplementedExampleServiceServer

	createNote *application.CreateNote
	getNote    *application.GetNote
	updateNote *application.UpdateNote
	listNotes  *application.ListNotes
}

// NewHandler wires the handler.
func NewHandler(create *application.CreateNote, get *application.GetNote, update *application.UpdateNote, list *application.ListNotes) *Handler {
	return &Handler{createNote: create, getNote: get, updateNote: update, listNotes: list}
}

// CreateNote creates a note owned by the authenticated principal.
func (h *Handler) CreateNote(ctx context.Context, req *examplev1.CreateNoteRequest) (*examplev1.CreateNoteResponse, error) {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}

	note, err := h.createNote.Execute(ctx, principal.UserID, req.GetTitle(), req.GetBody())
	if err != nil {
		return nil, classify(err)
	}
	return &examplev1.CreateNoteResponse{Note: toProto(note)}, nil
}

// GetNote returns one note by id.
func (h *Handler) GetNote(ctx context.Context, req *examplev1.GetNoteRequest) (*examplev1.GetNoteResponse, error) {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}

	note, err := h.getNote.Execute(ctx, principal.UserID, req.GetId())
	if err != nil {
		return nil, classify(err)
	}
	return &examplev1.GetNoteResponse{Note: toProto(note)}, nil
}

// UpdateNote retitles or rewrites a note under optimistic locking.
func (h *Handler) UpdateNote(ctx context.Context, req *examplev1.UpdateNoteRequest) (*examplev1.UpdateNoteResponse, error) {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}

	note, err := h.updateNote.Execute(ctx, principal.UserID, req.GetId(), req.GetTitle(), req.GetBody(), req.GetVersion())
	if err != nil {
		return nil, classify(err)
	}
	return &examplev1.UpdateNoteResponse{Note: toProto(note)}, nil
}

// ListNotes pages the principal's notes.
func (h *Handler) ListNotes(ctx context.Context, req *examplev1.ListNotesRequest) (*examplev1.ListNotesResponse, error) {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}

	notes, err := h.listNotes.Execute(ctx, principal.UserID, req.GetLimit(), req.GetOffset())
	if err != nil {
		return nil, classify(err)
	}

	protoNotes := make([]*examplev1.Note, 0, len(notes))
	for _, note := range notes {
		protoNotes = append(protoNotes, toProto(note))
	}
	return &examplev1.ListNotesResponse{Notes: protoNotes}, nil
}

// requirePrincipal is this module's auth guard: every RPC here needs a
// logged-in caller. Open endpoints would simply skip the guard.
func requirePrincipal(ctx context.Context) (auth.Principal, error) {
	principal, ok := auth.FromContext(ctx)
	if !ok {
		return auth.Principal{}, apperror.New(apperror.KindUnauthenticated, "unauthenticated", "authentication required")
	}
	return principal, nil
}

// classify maps domain sentinels onto transport-agnostic kinds, in exactly
// one place per module.
func classify(err error) error {
	switch {
	case errors.Is(err, domain.ErrNoteNotFound):
		return apperror.Wrap(err, apperror.KindNotFound, "note_not_found", "note not found")
	case errors.Is(err, domain.ErrConcurrentUpdate):
		return apperror.Wrap(err, apperror.KindConflict, "note_conflict", "note was modified concurrently, re-read and retry")
	case errors.Is(err, vo.ErrInvalidNoteID), errors.Is(err, vo.ErrInvalidTitle), errors.Is(err, domain.ErrMissingOwner):
		return apperror.Wrap(err, apperror.KindInvalidInput, "invalid_input", err.Error())
	default:
		return apperror.Wrap(err, apperror.KindInternal, "internal", "internal error")
	}
}

func toProto(note *domain.Note) *examplev1.Note {
	return &examplev1.Note{
		Id:        note.ID().String(),
		Title:     note.Title().String(),
		Body:      note.Body(),
		Version:   note.Version(),
		CreatedAt: timestamppb.New(note.CreatedAt()),
		UpdatedAt: timestamppb.New(note.UpdatedAt()),
	}
}
