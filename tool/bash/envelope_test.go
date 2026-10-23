package bash_test

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sdougbrown/poo/tool/bash"
)

// observation decodes the structured payload an envelope carries.
func observation(t *testing.T, structured json.RawMessage) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(structured, &out); err != nil {
		t.Fatalf("decoding the observation %q: %v", structured, err)
	}
	return out
}

// text returns a string field of an observation, and "" when it is not
// there.
func text(obs map[string]any, key string) string {
	value, _ := obs[key].(string)
	return value
}

// TestTheTwoStreamEnvelopeSharesTheCap covers the budget: the two
// streams share the cap, so the streams stay whole when they fit
// together, and when they do not, the short one keeps what it needs and
// the long one takes the rest.
func TestTheTwoStreamEnvelopeSharesTheCap(t *testing.T) {
	cases := []struct {
		name   string
		output string
		stderr string
		cap    int
		// check asserts the observation the case expects.
		check func(t *testing.T, obs map[string]any)
	}{
		{
			name:   "both fit together",
			output: strings.Repeat("x", 35),
			stderr: "y",
			cap:    40,
			check: func(t *testing.T, obs map[string]any) {
				if got := text(obs, bash.EnvelopeOutput); got != strings.Repeat("x", 35) {
					t.Errorf("the output is %q, want it whole", got)
				}
				if got := text(obs, bash.EnvelopeStderr); got != "y" {
					t.Errorf("the stderr is %q, want it whole", got)
				}
				if _, ok := obs[bash.EnvelopeElidedChars]; ok {
					t.Errorf("the observation elides output that fits: %v", obs)
				}
			},
		},
		{
			name:   "a short stderr keeps the long stdout's budget",
			output: strings.Repeat("x", 100),
			stderr: "y",
			cap:    40,
			check: func(t *testing.T, obs map[string]any) {
				if got := text(obs, bash.EnvelopeStderr); got != "y" {
					t.Errorf("the stderr is %q, want it whole", got)
				}
				if got := text(obs, bash.EnvelopeOutputHead) + text(obs, bash.EnvelopeOutputTail); len(got) != 38 {
					t.Errorf("the output keeps %d characters, want 38: %v", len(got), obs)
				}
				if got, _ := obs[bash.EnvelopeElidedChars].(float64); got != 62 {
					t.Errorf("the observation elides %v characters, want 62", got)
				}
			},
		},
		{
			name:   "a short stdout keeps the long stderr's budget",
			output: "y",
			stderr: strings.Repeat("z", 100),
			cap:    40,
			check: func(t *testing.T, obs map[string]any) {
				if got := text(obs, bash.EnvelopeOutput); got != "y" {
					t.Errorf("the output is %q, want it whole", got)
				}
				if got := text(obs, bash.EnvelopeStderrHead) + text(obs, bash.EnvelopeStderrTail); len(got) != 38 {
					t.Errorf("the stderr keeps %d characters, want 38: %v", len(got), obs)
				}
			},
		},
		{
			name:   "two long streams split the cap",
			output: strings.Repeat("x", 100),
			stderr: strings.Repeat("y", 100),
			cap:    40,
			check: func(t *testing.T, obs map[string]any) {
				for _, key := range []string{bash.EnvelopeOutputHead, bash.EnvelopeOutputTail, bash.EnvelopeStderrHead, bash.EnvelopeStderrTail} {
					if got := text(obs, key); len(got) != 10 {
						t.Errorf("the %s holds %d characters, want 10: %v", key, len(got), obs)
					}
				}
			},
		},
		{
			name:   "exactly the cap",
			output: strings.Repeat("x", 39),
			stderr: "y",
			cap:    40,
			check: func(t *testing.T, obs map[string]any) {
				if _, ok := obs[bash.EnvelopeElidedChars]; ok {
					t.Errorf("the observation elides output that exactly fits: %v", obs)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, structured := bash.Envelope(bash.Observation{Output: c.output, Stderr: c.stderr, Cap: c.cap})
			c.check(t, observation(t, structured))
		})
	}
}

// TestTheTwoStreamEnvelopeNeverOutgrowsTheCap covers the budget over
// every combination of sizes: the characters the observation keeps are
// at most the cap, and a stream that fits in its share is whole.
func TestTheTwoStreamEnvelopeNeverOutgrowsTheCap(t *testing.T) {
	sizes := []int{0, 1, 5, 20, 39, 40, 41, 100}
	caps := []int{1, 2, 10, 20, 40, 41, 100}
	for _, cap := range caps {
		for _, outLen := range sizes {
			for _, errLen := range sizes {
				output := strings.Repeat("x", outLen)
				stderr := strings.Repeat("y", errLen)
				_, structured := bash.Envelope(bash.Observation{Output: output, Stderr: stderr, Cap: cap})
				obs := observation(t, structured)

				kept := len(text(obs, bash.EnvelopeOutput))
				kept += len(text(obs, bash.EnvelopeStderr))
				kept += len(text(obs, bash.EnvelopeOutputHead)) + len(text(obs, bash.EnvelopeOutputTail))
				kept += len(text(obs, bash.EnvelopeStderrHead)) + len(text(obs, bash.EnvelopeStderrTail))
				if kept > cap {
					t.Errorf("cap %d, %d output and %d stderr characters: the observation keeps %d characters: %v",
						cap, outLen, errLen, kept, obs)
				}

				fits := outLen+errLen <= cap
				_, elided := obs[bash.EnvelopeElidedChars]
				if fits && elided {
					t.Errorf("cap %d, %d output and %d stderr characters: the observation elides output that fits: %v",
						cap, outLen, errLen, obs)
				}
				if !fits && !elided {
					t.Errorf("cap %d, %d output and %d stderr characters: the observation claims to keep everything: %v",
						cap, outLen, errLen, obs)
				}
				if fits && (text(obs, bash.EnvelopeOutput) != output || text(obs, bash.EnvelopeStderr) != stderr) {
					t.Errorf("cap %d, %d output and %d stderr characters: the observation is not whole: %v",
						cap, outLen, errLen, obs)
				}
			}
		}
	}
}

// TestTheTwoStreamEnvelopeCutsBetweenCharacters covers the runes: a cut
// never splits a multi-byte character, in either stream.
func TestTheTwoStreamEnvelopeCutsBetweenCharacters(t *testing.T) {
	for _, cap := range []int{5, 10, 11, 17, 40} {
		_, structured := bash.Envelope(bash.Observation{
			Output: strings.Repeat("é", 50),
			Stderr: strings.Repeat("🙂", 50),
			Cap:    cap,
		})
		obs := observation(t, structured)
		for _, key := range []string{
			bash.EnvelopeOutput, bash.EnvelopeStderr,
			bash.EnvelopeOutputHead, bash.EnvelopeOutputTail,
			bash.EnvelopeStderrHead, bash.EnvelopeStderrTail,
		} {
			if got := text(obs, key); !utf8.ValidString(got) {
				t.Errorf("cap %d: the %s splits a character: %q", cap, key, got)
			}
		}
	}
}

// TestTheCombinedStreamEnvelopeIsUnchanged covers the shape bash's own
// observation has: bash captures stdout and stderr together, so its
// envelope is the single-stream one, and it renders byte for byte as it
// always has.
//
// The baseline renderer below is the shape before the two streams could
// be captured apart; the envelope must match it in the text and in the
// structured payload.
func TestTheCombinedStreamEnvelopeIsUnchanged(t *testing.T) {
	zero, fail := 0, 3
	cases := []bash.Observation{
		{Returncode: &zero, Output: "success\n", Cap: 40},
		{Returncode: &fail, Output: "failure\n", Cap: 40},
		{TimedOut: true, Output: "partial\n", Cap: 40},
		{Returncode: &zero, Output: strings.Repeat("aé🙂", 50), Cap: 11},
		{Output: "invalid\xff\xc3\xa9", Cap: 5},
		{Returncode: &zero, Cap: 0},
	}
	rng := rand.New(rand.NewSource(10))
	for i := 0; i < 5000; i++ {
		b := make([]byte, rng.Intn(200))
		rng.Read(b)
		var code *int
		if i%3 != 0 {
			code = &fail
		}
		cases = append(cases, bash.Observation{
			Returncode: code,
			TimedOut:   i%2 == 0,
			Output:     string(b),
			Cap:        1 + rng.Intn(210),
		})
	}
	for i, o := range cases {
		wantText, wantStructured := baselineEnvelope(o)
		text, structured := bash.Envelope(o)
		if text != wantText || string(structured) != string(wantStructured) {
			t.Fatalf("case %d: text=%q want=%q structured=%s want=%s",
				i, text, wantText, structured, wantStructured)
		}
	}
}

// baselineEnvelope renders a single-stream observation the way bash's
// envelope always has.
func baselineEnvelope(o bash.Observation) (string, json.RawMessage) {
	cap := o.Cap
	if cap <= 0 {
		cap = bash.ObservationCapMax
	}
	// A killed process has no exit code, so the field is null.
	var code any
	if o.Returncode != nil {
		code = *o.Returncode
	}
	fields := []baselineField{
		{bash.EnvelopeReturncode, code},
		{bash.EnvelopeTimedOut, o.TimedOut},
	}
	outChars := utf8.RuneCountInString(o.Output)
	if outChars <= cap {
		fields = append(fields, baselineField{bash.EnvelopeOutput, o.Output})
	} else {
		half := cap / 2
		head, tail := baselineSplitRunes(o.Output, half)
		fields = append(fields,
			baselineField{bash.EnvelopeOutputHead, head},
			baselineField{bash.EnvelopeOutputTail, tail},
			baselineField{bash.EnvelopeElidedChars, outChars - 2*half},
			baselineField{bash.EnvelopeHint, bash.ElisionHint},
		)
	}
	structured := map[string]any{}
	for _, f := range fields {
		structured[f.key] = f.value
	}
	return baselineRender(fields), baselineJSON(structured)
}

// baselineField is one entry of the baseline envelope.
type baselineField struct {
	key   string
	value any
}

// baselineRender formats the baseline envelope.
func baselineRender(fields []baselineField) string {
	var b strings.Builder
	b.WriteString("{")
	for i, f := range fields {
		if i > 0 {
			b.WriteString(", ")
		}
		key, _ := json.Marshal(f.key)
		value, _ := json.Marshal(f.value)
		b.Write(key)
		b.WriteString(": ")
		b.Write(value)
	}
	b.WriteString("}")
	return b.String()
}

// baselineSplitRunes cuts a string into its head and its tail, each
// within a character budget, without splitting a rune.
func baselineSplitRunes(s string, half int) (string, string) {
	if half <= 0 {
		return "", ""
	}
	if utf8.RuneCountInString(s) <= half {
		return s, ""
	}
	return baselineHeadRunes(s, half), baselineTailRunes(s, half)
}

// baselineHeadRunes returns the first n characters of a string.
func baselineHeadRunes(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// baselineTailRunes returns the last n characters of a string.
func baselineTailRunes(s string, n int) string {
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

// baselineJSON encodes a value, falling back to an empty object.
func baselineJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}
