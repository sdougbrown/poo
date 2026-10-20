package eval

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Aggregate is the summary of a set of runs.
//
// Every number here is a mean over the runs, with the spread across
// seeds beside it, so a reader sees both the level and the noise.
//
// The spread of a metric is the standard deviation of the metric's
// per-seed means, not of the individual runs: the runs inside one seed
// share whatever the seed gave them, and the seed is the unit the plan's
// noise floor names.
type Aggregate struct {
	// Runs is the number of runs behind the numbers.
	Runs int `json:"runs"`
	// Seeds is the number of seeds behind the numbers.
	Seeds int `json:"seeds"`
	// SuccessRate is the share of runs whose check passed, in [0,1].
	SuccessRate float64 `json:"success_rate"`
	// SuccessSpread is the standard deviation of the per-seed success
	// rates, in points. It is the noise floor's input.
	SuccessSpread float64 `json:"success_spread"`
	// Turns, InputTokens, OutputTokens and WallMS are the means.
	Turns        float64 `json:"turns"`
	TurnsSpread  float64 `json:"turns_spread"`
	InputTokens  float64 `json:"input_tokens"`
	InputSpread  float64 `json:"input_tokens_spread"`
	OutputTokens float64 `json:"output_tokens"`
	OutputSpread float64 `json:"output_tokens_spread"`
	WallMS       float64 `json:"wall_ms"`
	WallSpread   float64 `json:"wall_ms_spread"`
	// TTFTMS is the mean time to the first token.
	TTFTMS     float64 `json:"ttft_ms"`
	TTFTSpread float64 `json:"ttft_ms_spread"`
	// TTFTTrend is the mean per-turn trend of the time to the first
	// token, in milliseconds per turn.
	TTFTTrend       float64 `json:"ttft_trend_ms_per_turn"`
	TTFTTrendSpread float64 `json:"ttft_trend_ms_per_turn_spread"`
	// CacheHitRatio is the mean cached share of the prompt tokens, and
	// nil when no endpoint reported cache counts.
	CacheHitRatio *float64 `json:"cache_hit_ratio,omitempty"`
	// ToolErrorRate is the mean failed share of the tool calls.
	ToolErrorRate   float64 `json:"tool_error_rate"`
	ToolErrorSpread float64 `json:"tool_error_rate_spread"`
	// TextParseRate is the mean unparsable share of the tool calls.
	TextParseRate   float64 `json:"text_parse_rate"`
	TextParseSpread float64 `json:"text_parse_rate_spread"`
	// RereadRate is the mean re-read share of the reads.
	RereadRate   float64 `json:"reread_rate"`
	RereadSpread float64 `json:"reread_rate_spread"`
	// DiffLines is the mean size of the diff, in changed lines.
	DiffLines float64 `json:"diff_lines"`
	// UnexpectedFiles is the number of runs that changed a path outside
	// the task's expected paths.
	UnexpectedFiles int `json:"runs_with_unexpected_files"`
	// SettleReasons counts the runs by their settle reason.
	SettleReasons map[string]int `json:"settle_reasons"`
	// TTFTGrows is the number of runs whose time to the first token grew
	// with the prefix.
	TTFTGrows int `json:"runs_with_ttft_growth"`
}

// ModelSummary is the aggregate of one model's runs.
type ModelSummary struct {
	// Model is the model's catalog name.
	Model string `json:"model"`
	// Overall is the aggregate over every task.
	Overall Aggregate `json:"overall"`
	// ByTask holds the aggregate of each task, keyed by task id.
	ByTask map[string]Aggregate `json:"by_task"`
}

// TaskSummary is the aggregate of one task's runs, across models.
type TaskSummary struct {
	// Task is the task's id.
	Task string `json:"task"`
	// ByModel holds the aggregate of each model, keyed by model name.
	ByModel map[string]Aggregate `json:"by_model"`
}

// Summary is the whole report's aggregate.
type Summary struct {
	// Models holds each model's aggregate.
	Models []ModelSummary `json:"models"`
	// Tasks holds each task's aggregate.
	Tasks []TaskSummary `json:"tasks"`
	// Overall is the aggregate over every run.
	Overall Aggregate `json:"overall"`
}

// Summarize aggregates a report.
//
// The aggregates are grouped by model and by task, and the numbers are
// rounded so two runs of the same thing produce the same JSON.
func Summarize(report *Report) Summary {
	summary := Summary{Overall: aggregate(report.Results)}

	byModel := map[string][]RunResult{}
	var modelOrder []string
	for _, r := range report.Results {
		if _, ok := byModel[r.Model]; !ok {
			modelOrder = append(modelOrder, r.Model)
		}
		byModel[r.Model] = append(byModel[r.Model], r)
	}
	sort.Strings(modelOrder)
	for _, m := range modelOrder {
		runs := byModel[m]
		ms := ModelSummary{Model: m, Overall: aggregate(runs), ByTask: map[string]Aggregate{}}
		byTask := map[string][]RunResult{}
		for _, r := range runs {
			byTask[r.Task] = append(byTask[r.Task], r)
		}
		for _, t := range sortedKeys(byTask) {
			ms.ByTask[t] = aggregate(byTask[t])
		}
		summary.Models = append(summary.Models, ms)
	}

	byTask := map[string][]RunResult{}
	var taskOrder []string
	for _, r := range report.Results {
		if _, ok := byTask[r.Task]; !ok {
			taskOrder = append(taskOrder, r.Task)
		}
		byTask[r.Task] = append(byTask[r.Task], r)
	}
	sort.Strings(taskOrder)
	for _, t := range taskOrder {
		ts := TaskSummary{Task: t, ByModel: map[string]Aggregate{}}
		byModel := map[string][]RunResult{}
		for _, r := range byTask[t] {
			byModel[r.Model] = append(byModel[r.Model], r)
		}
		for _, m := range sortedKeys(byModel) {
			ts.ByModel[m] = aggregate(byModel[m])
		}
		summary.Tasks = append(summary.Tasks, ts)
	}

	return summary
}

// aggregate computes the aggregate of a set of runs.
//
// The spread of a metric is the standard deviation of the metric's
// per-seed means. The runs inside one seed share whatever the seed gave
// them, so the seed is the unit the noise floor names, and the spread of
// the per-seed means is the noise.
func aggregate(runs []RunResult) Aggregate {
	agg := Aggregate{Runs: len(runs), SettleReasons: map[string]int{}}
	if len(runs) == 0 {
		return agg
	}

	// The runs of each seed, so a metric's per-seed means can be found.
	bySeed := map[int][]RunResult{}
	for _, r := range runs {
		bySeed[r.Seed] = append(bySeed[r.Seed], r)
	}
	seeds := sortedInts(bySeed)
	agg.Seeds = len(seeds)

	// spreadOf returns the standard deviation of a metric's per-seed
	// means.
	spreadOf := func(get func(RunResult) float64) float64 {
		var perSeed []float64
		for _, s := range seeds {
			var values []float64
			for _, r := range bySeed[s] {
				values = append(values, get(r))
			}
			perSeed = append(perSeed, mean(values))
		}
		return stdev(perSeed)
	}

	var turns, input, output, wall, ttft, trend, toolErr, parse, reread, diffLines []float64
	var cacheRatios []float64
	cacheReported := false

	for _, r := range runs {
		turns = append(turns, float64(r.Metrics.Turns))
		input = append(input, float64(r.Metrics.InputTokens))
		output = append(output, float64(r.Metrics.OutputTokens))
		wall = append(wall, r.Metrics.WallMS)
		ttft = append(ttft, r.Metrics.TTFTMS)
		trend = append(trend, r.Metrics.TTFTTrend)
		toolErr = append(toolErr, r.Metrics.ToolErrorRate)
		parse = append(parse, r.Metrics.TextParseRate)
		reread = append(reread, r.Metrics.RereadRate)
		diffLines = append(diffLines, float64(r.Metrics.DiffLines))
		if r.Metrics.CacheHitRatio != nil {
			cacheReported = true
			cacheRatios = append(cacheRatios, *r.Metrics.CacheHitRatio)
		}
		if len(r.Metrics.UnexpectedFiles) > 0 {
			agg.UnexpectedFiles++
		}
		if r.Metrics.TTFTGrowsWithPrefix {
			agg.TTFTGrows++
		}
		agg.SettleReasons[r.Metrics.SettleReason]++
	}

	var successes int
	for _, r := range runs {
		if r.Success {
			successes++
		}
	}
	agg.SuccessRate = float64(successes) / float64(len(runs))
	agg.SuccessSpread = spreadOf(func(r RunResult) float64 {
		if r.Success {
			return 1
		}
		return 0
	})

	agg.Turns = round2(mean(turns))
	agg.TurnsSpread = round2(spreadOf(func(r RunResult) float64 { return float64(r.Metrics.Turns) }))
	agg.InputTokens = round2(mean(input))
	agg.InputSpread = round2(spreadOf(func(r RunResult) float64 { return float64(r.Metrics.InputTokens) }))
	agg.OutputTokens = round2(mean(output))
	agg.OutputSpread = round2(spreadOf(func(r RunResult) float64 { return float64(r.Metrics.OutputTokens) }))
	agg.WallMS = round2(mean(wall))
	agg.WallSpread = round2(spreadOf(func(r RunResult) float64 { return r.Metrics.WallMS }))
	agg.TTFTMS = round2(mean(ttft))
	agg.TTFTSpread = round2(spreadOf(func(r RunResult) float64 { return r.Metrics.TTFTMS }))
	agg.TTFTTrend = round2(mean(trend))
	agg.TTFTTrendSpread = round2(spreadOf(func(r RunResult) float64 { return r.Metrics.TTFTTrend }))
	agg.ToolErrorRate = round4(mean(toolErr))
	agg.ToolErrorSpread = round4(spreadOf(func(r RunResult) float64 { return r.Metrics.ToolErrorRate }))
	agg.TextParseRate = round4(mean(parse))
	agg.TextParseSpread = round4(spreadOf(func(r RunResult) float64 { return r.Metrics.TextParseRate }))
	agg.RereadRate = round4(mean(reread))
	agg.RereadSpread = round4(spreadOf(func(r RunResult) float64 { return r.Metrics.RereadRate }))
	agg.DiffLines = round2(mean(diffLines))
	if cacheReported {
		v := round4(mean(cacheRatios))
		agg.CacheHitRatio = &v
	}
	return agg
}

// round4 rounds a number to four decimals.
//
// The rates need more precision than the counts do: a rate of 0.125 is a
// real number, and rounding it to 0.13 loses the difference between two
// runs.
func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

// WriteReport writes a report as JSON and as a markdown table.
//
// The two files share a base name, so a directory holds one run's report.
func WriteReport(dir string, report *Report) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("eval: creating %s: %w", dir, err)
	}
	report = portable(dir, report)

	summary := Summarize(report)

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: encoding the summary: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), data, 0o644); err != nil {
		return fmt.Errorf("eval: writing the summary: %w", err)
	}

	full, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: encoding the report: %w", err)
	}
	full = append(full, '\n')
	if err := os.WriteFile(filepath.Join(dir, "report.json"), full, 0o644); err != nil {
		return fmt.Errorf("eval: writing the report: %w", err)
	}

	markdown := RenderMarkdown(report, summary)
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(markdown), 0o644); err != nil {
		return fmt.Errorf("eval: writing the report: %w", err)
	}
	return nil
}

// RenderMarkdown renders a report as a markdown table.
//
// The table has one row per model and one per task, plus a row for the
// whole run, so a reader sees the level and the shape without opening
// the JSON.
func RenderMarkdown(report *Report, summary Summary) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# poo eval report\n\n")
	fmt.Fprintf(&b, "- started: %s\n", report.StartedAt)
	fmt.Fprintf(&b, "- finished: %s\n", report.FinishedAt)
	fmt.Fprintf(&b, "- git sha: %s\n", report.GitSHA)
	fmt.Fprintf(&b, "- runs: %d\n\n", len(report.Results))

	b.WriteString("## By model\n\n")
	b.WriteString("| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, m := range summary.Models {
		a := m.Overall
		fmt.Fprintf(&b, "| %s | %d | %.1f%% | %.1f | %.1f | %.0f | %.0f | %.0f | %.0f | %.2f | %.1f%% | %.1f%% | %.1f%% | %.0f | %s |\n",
			m.Model, a.Runs, a.SuccessRate*100, a.SuccessSpread*100, a.Turns, a.InputTokens, a.OutputTokens,
			a.WallMS, a.TTFTMS, a.TTFTTrend, a.ToolErrorRate*100, a.TextParseRate*100, a.RereadRate*100,
			a.DiffLines, formatRatio(a.CacheHitRatio))
	}
	a := summary.Overall
	fmt.Fprintf(&b, "| **all** | %d | %.1f%% | %.1f | %.1f | %.0f | %.0f | %.0f | %.0f | %.2f | %.1f%% | %.1f%% | %.1f%% | %.0f | %s |\n",
		a.Runs, a.SuccessRate*100, a.SuccessSpread*100, a.Turns, a.InputTokens, a.OutputTokens,
		a.WallMS, a.TTFTMS, a.TTFTTrend, a.ToolErrorRate*100, a.TextParseRate*100, a.RereadRate*100,
		a.DiffLines, formatRatio(a.CacheHitRatio))

	b.WriteString("\n## By task\n\n")
	b.WriteString("| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, t := range summary.Tasks {
		for _, m := range sortedKeys(t.ByModel) {
			x := t.ByModel[m]
			fmt.Fprintf(&b, "| %s | %s | %d | %.1f%% | %.1f | %.1f | %.2f | %.1f%% | %.1f%% | %.0f |\n",
				t.Task, m, x.Runs, x.SuccessRate*100, x.SuccessSpread*100, x.Turns, x.TTFTTrend,
				x.ToolErrorRate*100, x.RereadRate*100, x.DiffLines)
		}
	}

	b.WriteString("\n## Settle reasons\n\n")
	b.WriteString("| model | reason | runs |\n")
	b.WriteString("|---|---|---|\n")
	for _, m := range summary.Models {
		for _, reason := range sortedKeys(m.Overall.SettleReasons) {
			fmt.Fprintf(&b, "| %s | %s | %d |\n", m.Model, reason, m.Overall.SettleReasons[reason])
		}
	}

	b.WriteString("\n## Runs\n\n")
	b.WriteString("| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range report.Results {
		ok := "no"
		if r.Success {
			ok = "yes"
		}
		note := ""
		if r.Error != "" {
			note = " (" + r.Error + ")"
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %d | %.0f | %d | %d | %d | `%s`%s |\n",
			r.Task, r.Model, r.Seed, ok, r.Metrics.SettleReason, r.Metrics.Turns, r.Metrics.TTFTMS,
			r.Metrics.ToolErrors, r.Metrics.Rereads, r.Metrics.DiffLines, r.LogPath, note)
	}

	return b.String()
}

// formatRatio formats an optional ratio for the table.
func formatRatio(r *float64) string {
	if r == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", *r*100)
}

// portable returns a copy of the report whose paths are relative to the
// report's own directory, so a committed report names no machine's
// home directory. The caller's report is left as it is.
func portable(dir string, report *Report) *Report {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	out := *report
	out.Dir = "."
	out.Results = make([]RunResult, len(report.Results))
	for i, r := range report.Results {
		if rel, err := filepath.Rel(dir, r.LogPath); err == nil && r.LogPath != "" {
			r.LogPath = rel
		}
		out.Results[i] = r
	}
	return &out
}
