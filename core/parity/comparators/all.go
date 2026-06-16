package comparators

import "github.com/projectbarks/goheadroom/core/parity"

// AllComparators returns all parity comparators sorted alphabetically by name.
func AllComparators() []parity.Comparator {
	return []parity.Comparator{
		CacheAligner{},
		CCR{},
		CodeCompressor{},
		ContentDetector{},
		DiffCompressor{},
		E2EMutated{},
		E2EUnmutated{},
		JSONCompressor{},
		LogCompressor{},
		SearchCompressor{},
		SmartCrusher{},
		Tokenizer{},
	}
}

// DefaultRegistry returns a Registry pre-loaded with all comparators.
func DefaultRegistry() *parity.Registry {
	r := parity.NewRegistry()
	for _, c := range AllComparators() {
		r.Register(c)
	}
	return r
}
