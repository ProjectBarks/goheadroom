package smartcrusher

import (
	"encoding/json"
)

// Constraint is a hard preservation constraint for indices the allocator must keep.
type Constraint interface {
	Name() string
	MustKeep(items []json.RawMessage, itemStrings []string) []int
}

// CrushEvent is a telemetry event emitted after each crush call.
type CrushEvent struct {
	Strategy    string
	InputBytes  int
	OutputBytes int
	ElapsedNs   uint64
	WasModified bool
}

// Observer is a decision-stream hook called after each crush.
type Observer interface {
	Name() string
	OnEvent(event *CrushEvent)
}
