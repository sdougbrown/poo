package repl

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// page shows the transcript through less, with ANSI colors kept
// (-R). A terminal that has no less, or a transcript that is empty,
// does nothing: the conversation stays where it is.
//
// The pager takes the terminal over between prompts, the way an
// editor would; the repl waits for it to leave. Its stdio is the
// repl's own, so an embedded or redirected session pages where it
// reads and writes, not at the process's terminal.
//
// The transcript is already the final ANSI text, so the pager reads
// the file itself: the LESSOPEN preprocessor is cleared from the
// pager's environment, since a lesspipe hook re-colorizes the file
// and a highlighter reads the escape sequences as code, leaving
// their parameters as text on the screen.
func (r *renderer) page(in io.Reader) error {
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

	// The pager takes the whole terminal: the region and the footer come
	// off first, and the next prompt reserves them again.
	if r.layout != nil {
		r.layout.suspend()
		defer r.layout.resume()
	}
	cmd := exec.Command(less, "-R", f.Name())
	// Filter the hook out rather than only appending an empty entry:
	// the intent reads from the code, and the cleared pair holds no
	// matter what the child's getenv resolves first.
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if name, _, ok := strings.Cut(kv, "="); ok && (name == "LESSOPEN" || name == "LESSCLOSE") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env, "LESSOPEN=", "LESSCLOSE=")
	cmd.Stdin = in
	cmd.Stdout = r.out
	cmd.Stderr = r.out
	return cmd.Run()
}
