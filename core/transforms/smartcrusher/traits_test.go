package smartcrusher

import (
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------- Constraint interface tests ----------

type alwaysKeepFirst struct{}

func (a alwaysKeepFirst) Name() string { return "always_keep_first" }
func (a alwaysKeepFirst) MustKeep(items []json.RawMessage, itemStrings []string) []int {
	if len(items) == 0 {
		return nil
	}
	return []int{0}
}

func TestConstraintReturnsIndicesInBounds(t *testing.T) {
	items := []json.RawMessage{[]byte(`{"a":1}`), []byte(`{"a":2}`)}
	c := alwaysKeepFirst{}
	kept := c.MustKeep(items, nil)
	assert.Equal(t, []int{0}, kept)
	assert.Equal(t, "always_keep_first", c.Name())
}

func TestConstraintHandlesEmptyInput(t *testing.T) {
	kept := alwaysKeepFirst{}.MustKeep(nil, nil)
	assert.Empty(t, kept)
}

// ---------- Observer interface / CrushEvent tests ----------

type countingObserver struct {
	count *int64
}

func (c countingObserver) Name() string { return "counting" }
func (c countingObserver) OnEvent(event *CrushEvent) {
	atomic.AddInt64(c.count, 1)
}

func TestObserverEventCarriesStrategyAndSizes(t *testing.T) {
	var count int64
	obs := countingObserver{count: &count}
	event := &CrushEvent{
		Strategy:    "smart_sample(30->15)",
		InputBytes:  1000,
		OutputBytes: 500,
		ElapsedNs:   12345,
		WasModified: true,
	}
	obs.OnEvent(event)
	assert.Equal(t, int64(1), atomic.LoadInt64(&count))
}
