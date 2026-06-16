package smartcrusher

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeepErrorsConstraintFindsErrorItems(t *testing.T) {
	items := make([]json.RawMessage, 10)
	for i := 0; i < 9; i++ {
		items[i], _ = json.Marshal(map[string]interface{}{"id": i, "status": "ok"})
	}
	items[9], _ = json.Marshal(map[string]interface{}{"id": 9, "status": "ERROR", "msg": "FATAL: boom"})
	kept := KeepErrorsConstraint{}.MustKeep(items, nil)
	assert.Contains(t, kept, 9, "error item must be flagged for keep")
}

func TestKeepErrorsConstraintUsesItemStrings(t *testing.T) {
	items := []json.RawMessage{
		[]byte(`{"a":1}`),
		[]byte(`{"a":"exception"}`),
	}
	strings := make([]string, len(items))
	for i, item := range items {
		strings[i] = string(item)
	}
	withCache := KeepErrorsConstraint{}.MustKeep(items, strings)
	withoutCache := KeepErrorsConstraint{}.MustKeep(items, nil)
	assert.Equal(t, withCache, withoutCache)
	assert.Contains(t, withCache, 1)
}

func TestKeepStructuralOutliersConstraintReturnsIndices(t *testing.T) {
	items := make([]json.RawMessage, 21)
	for i := 0; i < 20; i++ {
		items[i], _ = json.Marshal(map[string]interface{}{"id": i, "kind": "common"})
	}
	items[20], _ = json.Marshal(map[string]interface{}{"id": 20, "kind": "common", "rare_extra_field": "x"})
	kept := KeepStructuralOutliersConstraint{}.MustKeep(items, nil)
	assert.Contains(t, kept, 20, "item with rare field should be a structural outlier")
}

func TestDefaultOSSConstraintsReturnsTwo(t *testing.T) {
	cs := DefaultOSSConstraints()
	assert.Len(t, cs, 2)
	names := make([]string, len(cs))
	for i, c := range cs {
		names[i] = c.Name()
	}
	assert.Equal(t, []string{"keep_errors", "keep_structural_outliers"}, names)
}

func TestConstraintsHandleEmptyArray(t *testing.T) {
	assert.Empty(t, KeepErrorsConstraint{}.MustKeep(nil, nil))
	assert.Empty(t, KeepStructuralOutliersConstraint{}.MustKeep(nil, nil))
}
