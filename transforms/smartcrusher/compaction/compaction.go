// Package compaction implements lossless JSON array compaction into tabular
// and bucketed intermediate representations with CSV, JSON, and Markdown
// output formatters.
package compaction

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// OpaqueKind identifies the type of opaque payload substituted by CCR.
type OpaqueKind int

const (
	OpaqueBase64Blob OpaqueKind = iota
	OpaqueLongString
	OpaqueHTMLChunk
	OpaqueOther
)

func (k OpaqueKind) String() string {
	switch k {
	case OpaqueBase64Blob:
		return "base64"
	case OpaqueLongString:
		return "string"
	case OpaqueHTMLChunk:
		return "html"
	case OpaqueOther:
		return "other"
	default:
		return "unknown"
	}
}

// FieldSpec describes one column in a tabular compaction.
type FieldSpec struct {
	Name     string
	TypeTag  string // "int", "float", "string", "bool", "null", "json", "ccr"
	Nullable bool
}

// Schema is the column set for a homogeneous table.
type Schema struct {
	Fields []FieldSpec
}

// FieldNames returns column names in order.
func (s *Schema) FieldNames() []string {
	names := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		names[i] = f.Name
	}
	return names
}

// CellValue is one cell in a row.
type CellValue struct {
	Kind     CellValueKind
	Scalar   interface{}       // for KindScalar
	Nested   *Compaction       // for KindNested
	CCRHash  string            // for KindOpaqueRef
	ByteSize int               // for KindOpaqueRef
	OpaqueK  OpaqueKind        // for KindOpaqueRef
}

// CellValueKind discriminates CellValue variants.
type CellValueKind int

const (
	KindScalar    CellValueKind = iota
	KindNested
	KindOpaqueRef
	KindMissing
)

// NewScalarCell creates a scalar cell.
func NewScalarCell(v interface{}) CellValue {
	return CellValue{Kind: KindScalar, Scalar: v}
}

// NewMissingCell creates a missing cell.
func NewMissingCell() CellValue {
	return CellValue{Kind: KindMissing}
}

// Row is a row of a tabular compaction.
type Row struct {
	Cells []CellValue
}

// NewRow creates a new row.
func NewRow(cells []CellValue) Row {
	return Row{Cells: cells}
}

// Bucket is one partition of a heterogeneous array.
type Bucket struct {
	Key    interface{}
	Schema Schema
	Rows   []Row
}

// CompactionKind discriminates Compaction variants.
type CompactionKind int

const (
	CompactionTable     CompactionKind = iota
	CompactionBuckets
	CompactionOpaqueRef
	CompactionUntouched
)

// Compaction is the top-level compaction result.
type Compaction struct {
	Kind          CompactionKind
	// Table fields
	Schema        Schema
	Rows          []Row
	OriginalCount int
	// Buckets fields
	Discriminator string
	Buckets       []Bucket
	// OpaqueRef fields
	CCRHash       string
	ByteSize      int
	OpaqueK       OpaqueKind
	// Untouched field
	Original      interface{}
}

// WasCompacted returns true if compaction actually happened.
func (c *Compaction) WasCompacted() bool {
	return c.Kind == CompactionTable || c.Kind == CompactionBuckets || c.Kind == CompactionOpaqueRef
}

// KeptRowCount returns total kept rows.
func (c *Compaction) KeptRowCount() int {
	switch c.Kind {
	case CompactionTable:
		return len(c.Rows)
	case CompactionBuckets:
		total := 0
		for _, b := range c.Buckets {
			total += len(b.Rows)
		}
		return total
	default:
		return 0
	}
}

// OriginalRowCount returns original (pre-drop) row count.
func (c *Compaction) OriginalRowCount() int {
	switch c.Kind {
	case CompactionTable:
		return c.OriginalCount
	case CompactionBuckets:
		return c.OriginalCount
	default:
		return 0
	}
}

// CellClass is the classification result for a JSON value.
type CellClass int

const (
	CellNull      CellClass = iota
	CellBool
	CellInt
	CellFloat
	CellShortStr
	CellUUID
	CellURL
	CellLongStr
	CellJSON
	CellTimestamp
	CellArray
	CellObject
	CellEnum
	CellOpaque
)

// ClassifyConfig holds thresholds for cell classification.
type ClassifyConfig struct {
	LongStringThreshold int
	OpaqueThreshold     int
}

// DefaultClassifyConfig returns default classification thresholds.
func DefaultClassifyConfig() ClassifyConfig {
	return ClassifyConfig{
		LongStringThreshold: 100,
		OpaqueThreshold:     500,
	}
}

// ClassifyCell classifies a JSON value into a CellClass.
func ClassifyCell(value interface{}, cfg *ClassifyConfig) CellClass {
	if value == nil {
		return CellNull
	}
	switch v := value.(type) {
	case bool:
		return CellBool
	case float64:
		if v == float64(int64(v)) {
			return CellInt
		}
		return CellFloat
	case json.Number:
		if _, err := v.Int64(); err == nil {
			return CellInt
		}
		return CellFloat
	case string:
		return classifyString(v, cfg)
	case []interface{}:
		return CellArray
	case map[string]interface{}:
		return CellObject
	default:
		return CellJSON
	}
}

func classifyString(s string, cfg *ClassifyConfig) CellClass {
	if len(s) == 0 {
		return CellShortStr
	}
	// UUID check.
	if isUUIDLike(s) {
		return CellUUID
	}
	// URL check.
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return CellURL
	}
	// Opaque check.
	if cfg != nil && len(s) >= cfg.OpaqueThreshold && isOpaqueLike(s) {
		return CellOpaque
	}
	// Long string check.
	if cfg != nil && len(s) >= cfg.LongStringThreshold {
		return CellLongStr
	}
	return CellShortStr
}

func isUUIDLike(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

func isOpaqueLike(s string) bool {
	// Base64-like: high ratio of alphanumeric + /+=
	if len(s) < 100 {
		return false
	}
	b64Chars := 0
	for _, c := range s {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
			b64Chars++
		}
	}
	return float64(b64Chars)/float64(len(s)) > 0.8
}

// TryParseJSONContainer tries to parse a string as a JSON object or array.
// Returns nil if it's not a container.
func TryParseJSONContainer(s string) interface{} {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 2 {
		return nil
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return nil
	}
	var v interface{}
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return nil
	}
	// Verify it's actually a container.
	switch v.(type) {
	case map[string]interface{}, []interface{}:
		return v
	default:
		return nil
	}
}

// Formatter renders a Compaction to a string.
type Formatter interface {
	FormatCompaction(c *Compaction) string
	FormatRow(schema *Schema, row *Row) string
}

// CSVSchemaFormatter renders compactions as CSV with a schema header.
type CSVSchemaFormatter struct{}

func (f *CSVSchemaFormatter) FormatCompaction(c *Compaction) string {
	if c.Kind == CompactionUntouched {
		data, _ := json.Marshal(c.Original)
		return string(data)
	}
	if c.Kind == CompactionTable {
		return f.formatTable(&c.Schema, c.Rows, c.OriginalCount)
	}
	if c.Kind == CompactionBuckets {
		var parts []string
		for _, b := range c.Buckets {
			keyStr := fmt.Sprintf("%v", b.Key)
			header := fmt.Sprintf("[%s=%s]", c.Discriminator, keyStr)
			table := f.formatTable(&b.Schema, b.Rows, len(b.Rows))
			parts = append(parts, header+"\n"+table)
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func (f *CSVSchemaFormatter) formatTable(schema *Schema, rows []Row, originalCount int) string {
	var buf strings.Builder
	// Row count header.
	buf.WriteString(fmt.Sprintf("[%d]", originalCount))
	// Schema header.
	buf.WriteString("{")
	for i, field := range schema.Fields {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString(field.Name)
		buf.WriteString(":")
		buf.WriteString(field.TypeTag)
		if field.Nullable {
			buf.WriteString("?")
		}
	}
	buf.WriteString("}\n")
	// Data rows.
	for _, row := range rows {
		for i, cell := range row.Cells {
			if i > 0 {
				buf.WriteString(",")
			}
			buf.WriteString(formatCellCSV(cell))
		}
		buf.WriteString("\n")
	}
	return strings.TrimRight(buf.String(), "\n")
}

func (f *CSVSchemaFormatter) FormatRow(schema *Schema, row *Row) string {
	var parts []string
	for _, cell := range row.Cells {
		parts = append(parts, formatCellCSV(cell))
	}
	return strings.Join(parts, ",")
}

func formatCellCSV(cell CellValue) string {
	switch cell.Kind {
	case KindMissing:
		return ""
	case KindScalar:
		return formatScalarCSV(cell.Scalar)
	case KindNested:
		f := &CSVSchemaFormatter{}
		return f.FormatCompaction(cell.Nested)
	case KindOpaqueRef:
		return fmt.Sprintf("<<ccr:%s,%s,%d>>", cell.CCRHash, cell.OpaqueK, cell.ByteSize)
	default:
		return ""
	}
}

func formatScalarCSV(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		if strings.ContainsAny(val, ",\"\n") {
			return fmt.Sprintf("%q", val)
		}
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		data, _ := json.Marshal(v)
		return string(data)
	}
}

// JSONFormatter renders compactions as JSON.
type JSONFormatter struct{}

func (f *JSONFormatter) FormatCompaction(c *Compaction) string {
	data, _ := json.Marshal(c.Original)
	return string(data)
}

func (f *JSONFormatter) FormatRow(schema *Schema, row *Row) string {
	obj := make(map[string]interface{})
	for i, field := range schema.Fields {
		if i < len(row.Cells) {
			obj[field.Name] = cellToInterface(row.Cells[i])
		}
	}
	data, _ := json.Marshal(obj)
	return string(data)
}

func cellToInterface(cell CellValue) interface{} {
	switch cell.Kind {
	case KindMissing:
		return nil
	case KindScalar:
		return cell.Scalar
	default:
		return nil
	}
}

// MarkdownKVFormatter renders compactions as Markdown key-value lists.
type MarkdownKVFormatter struct{}

func (f *MarkdownKVFormatter) FormatCompaction(c *Compaction) string {
	if c.Kind == CompactionTable {
		var parts []string
		for i, row := range c.Rows {
			parts = append(parts, fmt.Sprintf("### Item %d", i+1))
			parts = append(parts, f.FormatRow(&c.Schema, &row))
		}
		return strings.Join(parts, "\n")
	}
	data, _ := json.Marshal(c.Original)
	return string(data)
}

func (f *MarkdownKVFormatter) FormatRow(schema *Schema, row *Row) string {
	var lines []string
	for i, field := range schema.Fields {
		if i < len(row.Cells) {
			lines = append(lines, fmt.Sprintf("- **%s**: %s", field.Name, formatCellCSV(row.Cells[i])))
		}
	}
	return strings.Join(lines, "\n")
}

// TabularCompactor compacts arrays of objects into tabular IR.
type TabularCompactor struct {
	Config ClassifyConfig
}

// NewTabularCompactor creates a compactor with default config.
func NewTabularCompactor() *TabularCompactor {
	return &TabularCompactor{Config: DefaultClassifyConfig()}
}

// Compact compacts an array of JSON objects into a Compaction.
func (tc *TabularCompactor) Compact(items []interface{}) *Compaction {
	if len(items) < 2 {
		return &Compaction{Kind: CompactionUntouched, Original: items}
	}

	// Check all items are objects.
	for _, item := range items {
		if _, ok := item.(map[string]interface{}); !ok {
			return &Compaction{Kind: CompactionUntouched, Original: items}
		}
	}

	// Collect all field names.
	fieldSet := make(map[string]bool)
	for _, item := range items {
		obj := item.(map[string]interface{})
		for k := range obj {
			fieldSet[k] = true
		}
	}
	fieldNames := make([]string, 0, len(fieldSet))
	for k := range fieldSet {
		fieldNames = append(fieldNames, k)
	}
	sort.Strings(fieldNames)

	// Build schema.
	schema := Schema{Fields: make([]FieldSpec, len(fieldNames))}
	for i, name := range fieldNames {
		typeTag := "string"
		nullable := false
		for _, item := range items {
			obj := item.(map[string]interface{})
			v, exists := obj[name]
			if !exists || v == nil {
				nullable = true
				continue
			}
			cls := ClassifyCell(v, &tc.Config)
			switch cls {
			case CellInt:
				typeTag = "int"
			case CellFloat:
				typeTag = "float"
			case CellBool:
				typeTag = "bool"
			default:
				typeTag = "string"
			}
		}
		schema.Fields[i] = FieldSpec{Name: name, TypeTag: typeTag, Nullable: nullable}
	}

	// Build rows.
	rows := make([]Row, len(items))
	for i, item := range items {
		obj := item.(map[string]interface{})
		cells := make([]CellValue, len(fieldNames))
		for j, name := range fieldNames {
			v, exists := obj[name]
			if !exists {
				cells[j] = NewMissingCell()
			} else {
				cells[j] = NewScalarCell(v)
			}
		}
		rows[i] = NewRow(cells)
	}

	return &Compaction{
		Kind:          CompactionTable,
		Schema:        schema,
		Rows:          rows,
		OriginalCount: len(items),
	}
}

// CompactionStage wraps a TabularCompactor + Formatter for the crush_array pipeline.
type CompactionStage struct {
	Compactor *TabularCompactor
	Formatter Formatter
}

// NewDefaultCompactionStage creates a stage with CSV-schema formatter.
func NewDefaultCompactionStage() *CompactionStage {
	return &CompactionStage{
		Compactor: NewTabularCompactor(),
		Formatter: &CSVSchemaFormatter{},
	}
}

// Run runs compaction on items and returns (compaction, rendered_string).
func (s *CompactionStage) Run(items []interface{}) (*Compaction, string) {
	c := s.Compactor.Compact(items)
	if !c.WasCompacted() {
		return c, ""
	}
	rendered := s.Formatter.FormatCompaction(c)
	return c, rendered
}

// EmitOpaqueCCRMarker produces a CCR marker for opaque content.
func EmitOpaqueCCRMarker(content string, kind OpaqueKind) string {
	return fmt.Sprintf("<<ccr:%s,%s,%d>>", hashContent(content), kind, len(content))
}

func hashContent(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)[:12]
}

// DocumentCompactor walks a JSON document and compacts arrays.
type DocumentCompactor struct {
	Compactor *TabularCompactor
	Formatter Formatter
}

// NewDocumentCompactor creates a document compactor.
func NewDocumentCompactor() *DocumentCompactor {
	return &DocumentCompactor{
		Compactor: NewTabularCompactor(),
		Formatter: &CSVSchemaFormatter{},
	}
}

// Walk processes a JSON value, compacting arrays.
func (dc *DocumentCompactor) Walk(value interface{}) (interface{}, bool) {
	return dc.walkValue(value, 0)
}

func (dc *DocumentCompactor) walkValue(value interface{}, depth int) (interface{}, bool) {
	if depth > 50 {
		return value, false
	}
	modified := false

	switch v := value.(type) {
	case []interface{}:
		if len(v) >= 5 {
			c := dc.Compactor.Compact(v)
			if c.WasCompacted() {
				rendered := dc.Formatter.FormatCompaction(c)
				return rendered, true
			}
		}
		result := make([]interface{}, len(v))
		for i, item := range v {
			processed, m := dc.walkValue(item, depth+1)
			result[i] = processed
			if m {
				modified = true
			}
		}
		return result, modified

	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, val := range v {
			processed, m := dc.walkValue(val, depth+1)
			result[k] = processed
			if m {
				modified = true
			}
		}
		return result, modified

	default:
		return value, false
	}
}
