// Package domain is the example module's core: the Note aggregate, its value
// objects, events, errors, and the repository port. It imports only the
// standard library, so business rules stay portable and transport-ignorant;
// adapters translate these sentinels into status codes at the boundary.
package domain

import "errors"

// ErrNoteNotFound means the id does not exist within the caller's tenant.
var ErrNoteNotFound = errors.New("note not found")

// ErrConcurrentUpdate means another writer won the optimistic-lock race; the
// caller should re-read and retry with the fresh version.
var ErrConcurrentUpdate = errors.New("note was modified concurrently")
