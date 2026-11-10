// Package caps holds the output caps the tools share with the settings.
// Cap names a bound the code truncates with; Max names the constant bound
// of a Cap.
//
// A tool's output is capped relative to the model's context window: the
// cap is a fraction of the window in characters, and never above a fixed
// maximum. The fractions and the maximums are here; the caller supplies
// the characters-per-token scale, so the one estimate lives with the
// session's estimator (poo.CharsPerToken) and a recalibration moves
// every consumer.
package caps

import "unicode/utf8"

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

// RunePrefix returns the longest prefix of s that is at most n
// characters.
//
// A cap counted in characters is not a byte count: cutting at n bytes
// would cut a multi-byte character in half, and would cut text that fits
// the cap. The cut lands on the character boundary instead.
func RunePrefix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// RuneCount returns the number of characters in s.
//
// A byte that does not form a valid rune counts as one character, the
// way the cut counts it, so a count and the cut it feeds agree.
func RuneCount(s string) int {
	return utf8.RuneCountInString(s)
}

// SplitRunes cuts a string into its head and its tail, each within a
// character budget, without splitting a rune.
//
// The text is walked rather than copied: the head is found by ranging
// over the string, and the tail by walking back over its last
// characters, so a large output costs no rune slice of its own.
func SplitRunes(s string, half int) (string, string) {
	if half <= 0 {
		return "", ""
	}
	if utf8.RuneCountInString(s) <= half {
		return s, ""
	}
	return RunePrefix(s, half), RuneSuffix(s, half)
}

// RuneSuffix returns the last n characters of a string.
func RuneSuffix(s string, n int) string {
	// The end of the string is found by walking back over its
	// characters, so a rune is never cut in half.
	// A byte that is not part of a valid rune counts as one character,
	// as it does for utf8.RuneCountInString and RunePrefix.
	end := len(s)
	count := 0
	for end > 0 {
		_, size := utf8.DecodeLastRuneInString(s[:end])
		end -= size
		count++
		if count == n {
			break
		}
	}
	return s[end:]
}
