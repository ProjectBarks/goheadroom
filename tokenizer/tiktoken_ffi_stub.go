//go:build !cgo

package tokenizer

import "fmt"

type TiktokenFFICounter struct{}

func newTiktokenFFI(model string) (*TiktokenFFICounter, error) {
	return nil, fmt.Errorf("tiktoken FFI requires CGO (build with CGO_ENABLED=1)")
}

func (t *TiktokenFFICounter) CountText(text string) int { return 0 }
func (t *TiktokenFFICounter) Backend() Backend           { return BackendTiktoken }
func (t *TiktokenFFICounter) Model() string              { return "" }
func (t *TiktokenFFICounter) EncodingName() string       { return "" }
