package repl

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/chubin/vt10x"
)

func TestHeldPreviewSurvivesFooterChangesAndSettlesOnce(t *testing.T) {
	for _, footers := range [][]int{{2}, {2, 1}, {4, 2, 1}} {
		t.Run(fmt.Sprint(footers), func(t *testing.T) {
			vt := vt10x.New(vt10x.WithSize(40, 12))
			rec := &recordingScreen{vt: vt}
			l := aLayout(t, rec, 40, 12)
			r := aStreamingRenderer(t, rec, 40, l)
			showPrompt(l, promptMark)
			r.streamDelta("text", "held reply")
			for _, footer := range footers {
				l.ensure(footer)
				showPrompt(l, promptMark+"draft")
				if n := strings.Count(vt.String(), "held reply"); n != 1 {
					t.Fatalf("held reply appears %d times after footer %d:\n%s", n, footer, vt.String())
				}
			}
			rec.take()
			r.finish()
			if strings.Contains(ansi.Strip(rec.take()), "held reply") {
				t.Fatal("a fitting preview was redrawn on finish")
			}
			r.notice("settled")
			for _, token := range []string{"held reply", "settled", "draft"} {
				if n := strings.Count(vt.String(), token); n != 1 {
					t.Fatalf("%q appears %d times:\n%s", token, n, vt.String())
				}
			}
			if n := strings.Count(ansi.Strip(r.transcriptText()), "held reply"); n != 1 {
				t.Fatalf("transcript has reply %d times: %q", n, r.transcriptText())
			}
		})
	}
}
