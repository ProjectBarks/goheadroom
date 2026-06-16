package smartcrusher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTracingObserverDoesNotPanicOnEvent(t *testing.T) {
	event := &CrushEvent{
		Strategy:    "passthrough",
		InputBytes:  100,
		OutputBytes: 100,
		ElapsedNs:   0,
		WasModified: false,
	}
	obs := TracingObserver{}
	obs.OnEvent(event) // must not panic
}

func TestTracingObserverNameIsStable(t *testing.T) {
	assert.Equal(t, "tracing", TracingObserver{}.Name())
}
