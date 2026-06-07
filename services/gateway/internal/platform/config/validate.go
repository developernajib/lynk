package config

import (
	"fmt"
	"strings"
)

// Validation accumulates configuration problems so startup can report every
// missing or invalid setting at once instead of failing one variable at a
// time across repeated restarts.
type Validation struct {
	problems []string
}

// NewValidation returns an empty Validation.
func NewValidation() *Validation {
	return &Validation{}
}

// Require records a problem when value is blank, naming the environment
// variable the operator must set.
func (v *Validation) Require(envName, value string) {
	if strings.TrimSpace(value) == "" {
		v.problems = append(v.problems, "missing required environment variable "+envName)
	}
}

// Check records problem when ok is false, for arbitrary invariants such as
// numeric ranges.
func (v *Validation) Check(ok bool, problem string) {
	if !ok {
		v.problems = append(v.problems, problem)
	}
}

// Err returns nil when no problems were recorded, otherwise a single error
// listing all of them.
func (v *Validation) Err() error {
	if len(v.problems) == 0 {
		return nil
	}
	return fmt.Errorf("config: %s", strings.Join(v.problems, "; "))
}
