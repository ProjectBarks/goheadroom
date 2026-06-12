package smartcrusher

import "encoding/json"

// KeepErrorsConstraint keeps items containing error keywords.
type KeepErrorsConstraint struct{}

func (k KeepErrorsConstraint) Name() string { return "keep_errors" }
func (k KeepErrorsConstraint) MustKeep(items []json.RawMessage, itemStrings []string) []int {
	return DetectErrorItemsForPreservation(items, itemStrings)
}

// KeepStructuralOutliersConstraint keeps structurally unusual items.
type KeepStructuralOutliersConstraint struct{}

func (k KeepStructuralOutliersConstraint) Name() string { return "keep_structural_outliers" }
func (k KeepStructuralOutliersConstraint) MustKeep(items []json.RawMessage, _ []string) []int {
	return DetectStructuralOutliers(items)
}

// DefaultOSSConstraints returns the default OSS constraint stack.
func DefaultOSSConstraints() []Constraint {
	return []Constraint{
		KeepErrorsConstraint{},
		KeepStructuralOutliersConstraint{},
	}
}
