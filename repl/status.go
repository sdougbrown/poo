package repl

import (
	"os/exec"
	"strings"
)

// status is what the statusline says. The repl fills it in as it
// learns: the session names the model, the runs report the token
// accounting, and the working directory names the folder and the
// branch.
type status struct {
	// dir is the directory the session's tools work in.
	dir string
	// branch is the git branch of dir, when dir is a repository.
	branch string
	// model is the name the session's chat model answers to.
	model string
	// level is the reasoning level the session runs at. It is empty when
	// the session sends no reasoning setting.
	level string
	// contextLimit is the model's context window in tokens. Zero says
	// the repl does not know it.
	contextLimit int
	// inputTokens is the last request's prompt-token count, the
	// closest thing on the wire to the context's size.
	inputTokens int
}

// gitBranch returns the branch of the repository at dir.
//
// A directory that is not a repository, or a git the repl cannot run,
// has no branch: the statusline leaves the segment out.
func gitBranch(dir string) (string, bool) {
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").Output()
	if err == nil {
		if branch := strings.TrimSpace(string(out)); branch != "" {
			return branch, true
		}
	}
	// A detached head has no symbolic ref; the commit names it.
	out, err = exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err == nil {
		if branch := strings.TrimSpace(string(out)); branch != "" {
			return branch, true
		}
	}
	return "", false
}

// newStatus reads the parts of the statusline a session does not name.
func newStatus(dir string) *status {
	s := &status{dir: dir}
	if dir != "" {
		if branch, ok := gitBranch(dir); ok {
			s.branch = branch
		}
	}
	return s
}

// line renders the statusline: the folder, the branch, the model and
// the context's fill, dimmed to stay behind the conversation.
func (s *status) line() string {
	var segments []string
	if s.dir != "" {
		segments = append(segments, "⌂ "+folderName(s.dir))
	}
	if s.branch != "" {
		segments = append(segments, "⎇ "+s.branch)
	}
	if s.model != "" {
		segments = append(segments, ansiCyan+s.model+ansiReset)
	}
	if s.level != "" {
		segments = append(segments, "🧠 "+s.level)
	}
	if s.inputTokens > 0 {
		ctx := shortTokens(s.inputTokens)
		if s.contextLimit > 0 {
			ctx += "/" + shortTokens(s.contextLimit)
		}
		segments = append(segments, "ctx "+ctx)
	}
	if len(segments) == 0 {
		return ""
	}
	return ansiDim + strings.Join(segments, " │ ") + ansiReset
}

// prompt renders the statusline and the input mark the repl reads its
// lines behind. It is a readline prompt: the first line holds the
// status, the second holds the mark.
func (s *status) prompt() string {
	if line := s.line(); line != "" {
		return line + "\n💩❯ "
	}
	return "💩❯ "
}

// folderName returns the directory a statusline names: the folder's
// own name, or the whole path for the filesystem's root.
func folderName(dir string) string {
	dir = strings.TrimRight(dir, "/")
	if dir == "" {
		return "/"
	}
	if i := strings.LastIndexByte(dir, '/'); i >= 0 {
		return dir[i+1:]
	}
	return dir
}

// setUsage records what the last request cost, with the window the
// model fills. A run that reports nothing changes nothing.
func (s *status) setUsage(inputTokens, contextLimit int) {
	if inputTokens >= 0 {
		s.inputTokens = inputTokens
	}
	if contextLimit > 0 {
		s.contextLimit = contextLimit
	}
}

// setWindow records the context window of the model the session now
// runs. A limit of zero says the catalog does not know the window,
// and the line drops it rather than keeping the last model's.
func (s *status) setWindow(contextLimit int) {
	s.contextLimit = contextLimit
}
