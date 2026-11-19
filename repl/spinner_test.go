package repl

import (
	"testing"
	"time"
)

// ageSpinner pretends the spinner has run for d, so a test does not
// sleep through streamWriteDelay.
func ageSpinner(r *renderer, d time.Duration) {
	r.block.spinner.started = time.Now().Add(-d)
}

// TestTheWritingHintAnimates covers the hint's braille spinner: the
// first frame stands when the hint is painted, the run loop's frame
// advance shows the next one, and taking the hint takes the row off.
func TestTheWritingHintAnimates(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.hint()
	if rowOf(t, scr, "⠋ writing") < 0 {
		t.Fatalf("the hint never showed its first frame:\n%s", scr.text())
	}
	r.paintHintFrame()
	if rowOf(t, scr, "⠙ writing") < 0 {
		t.Fatalf("the hint never advanced to its second frame:\n%s", scr.text())
	}
	if rowOf(t, scr, "⠋ writing") >= 0 {
		t.Fatalf("the first frame stood beside the second:\n%s", scr.text())
	}
	r.takeHint()
	if rowOf(t, scr, "writing") >= 0 {
		t.Fatalf("the hint stood after it was taken:\n%s", scr.text())
	}
}

// TestASlowToolCallShowsItsSpinner covers the tool spinner: a call
// that has run past the delay paints a row under the call's own, with
// the tool, the summary and the seconds, and the result takes the row
// down.
func TestASlowToolCallShowsItsSpinner(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.toolCall("bash", "ls -la")
	r.armSpinner("bash", "ls -la")
	ageSpinner(r, 2500*time.Millisecond)
	r.paintSpinner()
	if rowOf(t, scr, "⠴ bash · ls -la · 2s") < 0 {
		t.Fatalf("the spinner never showed:\n%s", scr.text())
	}

	r.takeSpinner()
	r.toolResult("bash", "ok", "file.go\n", 0)
	if rowOf(t, scr, "2s") >= 0 {
		t.Fatalf("the spinner stood under its result:\n%s", scr.text())
	}
}

// TestAFastToolCallNeverShowsASpinner covers the delay: a call whose
// result lands within streamWriteDelay never paints a row.
func TestAFastToolCallNeverShowsASpinner(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.toolCall("grep", "needle")
	r.armSpinner("grep", "needle")
	r.paintSpinner()
	if rowOf(t, scr, "⠋") >= 0 {
		t.Fatalf("the spinner showed before the delay ran out:\n%s", scr.text())
	}
	r.takeSpinner()
	r.toolResult("grep", "ok", "hit\n", 0)
	if rowOf(t, scr, "⠋") >= 0 {
		t.Fatalf("the spinner showed after the result:\n%s", scr.text())
	}
}

// TestASpinnerCountsTheSeconds covers the timer: each second the call
// waits replaces the row, so the count reads the wait, and no stale
// count stands beside the new one.
func TestASpinnerCountsTheSeconds(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.toolCall("bash", "sleep")
	r.armSpinner("bash", "sleep")

	ageSpinner(r, 2400*time.Millisecond)
	r.paintSpinner()
	if rowOf(t, scr, "· 2s") < 0 {
		t.Fatalf("the row never showed two seconds:\n%s", scr.text())
	}

	ageSpinner(r, 3900*time.Millisecond)
	r.paintSpinner()
	if rowOf(t, scr, "· 3s") < 0 {
		t.Fatalf("the row never showed three seconds:\n%s", scr.text())
	}
	if rowOf(t, scr, "· 2s") >= 0 {
		t.Fatalf("the stale count stood beside the new one:\n%s", scr.text())
	}
}

// TestANoticeTakesTheSpinnerDown covers a write that lands while a
// tool runs: the wait's row comes off, and it never stands again
// under the line the repl wrote.
func TestANoticeTakesTheSpinnerDown(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.toolCall("bash", "make")
	r.armSpinner("bash", "make")
	ageSpinner(r, 2200*time.Millisecond)
	r.paintSpinner()
	if rowOf(t, scr, "bash · make · 2s") < 0 {
		t.Fatalf("the spinner never showed:\n%s", scr.text())
	}

	r.notice("older history is not shown")
	if rowOf(t, scr, "· 2s") >= 0 {
		t.Fatalf("the spinner stood under the notice:\n%s", scr.text())
	}
	r.paintSpinner()
	if rowOf(t, scr, "· 2s") >= 0 {
		t.Fatalf("the spinner came back after the notice:\n%s", scr.text())
	}
}
