package repl

import (
	"fmt"
	"os"
	"os/exec"
)

// page shows the transcript through less, with ANSI colors kept
// (-R). A terminal that has no less, or a transcript that is empty,
// does nothing: the conversation stays where it is.
//
// The pager takes the terminal over between prompts, the way an
// editor would; the repl waits for it to leave.
func (r *renderer) page() error {
	text := r.transcriptText()
	if text == "" {
		return nil
	}
	less, err := exec.LookPath("less")
	if err != nil {
		return fmt.Errorf("repl: no less on the path: %w", err)
	}
	f, err := os.CreateTemp("", "poo-transcript-*.ansi")
	if err != nil {
		return fmt.Errorf("repl: writing the transcript: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return fmt.Errorf("repl: writing the transcript: %w", err)
	}
	f.Close()

	cmd := exec.Command(less, "-R", f.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
