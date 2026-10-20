package eval

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// judgePrompt is the judge's prompt.
//
// The prompt is a file next to the code, so a reader can change what the
// judge is asked without touching the code.
//
//go:embed judge_prompt.txt
var judgePrompt string

// Defaults for the judge.
const (
	// DefaultJudgeBaseURL is the endpoint the judge talks to.
	DefaultJudgeBaseURL = "http://localhost:4000/v1"
	// DefaultJudgeModel is the model the judge is.
	DefaultJudgeModel = "deepseek-flash"
	// DefaultJudgeMaxChars is the size a transcript may have before it
	// is summarized before it is sent.
	DefaultJudgeMaxChars = 60000
	// JudgeKeepHead is the number of segments kept from the front of a
	// summarized transcript.
	JudgeKeepHead = 3
	// JudgeKeepTail is the number of segments kept from the back of a
	// summarized transcript.
	JudgeKeepTail = 3
	// judgeTimeout bounds one judge request.
	judgeTimeout = 120 * time.Second
)

// Pathology is one failure mode a judge can report.
type Pathology string

const (
	// PathologyToolCallParse means a tool call was malformed.
	PathologyToolCallParse Pathology = "tool_call_parse_failure"
	// PathologyPrematureEndTurn means the agent stopped too early.
	PathologyPrematureEndTurn Pathology = "premature_end_turn"
	// PathologyContextBlowup means the agent over-read.
	PathologyContextBlowup Pathology = "context_blowup"
	// PathologyTTFTGrowth means the later requests got much slower.
	PathologyTTFTGrowth Pathology = "ttft_growth"
	// PathologyLoopOrReread means the agent repeated itself.
	PathologyLoopOrReread Pathology = "loop_or_reread"
	// PathologyOffTaskEdit means the agent changed what it should not.
	PathologyOffTaskEdit Pathology = "off_task_edit"
	// PathologyOther means something else went wrong.
	PathologyOther Pathology = "other"
)

// knownPathologies are the names a judge may report.
//
// A judge that reports a name outside this list is not understood, and
// the name is kept as "other" with the original in the reason.
var knownPathologies = map[Pathology]bool{
	PathologyToolCallParse:    true,
	PathologyPrematureEndTurn: true,
	PathologyContextBlowup:    true,
	PathologyTTFTGrowth:       true,
	PathologyLoopOrReread:     true,
	PathologyOffTaskEdit:      true,
	PathologyOther:            true,
}

// Verdict is one judge's answer about one transcript.
type Verdict struct {
	// Task, Model and Seed name the run.
	Task  string `json:"task"`
	Model string `json:"model"`
	Seed  int    `json:"seed"`
	// LogicalPath reports whether the agent took a sensible route.
	LogicalPath bool `json:"logical_path"`
	// AgreesWithCheck reports whether the check's verdict matches the
	// trajectory.
	AgreesWithCheck bool `json:"agrees_with_check"`
	// Pathologies are the failure modes the judge saw.
	Pathologies []Pathology `json:"pathologies"`
	// NeedsOwner reports whether a human should read this transcript.
	NeedsOwner bool `json:"needs_owner"`
	// Reason is the judge's one-line reason, citing turn numbers.
	Reason string `json:"reason"`
	// Summarized reports whether the transcript was too large for the
	// judge and was summarized before it was sent.
	Summarized bool `json:"summarized,omitempty"`
	// TranscriptChars is the size of the transcript that was sent.
	TranscriptChars int `json:"transcript_chars"`
	// ElidedTurns is the number of turns the summary dropped.
	ElidedTurns int `json:"elided_turns,omitempty"`
	// Error is set when the judge could not be reached.
	Error string `json:"error,omitempty"`
}

// JudgeOptions configure a judging run.
type JudgeOptions struct {
	// BaseURL is the judge's endpoint.
	BaseURL string
	// Model is the judge's model identifier.
	Model string
	// APIKeyEnv names the environment variable holding the judge's API
	// key. The value is never read by the harness: it is passed to the
	// judge client by name.
	APIKeyEnv string
	// ResultsDir is the directory the transcripts come from.
	ResultsDir string
	// OutDir is the directory the verdicts go to.
	OutDir string
	// MaxChars is the size a transcript may have before it is
	// summarized.
	MaxChars int
	// PromptPath is the file holding the judge's prompt.
	PromptPath string
	// HTTPClient is the client the judge uses. Nil means a default.
	HTTPClient *http.Client
}

// Judge reads transcripts and reports what it sees.
//
// The judge is a model on an OpenAI-compatible endpoint. The harness
// sends each transcript, with the check's outcome and the metrics, and
// writes one verdict per transcript.
type Judge struct {
	opts JudgeOptions
	// prompt is the judge's prompt.
	prompt string
	// client is the HTTP client the judge uses.
	client *http.Client
}

// NewJudge returns a judge with the options filled in.
func NewJudge(opts JudgeOptions) (*Judge, error) {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultJudgeBaseURL
	}
	if opts.Model == "" {
		opts.Model = DefaultJudgeModel
	}
	if opts.MaxChars <= 0 {
		opts.MaxChars = DefaultJudgeMaxChars
	}
	if opts.PromptPath == "" {
		opts.PromptPath = DefaultJudgePromptPath
	}
	if opts.OutDir == "" {
		opts.OutDir = filepath.Join(opts.ResultsDir, "judgments")
	}

	prompt := judgePrompt
	if opts.PromptPath != DefaultJudgePromptPath {
		read, err := os.ReadFile(opts.PromptPath)
		if err != nil {
			return nil, fmt.Errorf("eval: reading the judge prompt: %w", err)
		}
		prompt = string(read)
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: judgeTimeout}
	}
	return &Judge{opts: opts, prompt: prompt, client: client}, nil
}

// DefaultJudgePromptPath is the path of the judge's prompt.
//
// The prompt ships with the package, so a judge run works out of the box.
// A caller that names a different path overrides it.
const DefaultJudgePromptPath = "eval/judge_prompt.txt"

// JudgeResult is the result of judging a set of transcripts.
type JudgeResult struct {
	// Verdicts are the verdicts, in the order the transcripts were
	// found.
	Verdicts []Verdict `json:"verdicts"`
	// OutDir is where the verdicts were written.
	OutDir string `json:"out_dir"`
}

// Run judges every transcript under the results directory.
//
// A transcript is a session log under <results-dir>/runs/. Every log is
// judged, and every verdict is written under <results-dir>/judgments/.
func (j *Judge) Run(ctx context.Context) (*JudgeResult, error) {
	transcripts, err := j.findTranscripts()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(j.opts.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("eval: creating %s: %w", j.opts.OutDir, err)
	}

	result := &JudgeResult{OutDir: j.opts.OutDir}
	for _, t := range transcripts {
		verdict, err := j.judgeOne(ctx, t)
		if err != nil {
			return nil, err
		}
		result.Verdicts = append(result.Verdicts, *verdict)

		name := verdictFileName(*verdict)
		data, err := json.MarshalIndent(verdict, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("eval: encoding the verdict: %w", err)
		}
		data = append(data, '\n')
		if err := os.WriteFile(filepath.Join(j.opts.OutDir, name), data, 0o644); err != nil {
			return nil, fmt.Errorf("eval: writing the verdict: %w", err)
		}
	}

	if err := j.writeSummary(result); err != nil {
		return nil, err
	}
	return result, nil
}

// transcript is one transcript to judge.
type transcript struct {
	// path is the session log.
	path string
	// task, model and seed name the run.
	task, model string
	seed        int
}

// findTranscripts returns the transcripts under the results directory.
//
// A transcript is a session.jsonl under a run directory. The directory
// layout names the task, the model and the seed.
func (j *Judge) findTranscripts() ([]transcript, error) {
	runsDir := filepath.Join(j.opts.ResultsDir, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("eval: reading %s: %w", runsDir, err)
	}

	var out []transcript
	for _, task := range entries {
		if !task.IsDir() {
			continue
		}
		taskDir := filepath.Join(runsDir, task.Name())
		models, err := os.ReadDir(taskDir)
		if err != nil {
			return nil, fmt.Errorf("eval: reading %s: %w", taskDir, err)
		}
		for _, model := range models {
			if !model.IsDir() {
				continue
			}
			modelDir := filepath.Join(taskDir, model.Name())
			seeds, err := os.ReadDir(modelDir)
			if err != nil {
				return nil, fmt.Errorf("eval: reading %s: %w", modelDir, err)
			}
			for _, seed := range seeds {
				if !seed.IsDir() {
					continue
				}
				path := filepath.Join(modelDir, seed.Name(), "session.jsonl")
				if _, err := os.Stat(path); err != nil {
					continue
				}
				out = append(out, transcript{
					path:  path,
					task:  task.Name(),
					model: model.Name(),
					seed:  seedNumber(seed.Name()),
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].task != out[j].task {
			return out[i].task < out[j].task
		}
		if out[i].model != out[j].model {
			return out[i].model < out[j].model
		}
		return out[i].seed < out[j].seed
	})
	return out, nil
}

// judgeOne judges one transcript.
//
// A transcript that cannot be read, or a judge that cannot be reached,
// produces a verdict with the error in it rather than stopping the run:
// one bad transcript must not lose the other verdicts.
func (j *Judge) judgeOne(ctx context.Context, t transcript) (*Verdict, error) {
	verdict := &Verdict{
		Task:  t.task,
		Model: t.model,
		Seed:  t.seed,
	}

	log, err := ParseLog(t.path)
	if err != nil {
		verdict.Error = err.Error()
		return verdict, nil
	}

	segments := log.TranscriptSegments()
	text, summarized, elided := summarizeTranscript(segments, j.opts.MaxChars)
	verdict.Summarized = summarized
	verdict.TranscriptChars = len(text)
	verdict.ElidedTurns = elided

	payload := j.buildPayload(log, text, t)
	answer, err := j.ask(ctx, payload)
	if err != nil {
		verdict.Error = err.Error()
		return verdict, nil
	}

	parsed, err := parseVerdict(answer)
	if err != nil {
		verdict.Error = err.Error()
		return verdict, nil
	}
	verdict.LogicalPath = *parsed.LogicalPath
	verdict.AgreesWithCheck = *parsed.AgreesWithCheck
	verdict.Pathologies = normalizePathologies(parsed.Pathologies)
	verdict.NeedsOwner = *parsed.NeedsOwner
	verdict.Reason = parsed.Reason
	return verdict, nil
}

// judgePayload is what the judge is sent.
type judgePayload struct {
	// Task, Model and Seed name the run.
	Task  string `json:"task"`
	Model string `json:"model"`
	Seed  int    `json:"seed"`
	// CheckPassed is the check script's verdict.
	CheckPassed bool `json:"check_passed"`
	// CheckOutput is the tail of the check's output.
	CheckOutput string `json:"check_output,omitempty"`
	// Metrics holds the harness's numbers.
	Metrics Metrics `json:"metrics"`
	// Transcript is the transcript, summarized if it was too large.
	Transcript string `json:"transcript"`
	// Summarized reports whether the transcript was summarized.
	Summarized bool `json:"summarized"`
}

// buildPayload assembles what the judge is sent.
//
// The check's outcome and the metrics come from the run's own report
// entry, when the harness wrote one. The judge and a reader of the
// report then see the same numbers, and the judge's "agrees with check"
// question has the check's real answer to compare against.
func (j *Judge) buildPayload(log *Log, transcript string, t transcript) judgePayload {
	payload := judgePayload{Transcript: transcript}
	if entry, ok := j.runEntry(t); ok {
		payload.CheckPassed = entry.Success
		payload.CheckOutput = entry.CheckOutput
		payload.Metrics = entry.Metrics
		return payload
	}

	// No report entry: the log alone. The settle reason is the closest
	// thing to a check's verdict, and the metrics come from the log.
	payload.CheckPassed = false
	for _, r := range log.Records {
		if r.Record == "run.settled" {
			payload.CheckPassed = r.Reason == "end_turn"
		}
	}
	payload.Metrics = ComputeMetrics(log, Diff{})
	return payload
}

// runEntry returns the report entry of a run, when the harness wrote
// one.
func (j *Judge) runEntry(t transcript) (RunResult, bool) {
	report, err := LoadReport(j.opts.ResultsDir)
	if err != nil {
		return RunResult{}, false
	}
	for _, r := range report.Results {
		if r.Task == t.task && r.Model == t.model && r.Seed == t.seed {
			return r, true
		}
	}
	return RunResult{}, false
}

// ask sends the payload to the judge and returns its answer.
func (j *Judge) ask(ctx context.Context, payload judgePayload) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model": j.opts.Model,
		"messages": []map[string]string{
			{"role": "system", "content": j.prompt},
			{"role": "user", "content": judgeUserMessage(payload)},
		},
		"stream": false,
	})
	if err != nil {
		return "", fmt.Errorf("eval: encoding the judge request: %w", err)
	}

	url := strings.TrimRight(j.opts.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("eval: making the judge request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key, ok := os.LookupEnv(j.opts.APIKeyEnv); ok && j.opts.APIKeyEnv != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := j.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("eval: calling the judge: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", fmt.Errorf("eval: reading the judge's answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("eval: the judge answered %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var answer struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return "", fmt.Errorf("eval: decoding the judge's answer: %w", err)
	}
	if len(answer.Choices) == 0 {
		return "", fmt.Errorf("eval: the judge returned no choice")
	}
	return answer.Choices[0].Message.Content, nil
}

// judgeUserMessage renders the payload for the judge.
//
// The transcript is fenced, so the judge can tell the data from the
// instructions, and the metrics are in front of it.
func judgeUserMessage(payload judgePayload) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task: %s\n", payload.Task)
	fmt.Fprintf(&b, "Model: %s\n", payload.Model)
	fmt.Fprintf(&b, "Seed: %d\n", payload.Seed)
	fmt.Fprintf(&b, "Check script passed: %t\n", payload.CheckPassed)
	if payload.CheckOutput != "" {
		fmt.Fprintf(&b, "Check output:\n%s\n", payload.CheckOutput)
	}
	fmt.Fprintf(&b, "Metrics: %s\n", formatMetrics(payload.Metrics))
	if payload.Summarized {
		fmt.Fprintf(&b, "The transcript below was summarized: its first and last turns are kept and the middle is elided with counts.\n")
	}
	fmt.Fprintf(&b, "\n--- BEGIN TRANSCRIPT ---\n%s\n--- END TRANSCRIPT ---\n", payload.Transcript)
	return b.String()
}

// formatMetrics renders the metrics for the judge.
func formatMetrics(m Metrics) string {
	parts := []string{
		fmt.Sprintf("turns=%d", m.Turns),
		fmt.Sprintf("tool_calls=%d", m.ToolCalls),
		fmt.Sprintf("tool_errors=%d", m.ToolErrors),
		fmt.Sprintf("text_parse_failures=%d", m.TextParseFailures),
		fmt.Sprintf("reads=%d", m.Reads),
		fmt.Sprintf("rereads=%d", m.Rereads),
		fmt.Sprintf("diff_lines=%d", m.DiffLines),
		fmt.Sprintf("settle_reason=%s", m.SettleReason),
		fmt.Sprintf("ttft_ms=%.0f", m.TTFTMS),
	}
	if len(m.UnexpectedFiles) > 0 {
		parts = append(parts, "changed_files="+strings.Join(m.UnexpectedFiles, ","))
	}
	return strings.Join(parts, " ")
}

// summarizeTranscript keeps the first and last segments of a transcript
// that is too large, and elides the middle with counts.
//
// The summary is deterministic: the same transcript always produces the
// same summary, so a verdict can be reproduced.
func summarizeTranscript(segments []TranscriptSegment, maxChars int) (string, bool, int) {
	total := 0
	for _, s := range segments {
		total += len(s.Text)
	}
	if total <= maxChars {
		var b strings.Builder
		for _, s := range segments {
			b.WriteString(s.Text)
		}
		return b.String(), false, 0
	}

	head := segments
	tail := segments
	if len(segments) > JudgeKeepHead+JudgeKeepTail {
		head = segments[:JudgeKeepHead]
		tail = segments[len(segments)-JudgeKeepTail:]
	} else {
		// There are not enough segments to keep both ends. Keep the
		// first half and the last half, and elide the middle.
		half := len(segments) / 2
		head = segments[:half]
		tail = segments[half:]
	}
	elided := len(segments) - len(head) - len(tail)
	if elided < 0 {
		elided = 0
	}

	var b strings.Builder
	for _, s := range head {
		b.WriteString(s.Text)
	}
	fmt.Fprintf(&b, "[%d turn(s) elided from the middle of the transcript]\n", elided)
	for _, s := range tail {
		b.WriteString(s.Text)
	}
	return b.String(), true, elided
}

// rawVerdict is the judge's answer, as the judge writes it.
type rawVerdict struct {
	LogicalPath     *bool    `json:"logical_path"`
	AgreesWithCheck *bool    `json:"agrees_with_check"`
	Pathologies     []string `json:"pathologies"`
	NeedsOwner      *bool    `json:"needs_owner"`
	Reason          string   `json:"reason"`
}

// parseVerdict reads the judge's answer.
//
// The judge is asked for one JSON object, and a real model sometimes
// wraps it in prose. The object is found inside the answer, so a stray
// sentence does not lose the verdict.
func parseVerdict(answer string) (rawVerdict, error) {
	text := strings.TrimSpace(answer)
	if text == "" {
		return rawVerdict{}, fmt.Errorf("eval: the judge returned an empty answer")
	}

	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return rawVerdict{}, fmt.Errorf("eval: the judge's answer holds no JSON object: %s", truncate(text, 200))
	}
	var raw rawVerdict
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return rawVerdict{}, fmt.Errorf("eval: decoding the judge's answer: %w", err)
	}
	if raw.LogicalPath == nil || raw.AgreesWithCheck == nil || raw.NeedsOwner == nil {
		return rawVerdict{}, fmt.Errorf("eval: the judge's answer is missing a field: %s", truncate(text, 200))
	}
	return raw, nil
}

// writeSummary writes the summary of a judging run.
//
// The summary lists the transcripts a human should read, and counts the
// pathologies per model.
func (j *Judge) writeSummary(result *JudgeResult) error {
	var b strings.Builder
	b.WriteString("# Judgment summary\n\n")

	b.WriteString("## Needs owner\n\n")
	var flagged []Verdict
	for _, v := range result.Verdicts {
		if v.NeedsOwner {
			flagged = append(flagged, v)
		}
	}
	if len(flagged) == 0 {
		b.WriteString("No transcript needs the owner.\n")
	} else {
		b.WriteString("| task | model | seed | logical | agrees | pathologies | reason |\n")
		b.WriteString("|---|---|---|---|---|---|---|\n")
		for _, v := range flagged {
			fmt.Fprintf(&b, "| %s | %s | %d | %t | %t | %s | %s |\n",
				v.Task, v.Model, v.Seed, v.LogicalPath, v.AgreesWithCheck,
				strings.Join(pathologyNames(v.Pathologies), ", "), v.Reason)
		}
	}

	b.WriteString("\n## Pathologies per model\n\n")
	counts := map[string]map[Pathology]int{}
	for _, v := range result.Verdicts {
		m, ok := counts[v.Model]
		if !ok {
			m = map[Pathology]int{}
			counts[v.Model] = m
		}
		for _, p := range v.Pathologies {
			m[p]++
		}
	}
	b.WriteString("| model |")
	for _, p := range allPathologies() {
		fmt.Fprintf(&b, " %s |", p)
	}
	b.WriteString(" transcripts |\n|")
	for range allPathologies() {
		b.WriteString("---|")
	}
	b.WriteString("---|\n")
	for _, model := range sortedKeys(counts) {
		fmt.Fprintf(&b, "| %s |", model)
		for _, p := range allPathologies() {
			fmt.Fprintf(&b, " %d |", counts[model][p])
		}
		fmt.Fprintf(&b, " %d |\n", countVerdicts(result.Verdicts, model))
	}

	b.WriteString("\n## All verdicts\n\n")
	b.WriteString("| task | model | seed | logical | agrees | needs owner | pathologies | reason |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, v := range result.Verdicts {
		fmt.Fprintf(&b, "| %s | %s | %d | %t | %t | %t | %s | %s |\n",
			v.Task, v.Model, v.Seed, v.LogicalPath, v.AgreesWithCheck, v.NeedsOwner,
			strings.Join(pathologyNames(v.Pathologies), ", "), v.Reason)
	}

	if err := os.WriteFile(filepath.Join(j.opts.OutDir, "summary.md"), []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("eval: writing the judgment summary: %w", err)
	}
	return nil
}

// countVerdicts returns the number of verdicts of one model.
func countVerdicts(verdicts []Verdict, model string) int {
	n := 0
	for _, v := range verdicts {
		if v.Model == model {
			n++
		}
	}
	return n
}

// allPathologies returns the pathology names, in a fixed order.
//
// The order is the order the judge prompt names them, so the table's
// columns do not move between runs.
func allPathologies() []Pathology {
	return []Pathology{
		PathologyToolCallParse,
		PathologyPrematureEndTurn,
		PathologyContextBlowup,
		PathologyTTFTGrowth,
		PathologyLoopOrReread,
		PathologyOffTaskEdit,
		PathologyOther,
	}
}

// pathologyNames renders a list of pathologies for a table.
func pathologyNames(pathologies []Pathology) []string {
	out := make([]string, 0, len(pathologies))
	for _, p := range pathologies {
		out = append(out, string(p))
	}
	return out
}

// normalizePathologies maps a judge's pathology names onto the known
// set.
//
// A name the harness does not know becomes "other", so the table's
// columns stay fixed and the judge's own words are kept in the reason.
func normalizePathologies(names []string) []Pathology {
	out := make([]Pathology, 0, len(names))
	for _, n := range names {
		p := Pathology(strings.TrimSpace(n))
		if knownPathologies[p] {
			out = append(out, p)
			continue
		}
		out = append(out, PathologyOther)
	}
	return out
}

// verdictFileName returns the file name a verdict is written under.
func verdictFileName(v Verdict) string {
	return fmt.Sprintf("%s__%s__seed%d.json", v.Task, v.Model, v.Seed)
}

// seedNumber reads the seed out of a directory name.
//
// The name is "seed" and a number. A name that is not that shape has no
// seed, and the answer is zero.
func seedNumber(name string) int {
	rest, ok := strings.CutPrefix(name, "seed")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0
	}
	return n
}

// truncate shortens a string to n characters.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
