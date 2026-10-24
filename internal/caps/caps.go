// Package caps holds the output caps the tools share with the settings.
// Cap names a bound the code truncates with; Max names the constant bound
// of a Cap.
//
// A tool's output is capped relative to the model's context window: the
// cap is a fraction of the window in characters, and never above a fixed
// maximum. The fractions are the tools' own; the estimate and the
// maximums are here, so the tool that reads a cap and the build that
// sets it agree.
package caps

// CharsPerToken is the plan's estimate of characters per token.
const CharsPerToken = 3.5

// ObservationMax is the largest observation the model ever sees.
const ObservationMax = 30000

// ReadOutputMax is the largest read output the model ever sees.
const ReadOutputMax = 70000

// Observation returns the cap on the observation shown to the model.
//
// The cap is context-relative: min(30000, floor(0.06 × context_limit ×
// 3.5)) characters.
func Observation(contextLimit int) int {
	return relative(0.06, contextLimit, ObservationMax)
}

// ReadOutput returns the cap on a read's output.
//
// The cap is context-relative: min(70000, floor(0.10 × context_limit ×
// 3.5)) characters.
func ReadOutput(contextLimit int) int {
	return relative(0.10, contextLimit, ReadOutputMax)
}

// relative returns a fraction of the window in characters, never above
// the maximum.
func relative(fraction float64, contextLimit, max int) int {
	cap := int(fraction * float64(contextLimit) * CharsPerToken)
	if cap > max {
		return max
	}
	if cap < 0 {
		return 0
	}
	return cap
}
