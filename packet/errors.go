package packet

import (
	"errors"
	"fmt"
)

var (
	ErrAlreadyExists           = errors.New("packet already exists")
	ErrNotFound                = errors.New("packet not found")
	ErrConflict                = errors.New("concurrent modification")
	ErrProjectionUnavailable   = errors.New("projection unavailable")
	ErrInvalidEvent            = errors.New("invalid event")
	ErrIllegalTransition       = errors.New("illegal status transition")
	ErrEvidenceRequired        = errors.New("done transition requires evidence")
	ErrUnexpectedEvidence      = errors.New("evidence is allowed only on a done transition")
	ErrClosed                  = errors.New("packet is closed")
	ErrTenantValidatorRequired = errors.New("tenant validator is required")
)

// ConflictError reports the stale version that caused a mutation to be rejected.
type ConflictError struct {
	PacketID PacketID
	Expected Version
	Actual   Version
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s: packet %q expected version %d, actual version %d", ErrConflict, e.PacketID, e.Expected, e.Actual)
}

func (e *ConflictError) Unwrap() error { return ErrConflict }
