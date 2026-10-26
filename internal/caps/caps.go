// Package caps holds the output caps the tools share with the settings.
//
// A tool's output is capped relative to the model's context window: the
// cap is a fraction of the window in characters, and never above a fixed
// maximum. The fractions and the maximums are here; the caller supplies
// the characters-per-token scale, so the one estimate lives with the
// session's estimator (poo.CharsPerToken) and a recalibration moves
// every consumer.
package caps

// ObservationMax is the largest observation the model ever sees.
const ObservationMax = 30000

// ReadOutputMax is the largest read output the model ever sees.
const ReadOutputMax = 70000

// Observation returns the cap on the observation shown to the model.
//
// The cap is context-relative: min(30000, floor(0.06 × context_limit ×
// charsPerToken)) characters.
func Observation(contextLimit int, charsPerToken float64) int {
	return relative(0.06, contextLimit, ObservationMax, charsPerToken)
}

// ReadOutput returns the cap on a read's output.
//
// The cap is context-relative: min(70000, floor(0.10 × context_limit ×
// charsPerToken)) characters.
func ReadOutput(contextLimit int, charsPerToken float64) int {
	return relative(0.10, contextLimit, ReadOutputMax, charsPerToken)
}

// relative returns a fraction of the window in characters, never above
// the maximum.
func relative(fraction float64, contextLimit, max int, charsPerToken float64) int {
	cap := int(fraction * float64(contextLimit) * charsPerToken)
	if cap > max {
		return max
	}
	if cap < 0 {
		return 0
	}
	return cap
}
