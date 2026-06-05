// Package clock abstracts the current time behind an interface so
// application code can inject a deterministic source instead of calling
// time.Now directly, keeping time-dependent logic testable and replayable.
package clock

import "time"

// Clock supplies the current time.
type Clock interface {
	Now() time.Time
}

// System is the production Clock backed by time.Now.
type System struct{}

// Now returns the current time.
func (System) Now() time.Time { return time.Now() }
