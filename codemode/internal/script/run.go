// Package script holds the run limits and output accounting shared by engines.
package script

import (
	"fmt"
	"math"
	"time"
	"unicode/utf8"

	"github.com/sdougbrown/poo/codemode"
	"github.com/sdougbrown/poo/internal/caps"
)

const (
	DefaultTimeoutS       = 120
	DefaultScriptMaxBytes = 64 * 1024
	DefaultOutputCapChars = 16 * 1024
	MaxGatherWidth        = 16
)

// CheckRun validates the limits, script size and tool bridge, and fills shared defaults.
func CheckRun(opts codemode.RunOptions) (codemode.Limits, error) {
	l := opts.Limits
	for _, limit := range []struct {
		name  string
		value int
	}{
		{"timeout_s", l.TimeoutS},
		{"script_max_bytes", l.ScriptMaxBytes},
		{"output_cap_chars", l.OutputCapChars},
		{"exec_steps", l.ExecSteps},
	} {
		if limit.value < 0 {
			return codemode.Limits{}, fmt.Errorf("the codemode limit %s is negative", limit.name)
		}
	}
	const maxTimeoutS = math.MaxInt64 / int64(time.Second)
	if int64(l.TimeoutS) > maxTimeoutS {
		return codemode.Limits{}, fmt.Errorf("the codemode limit timeout_s is over the %d second maximum", maxTimeoutS)
	}
	if l.TimeoutS == 0 {
		l.TimeoutS = DefaultTimeoutS
	}
	if l.ScriptMaxBytes == 0 {
		l.ScriptMaxBytes = DefaultScriptMaxBytes
	}
	if l.OutputCapChars == 0 {
		l.OutputCapChars = DefaultOutputCapChars
	}
	if len(opts.Script) > l.ScriptMaxBytes {
		return codemode.Limits{}, fmt.Errorf("the script is %d bytes, over the %d byte limit", len(opts.Script), l.ScriptMaxBytes)
	}
	if opts.Nested == nil {
		return codemode.Limits{}, fmt.Errorf("the script has no way to call tools")
	}
	return l, nil
}

type Output struct {
	Lines     []string
	Truncated bool
	printed   int
}

func (o *Output) Print(msg string, limit int) {
	room := limit - o.printed
	if room <= 0 {
		o.Truncated = true
		return
	}
	n := utf8.RuneCountInString(msg)
	if n > room {
		msg = caps.RunePrefix(msg, room) + " …"
		n = room + 2
		o.Truncated = true
	}
	o.Lines = append(o.Lines, msg)
	o.printed += n + 1
}
