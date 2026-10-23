package poo

// estimator measures how much of the context window a run has used.
//
// The estimate starts at 3.5 characters per token, and the first usage
// report calibrates it: the ratio of the serialized request's characters
// to the reported input tokens becomes the model's ratio, clamped to
// [2.0, 5.0] and kept for the session.
//
// Between usage reports, the estimate is the last reported input tokens
// plus the calibrated ratio applied to everything appended since.
type estimator struct {
	// charsPerToken is the calibrated ratio.
	charsPerToken float64
	// baseline is the input token count the last usage report gave.
	baseline int
	// baselineChars is the serialized size of the view at that report.
	baselineChars int
	// calibrated reports that a usage report has been seen.
	calibrated bool
}

// estimate returns the estimated number of prompt tokens of the view.
func (e *estimator) estimate(history []Message) int {
	chars := historyChars(history)
	if !e.calibrated {
		// Before the first usage report the estimate is the whole view
		// at the starting ratio.
		return int(float64(chars) / e.ratio())
	}
	// After a report, the estimate is the last reported input tokens
	// plus the calibrated ratio applied to everything appended since.
	added := chars - e.baselineChars
	if added < 0 {
		added = 0
	}
	return e.baseline + int(float64(added)/e.ratio())
}

// observe records a provider's usage report and calibrates the ratio.
func (e *estimator) observe(inputTokens int, chars int) {
	if inputTokens <= 0 {
		return
	}
	ratio := float64(chars) / float64(inputTokens)
	if ratio < MinCharsPerToken {
		ratio = MinCharsPerToken
	}
	if ratio > MaxCharsPerToken {
		ratio = MaxCharsPerToken
	}
	e.charsPerToken = ratio
	e.baseline = inputTokens
	e.baselineChars = chars
	e.calibrated = true
}

// rebase makes the current view the accounting baseline.
//
// A compaction changes the view, and the estimate is recomputed over the
// whole view. The next usage report replaces it.
func (e *estimator) rebase(history []Message) {
	chars := historyChars(history)
	e.baselineChars = chars
	e.baseline = int(float64(chars) / e.ratio())
}

// historyChars returns the serialized size of a view: the estimate
// every view-size measurement in the session goes through, so a change
// to the per-message accounting lands everywhere at once.
func historyChars(history []Message) int {
	chars := 0
	for _, m := range history {
		chars += estimateMessageChars(m)
	}
	return chars
}

// ratio returns the characters-per-token ratio, never zero.
func (e *estimator) ratio() float64 {
	if e.charsPerToken <= 0 {
		return CharsPerToken
	}
	return e.charsPerToken
}
