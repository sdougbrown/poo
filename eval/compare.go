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

// The noise floor.
//
// A difference is a regression only when it is larger than the noise the
// runs themselves show. The floor is the larger of an absolute floor and
// twice the standard deviation of the larger side's seeds, so a run set
// that is noisy by itself never reports a regression.
const (
	// SuccessFloorPoints is the absolute floor for a success-rate
	// difference, in points.
	SuccessFloorPoints = 5.0
	// RateFloorPercent is the absolute floor for a rate difference, in
	// percent.
	RateFloorPercent = 20.0
)

// Comparison is the result of comparing two reports.
type Comparison struct {
	// Baseline and Candidate name the two reports.
	Baseline  string `json:"baseline"`
	Candidate string `json:"candidate"`
	// Regressions are the differences that exceed the noise floor.
	Regressions []Regression `json:"regressions"`
	// Improvements are the differences in the other direction.
	Improvements []Regression `json:"improvements"`
	// PerModel holds the compared numbers, per model.
	PerModel []ModelComparison `json:"per_model"`
}

// Regression is one difference between two reports.
type Regression struct {
	// Model is the model the difference belongs to.
	Model string `json:"model"`
	// Metric names the number that differs.
	Metric string `json:"metric"`
	// Baseline and Candidate are the two values.
	Baseline  float64 `json:"baseline"`
	Candidate float64 `json:"candidate"`
	// Delta is Candidate minus Baseline.
	Delta float64 `json:"delta"`
	// Floor is the noise floor the difference had to beat.
	Floor float64 `json:"floor"`
	// SD is the larger side's seed standard deviation.
	SD float64 `json:"sd"`
	// Worse reports which direction is worse: "lower" when a lower
	// value is better, "higher" when a higher value is better.
	Worse string `json:"worse"`
}

// ModelComparison holds the compared numbers of one model.
type ModelComparison struct {
	// Model is the model's name.
	Model string `json:"model"`
	// SuccessRate, SuccessSpread and the other numbers of each side.
	Baseline  Aggregate `json:"baseline"`
	Candidate Aggregate `json:"candidate"`
}

// metric is one compared number: the metric's name, the value of each
// side, and the direction that is better.
type metric struct {
	name string
	// get returns the value from an aggregate.
	get func(Aggregate) float64
	// spread returns the seed standard deviation of the metric.
	spread func(Aggregate) float64
	// higherIsBetter reports whether a bigger value is better.
	higherIsBetter bool
	// floor is the absolute floor of the noise floor, in the metric's
	// own unit.
	floor float64
	// relative reports whether the floor scales with the baseline value,
	// so a rate's floor grows with the rate.
	relative bool
}

// comparedMetrics are the numbers the comparison looks at.
//
// The success rate and the token counts are the plan's list. The tool
// error rate, the parse rate and the re-read rate come with it, because
// they are the numbers that tell a regression apart from a reroll.
func comparedMetrics() []metric {
	return []metric{
		{
			name:           "success_rate",
			get:            func(a Aggregate) float64 { return a.SuccessRate },
			spread:         func(a Aggregate) float64 { return a.SuccessSpread },
			higherIsBetter: true,
			floor:          SuccessFloorPoints / 100,
		},
		{
			name:           "input_tokens",
			get:            func(a Aggregate) float64 { return a.InputTokens },
			spread:         func(a Aggregate) float64 { return a.InputSpread },
			higherIsBetter: false,
			relative:       true,
		},
		{
			name:           "output_tokens",
			get:            func(a Aggregate) float64 { return a.OutputTokens },
			spread:         func(a Aggregate) float64 { return a.OutputSpread },
			higherIsBetter: false,
			relative:       true,
		},
		{
			name:           "tool_error_rate",
			get:            func(a Aggregate) float64 { return a.ToolErrorRate },
			spread:         func(a Aggregate) float64 { return a.ToolErrorSpread },
			higherIsBetter: false,
			floor:          RateFloorPercent / 100,
			relative:       true,
		},
		{
			name:           "text_parse_rate",
			get:            func(a Aggregate) float64 { return a.TextParseRate },
			spread:         func(a Aggregate) float64 { return a.TextParseSpread },
			higherIsBetter: false,
			floor:          RateFloorPercent / 100,
			relative:       true,
		},
		{
			name:           "reread_rate",
			get:            func(a Aggregate) float64 { return a.RereadRate },
			spread:         func(a Aggregate) float64 { return a.RereadSpread },
			higherIsBetter: false,
			floor:          RateFloorPercent / 100,
			relative:       true,
		},
	}
}

// Compare compares a baseline report with a candidate report.
//
// A difference is a regression when it is larger than the noise floor:
// max(an absolute floor, twice the larger side's seed standard
// deviation). A success-rate difference uses a floor of five points; a
// rate difference uses twenty percent; a token difference uses twenty
// percent of the baseline, because a tenth of a small token count and
// a tenth of a large one are not the same amount of work.
func Compare(baseline, candidate *Report) Comparison {
	cmp := Comparison{Baseline: baseline.Dir, Candidate: candidate.Dir}

	byModel := map[string][2]Aggregate{}
	var order []string
	add := func(r *Report, side int) {
		summary := Summarize(r)
		for _, m := range summary.Models {
			pair, ok := byModel[m.Model]
			if !ok {
				order = append(order, m.Model)
				pair = [2]Aggregate{}
			}
			pair[side] = m.Overall
			byModel[m.Model] = pair
		}
	}
	add(baseline, 0)
	add(candidate, 1)
	sort.Strings(order)

	for _, name := range order {
		pair := byModel[name]
		mc := ModelComparison{Model: name, Baseline: pair[0], Candidate: pair[1]}
		cmp.PerModel = append(cmp.PerModel, mc)

		for _, m := range comparedMetrics() {
			b := m.get(pair[0])
			c := m.get(pair[1])
			delta := c - b
			if delta == 0 {
				continue
			}
			// The noise is the larger side's spread: the run set that
			// is noisier decides how big a difference has to be.
			sd := math.Max(m.spread(pair[0]), m.spread(pair[1]))
			floor := m.floor
			if m.relative {
				floor = math.Max(floor, math.Abs(b)*RateFloorPercent/100)
			}
			if noise := 2 * sd; floor < noise {
				floor = noise
			}
			if math.Abs(delta) <= floor {
				continue
			}
			worse := "lower"
			if m.higherIsBetter {
				worse = "higher"
			}
			reg := Regression{
				Model:     name,
				Metric:    m.name,
				Baseline:  b,
				Candidate: c,
				Delta:     delta,
				Floor:     floor,
				SD:        sd,
				Worse:     worse,
			}
			isWorse := (m.higherIsBetter && delta < 0) || (!m.higherIsBetter && delta > 0)
			if isWorse {
				cmp.Regressions = append(cmp.Regressions, reg)
			} else {
				cmp.Improvements = append(cmp.Improvements, reg)
			}
		}
	}
	return cmp
}

// LoadReport reads a report from a directory.
//
// The directory holds report.json, which the runner wrote.
func LoadReport(dir string) (*Report, error) {
	path := filepath.Join(dir, "report.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: reading %s: %w", path, err)
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("eval: parsing %s: %w", path, err)
	}
	if report.Dir == "" {
		report.Dir = dir
	}
	return &report, nil
}

// RenderComparison renders a comparison as markdown.
func RenderComparison(cmp Comparison) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# poo eval comparison\n\n")
	fmt.Fprintf(&b, "- baseline: %s\n", cmp.Baseline)
	fmt.Fprintf(&b, "- candidate: %s\n", cmp.Candidate)
	fmt.Fprintf(&b, "- regressions: %d\n", len(cmp.Regressions))
	fmt.Fprintf(&b, "- improvements: %d\n\n", len(cmp.Improvements))

	b.WriteString("| model | metric | baseline | candidate | delta | floor | direction |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, r := range cmp.Regressions {
		fmt.Fprintf(&b, "| %s | %s | %.4g | %.4g | %+.4g | %.4g | worse (%s is better) |\n",
			r.Model, r.Metric, r.Baseline, r.Candidate, r.Delta, r.Floor, r.Worse)
	}
	for _, r := range cmp.Improvements {
		fmt.Fprintf(&b, "| %s | %s | %.4g | %.4g | %+.4g | %.4g | better (%s is better) |\n",
			r.Model, r.Metric, r.Baseline, r.Candidate, r.Delta, r.Floor, r.Worse)
	}
	if len(cmp.Regressions) == 0 && len(cmp.Improvements) == 0 {
		b.WriteString("| - | - | - | - | - | - | no difference above the noise floor |\n")
	}

	return b.String()
}

// WriteComparison writes a comparison as JSON and as markdown.
func WriteComparison(dir string, cmp Comparison) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("eval: creating %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(cmp, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: encoding the comparison: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, "comparison.json"), data, 0o644); err != nil {
		return fmt.Errorf("eval: writing the comparison: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comparison.md"), []byte(RenderComparison(cmp)), 0o644); err != nil {
		return fmt.Errorf("eval: writing the comparison: %w", err)
	}
	return nil
}
