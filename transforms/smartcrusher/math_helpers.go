package smartcrusher

import "math"

// sqrt returns the square root of x.
func sqrt(x float64) float64 { return math.Sqrt(x) }

// abs returns the absolute value of x.
func abs(x float64) float64 { return math.Abs(x) }

// isFinite returns true if x is not NaN or Inf.
func isFinite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
