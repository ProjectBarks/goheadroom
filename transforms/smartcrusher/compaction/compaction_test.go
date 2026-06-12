package compaction

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============ IR Tests ============

func TestOpaqueKindString(t *testing.T) {
	assert.Equal(t, "base64", OpaqueBase64Blob.String())
	assert.Equal(t, "string", OpaqueLongString.String())
	assert.Equal(t, "html", OpaqueHTMLChunk.String())
	assert.Equal(t, "other", OpaqueOther.String())
}

func TestSchemaFieldNames(t *testing.T) {
	s := Schema{Fields: []FieldSpec{
		{Name: "id", TypeTag: "int"},
		{Name: "name", TypeTag: "string"},
	}}
	assert.Equal(t, []string{"id", "name"}, s.FieldNames())
}

func TestUntouchedIsNotCompacted(t *testing.T) {
	c := &Compaction{Kind: CompactionUntouched, Original: []interface{}{1, 2, 3}}
	assert.False(t, c.WasCompacted())
	assert.Equal(t, 0, c.KeptRowCount())
	assert.Equal(t, 0, c.OriginalRowCount())
}

func TestTableRowCounts(t *testing.T) {
	c := &Compaction{
		Kind:          CompactionTable,
		Schema:        Schema{Fields: nil},
		Rows:          []Row{NewRow(nil), NewRow(nil)},
		OriginalCount: 5,
	}
	assert.True(t, c.WasCompacted())
	assert.Equal(t, 2, c.KeptRowCount())
	assert.Equal(t, 5, c.OriginalRowCount())
}

func TestBucketsAggregateRowCounts(t *testing.T) {
	c := &Compaction{
		Kind:          CompactionBuckets,
		Discriminator: "type",
		Buckets: []Bucket{
			{Key: "user", Schema: Schema{}, Rows: []Row{NewRow(nil), NewRow(nil)}},
			{Key: "order", Schema: Schema{}, Rows: []Row{NewRow(nil)}},
		},
		OriginalCount: 10,
	}
	assert.Equal(t, 3, c.KeptRowCount())
	assert.Equal(t, 10, c.OriginalRowCount())
}

func TestCellMissingDistinctFromScalarNull(t *testing.T) {
	m := NewMissingCell()
	n := NewScalarCell(nil)
	assert.NotEqual(t, m.Kind, n.Kind)
}

func TestRowNewAndLen(t *testing.T) {
	r := NewRow([]CellValue{NewScalarCell(1), NewScalarCell("hi")})
	assert.Equal(t, 2, len(r.Cells))
}

// ============ Classifier Tests ============

func TestClassifyCellNull(t *testing.T) {
	assert.Equal(t, CellNull, ClassifyCell(nil, nil))
}

func TestClassifyCellBool(t *testing.T) {
	assert.Equal(t, CellBool, ClassifyCell(true, nil))
	assert.Equal(t, CellBool, ClassifyCell(false, nil))
}

func TestClassifyCellInt(t *testing.T) {
	assert.Equal(t, CellInt, ClassifyCell(float64(42), nil))
}

func TestClassifyCellFloat(t *testing.T) {
	assert.Equal(t, CellFloat, ClassifyCell(3.14, nil))
}

func TestClassifyCellShortString(t *testing.T) {
	cfg := DefaultClassifyConfig()
	assert.Equal(t, CellShortStr, ClassifyCell("hello", &cfg))
}

func TestClassifyCellUUID(t *testing.T) {
	cfg := DefaultClassifyConfig()
	assert.Equal(t, CellUUID, ClassifyCell("550e8400-e29b-41d4-a716-446655440000", &cfg))
}

func TestClassifyCellURL(t *testing.T) {
	cfg := DefaultClassifyConfig()
	assert.Equal(t, CellURL, ClassifyCell("https://example.com/path", &cfg))
}

func TestClassifyCellLongString(t *testing.T) {
	cfg := DefaultClassifyConfig()
	long := strings.Repeat("x", 150)
	assert.Equal(t, CellLongStr, ClassifyCell(long, &cfg))
}

func TestClassifyCellArray(t *testing.T) {
	assert.Equal(t, CellArray, ClassifyCell([]interface{}{1, 2, 3}, nil))
}

func TestClassifyCellObject(t *testing.T) {
	assert.Equal(t, CellObject, ClassifyCell(map[string]interface{}{"k": "v"}, nil))
}

func TestClassifyCellOpaque(t *testing.T) {
	cfg := DefaultClassifyConfig()
	b64 := strings.Repeat("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef", 20)
	assert.Equal(t, CellOpaque, ClassifyCell(b64, &cfg))
}

// ============ Formatter Tests ============

func makeTestSchema() Schema {
	return Schema{Fields: []FieldSpec{
		{Name: "id", TypeTag: "int", Nullable: false},
		{Name: "name", TypeTag: "string", Nullable: false},
	}}
}

func makeTestRow(id int, name string) Row {
	return NewRow([]CellValue{NewScalarCell(float64(id)), NewScalarCell(name)})
}

// --- CSVSchemaFormatter ---

func TestCSVFormatterFormatRow(t *testing.T) {
	f := &CSVSchemaFormatter{}
	schema := makeTestSchema()
	row := makeTestRow(1, "alice")
	result := f.FormatRow(&schema, &row)
	assert.Equal(t, "1,alice", result)
}

func TestCSVFormatterFormatCompaction(t *testing.T) {
	f := &CSVSchemaFormatter{}
	c := &Compaction{
		Kind:          CompactionTable,
		Schema:        makeTestSchema(),
		Rows:          []Row{makeTestRow(1, "alice"), makeTestRow(2, "bob")},
		OriginalCount: 2,
	}
	result := f.FormatCompaction(c)
	assert.Contains(t, result, "[2]")
	assert.Contains(t, result, "id:int")
	assert.Contains(t, result, "name:string")
	assert.Contains(t, result, "1,alice")
	assert.Contains(t, result, "2,bob")
}

func TestCSVFormatterMissingCell(t *testing.T) {
	f := &CSVSchemaFormatter{}
	schema := makeTestSchema()
	row := NewRow([]CellValue{NewScalarCell(float64(1)), NewMissingCell()})
	result := f.FormatRow(&schema, &row)
	assert.Equal(t, "1,", result)
}

func TestCSVFormatterQuotesStringsWithComma(t *testing.T) {
	f := &CSVSchemaFormatter{}
	schema := makeTestSchema()
	row := NewRow([]CellValue{NewScalarCell(float64(1)), NewScalarCell("hello, world")})
	result := f.FormatRow(&schema, &row)
	assert.Contains(t, result, `"hello, world"`)
}

func TestCSVFormatterNullableFields(t *testing.T) {
	schema := Schema{Fields: []FieldSpec{
		{Name: "id", TypeTag: "int", Nullable: true},
	}}
	f := &CSVSchemaFormatter{}
	c := &Compaction{
		Kind:          CompactionTable,
		Schema:        schema,
		Rows:          []Row{NewRow([]CellValue{NewScalarCell(float64(1))})},
		OriginalCount: 1,
	}
	result := f.FormatCompaction(c)
	assert.Contains(t, result, "id:int?")
}

func TestCSVFormatterBoolValues(t *testing.T) {
	f := &CSVSchemaFormatter{}
	schema := Schema{Fields: []FieldSpec{{Name: "active", TypeTag: "bool"}}}
	row := NewRow([]CellValue{NewScalarCell(true)})
	result := f.FormatRow(&schema, &row)
	assert.Equal(t, "true", result)
}

// --- JSONFormatter ---

func TestJSONFormatterFormatRow(t *testing.T) {
	f := &JSONFormatter{}
	schema := makeTestSchema()
	row := makeTestRow(1, "alice")
	result := f.FormatRow(&schema, &row)
	var obj map[string]interface{}
	err := json.Unmarshal([]byte(result), &obj)
	require.NoError(t, err)
	assert.Equal(t, float64(1), obj["id"])
	assert.Equal(t, "alice", obj["name"])
}

// --- MarkdownKVFormatter ---

func TestMarkdownKVFormatterFormatRow(t *testing.T) {
	f := &MarkdownKVFormatter{}
	schema := makeTestSchema()
	row := makeTestRow(1, "alice")
	result := f.FormatRow(&schema, &row)
	assert.Contains(t, result, "**id**")
	assert.Contains(t, result, "**name**")
	assert.Contains(t, result, "1")
	assert.Contains(t, result, "alice")
}

func TestMarkdownKVFormatterFormatCompaction(t *testing.T) {
	f := &MarkdownKVFormatter{}
	c := &Compaction{
		Kind:   CompactionTable,
		Schema: makeTestSchema(),
		Rows:   []Row{makeTestRow(1, "alice"), makeTestRow(2, "bob")},
	}
	result := f.FormatCompaction(c)
	assert.Contains(t, result, "### Item 1")
	assert.Contains(t, result, "### Item 2")
}

// ============ Compactor Tests ============

func TestCompactEmpty(t *testing.T) {
	tc := NewTabularCompactor()
	c := tc.Compact(nil)
	assert.Equal(t, CompactionUntouched, c.Kind)
}

func TestCompactSingleRow(t *testing.T) {
	tc := NewTabularCompactor()
	items := []interface{}{map[string]interface{}{"id": float64(1)}}
	c := tc.Compact(items)
	assert.Equal(t, CompactionUntouched, c.Kind)
}

func TestCompactMultipleRows(t *testing.T) {
	tc := NewTabularCompactor()
	items := []interface{}{
		map[string]interface{}{"id": float64(1), "name": "alice"},
		map[string]interface{}{"id": float64(2), "name": "bob"},
		map[string]interface{}{"id": float64(3), "name": "carol"},
	}
	c := tc.Compact(items)
	assert.Equal(t, CompactionTable, c.Kind)
	assert.Equal(t, 3, c.KeptRowCount())
	assert.Equal(t, 3, c.OriginalRowCount())
	assert.True(t, c.WasCompacted())
}

func TestCompactNonObjectReturnsUntouched(t *testing.T) {
	tc := NewTabularCompactor()
	items := []interface{}{float64(1), float64(2), float64(3)}
	c := tc.Compact(items)
	assert.Equal(t, CompactionUntouched, c.Kind)
}

func TestCompactSchemaBuild(t *testing.T) {
	tc := NewTabularCompactor()
	items := []interface{}{
		map[string]interface{}{"id": float64(1), "name": "alice"},
		map[string]interface{}{"id": float64(2), "name": "bob"},
	}
	c := tc.Compact(items)
	require.Equal(t, CompactionTable, c.Kind)
	names := c.Schema.FieldNames()
	assert.Contains(t, names, "id")
	assert.Contains(t, names, "name")
}

func TestCompactMissingFields(t *testing.T) {
	tc := NewTabularCompactor()
	items := []interface{}{
		map[string]interface{}{"id": float64(1), "name": "alice"},
		map[string]interface{}{"id": float64(2)},
	}
	c := tc.Compact(items)
	require.Equal(t, CompactionTable, c.Kind)
	// name field should be nullable since row 2 is missing it.
	for _, f := range c.Schema.Fields {
		if f.Name == "name" {
			assert.True(t, f.Nullable)
		}
	}
	// Row 2 should have a Missing cell for name.
	nameIdx := -1
	for i, f := range c.Schema.Fields {
		if f.Name == "name" {
			nameIdx = i
		}
	}
	require.GreaterOrEqual(t, nameIdx, 0)
	assert.Equal(t, KindMissing, c.Rows[1].Cells[nameIdx].Kind)
}

func TestCompactNullFieldNullable(t *testing.T) {
	tc := NewTabularCompactor()
	items := []interface{}{
		map[string]interface{}{"id": float64(1), "val": nil},
		map[string]interface{}{"id": float64(2), "val": "hi"},
	}
	c := tc.Compact(items)
	require.Equal(t, CompactionTable, c.Kind)
	for _, f := range c.Schema.Fields {
		if f.Name == "val" {
			assert.True(t, f.Nullable)
		}
	}
}

// ============ CompactionStage Tests ============

func TestCompactionStageRun(t *testing.T) {
	stage := NewDefaultCompactionStage()
	items := []interface{}{
		map[string]interface{}{"id": float64(1), "name": "alice"},
		map[string]interface{}{"id": float64(2), "name": "bob"},
	}
	c, rendered := stage.Run(items)
	assert.True(t, c.WasCompacted())
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "id:")
}

func TestCompactionStageDeclines(t *testing.T) {
	stage := NewDefaultCompactionStage()
	items := []interface{}{float64(1)}
	c, rendered := stage.Run(items)
	assert.False(t, c.WasCompacted())
	assert.Empty(t, rendered)
}

// ============ TryParseJSONContainer Tests ============

func TestTryParseJSONContainerObject(t *testing.T) {
	result := TryParseJSONContainer(`{"a": 1}`)
	require.NotNil(t, result)
	obj, ok := result.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, float64(1), obj["a"])
}

func TestTryParseJSONContainerArray(t *testing.T) {
	result := TryParseJSONContainer(`[1, 2, 3]`)
	require.NotNil(t, result)
	arr, ok := result.([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 3, len(arr))
}

func TestTryParseJSONContainerScalar(t *testing.T) {
	assert.Nil(t, TryParseJSONContainer("123"))
	assert.Nil(t, TryParseJSONContainer(`"hello"`))
}

func TestTryParseJSONContainerInvalid(t *testing.T) {
	assert.Nil(t, TryParseJSONContainer("not json"))
	assert.Nil(t, TryParseJSONContainer("{malformed"))
}

// ============ DocumentCompactor (Walker) Tests ============

func TestDocumentCompactorNew(t *testing.T) {
	dc := NewDocumentCompactor()
	require.NotNil(t, dc)
	require.NotNil(t, dc.Compactor)
	require.NotNil(t, dc.Formatter)
}

func TestDocumentCompactorWalkEmpty(t *testing.T) {
	dc := NewDocumentCompactor()
	result, modified := dc.Walk(nil)
	assert.Nil(t, result)
	assert.False(t, modified)
}

func TestDocumentCompactorWalkScalar(t *testing.T) {
	dc := NewDocumentCompactor()
	result, modified := dc.Walk(float64(42))
	assert.Equal(t, float64(42), result)
	assert.False(t, modified)
}

func TestDocumentCompactorWalkFlatObject(t *testing.T) {
	dc := NewDocumentCompactor()
	obj := map[string]interface{}{"id": float64(1), "name": "alice"}
	result, modified := dc.Walk(obj)
	assert.False(t, modified)
	resultObj, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(1), resultObj["id"])
}

func TestDocumentCompactorWalkNestedArray(t *testing.T) {
	dc := NewDocumentCompactor()
	items := make([]interface{}, 10)
	for i := range items {
		items[i] = map[string]interface{}{"id": float64(i), "name": fmt.Sprintf("user_%d", i)}
	}
	doc := map[string]interface{}{"data": items}
	result, modified := dc.Walk(doc)
	// Array of 10 objects should be compacted.
	assert.True(t, modified)
	resultObj, ok := result.(map[string]interface{})
	require.True(t, ok)
	// The compacted array should now be a string (rendered CSV).
	_, isStr := resultObj["data"].(string)
	assert.True(t, isStr, "compacted array should be rendered as string")
}

func TestDocumentCompactorWalkSmallArray(t *testing.T) {
	dc := NewDocumentCompactor()
	items := []interface{}{float64(1), float64(2), float64(3)}
	result, modified := dc.Walk(items)
	assert.False(t, modified)
	arr, ok := result.([]interface{})
	require.True(t, ok)
	assert.Equal(t, 3, len(arr))
}

func TestDocumentCompactorWalkNonObjectArray(t *testing.T) {
	dc := NewDocumentCompactor()
	items := make([]interface{}, 10)
	for i := range items {
		items[i] = float64(i)
	}
	result, modified := dc.Walk(items)
	// Non-object arrays are not compacted by TabularCompactor.
	assert.False(t, modified)
	arr, ok := result.([]interface{})
	require.True(t, ok)
	assert.Equal(t, 10, len(arr))
}

// ============ EmitOpaqueCCRMarker Tests ============

func TestEmitOpaqueCCRMarker(t *testing.T) {
	marker := EmitOpaqueCCRMarker("test content", OpaqueBase64Blob)
	assert.True(t, strings.HasPrefix(marker, "<<ccr:"))
	assert.Contains(t, marker, "base64")
	assert.Contains(t, marker, ">>")
}

func TestEmitOpaqueCCRMarkerDeterministic(t *testing.T) {
	m1 := EmitOpaqueCCRMarker("same content", OpaqueLongString)
	m2 := EmitOpaqueCCRMarker("same content", OpaqueLongString)
	assert.Equal(t, m1, m2)
}
