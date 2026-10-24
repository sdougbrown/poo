package decide

import (
	"context"
	"errors"
	"testing"
)

type panickingDecider struct{ metadata bool }

func (d panickingDecider) Decide(context.Context, any, string, []Question) ([]Answer, error) {
	if !d.metadata {
		panic("decide failure")
	}
	return []Answer{{ID: "allow", Probabilities: map[string]float64{"yes": 1}}}, nil
}
func (panickingDecider) Requests() int { panic("requests failure") }

func TestRecordedDeciderPanicsDefer(t *testing.T) {
	for _, metadata := range []bool{false, true} {
		t.Run(map[bool]string{false: "answer", true: "metadata"}[metadata], func(t *testing.T) {
			var records []Decision
			d := Recorded(panickingDecider{metadata: metadata}, SinkFunc(func(_ context.Context, r Decision) error { records = append(records, r); return nil }), "test")
			answers, err := d.Decide(context.Background(), nil, "", nil)
			if !errors.Is(err, ErrDeferred) || len(answers) != 0 {
				t.Fatalf("answers=%v err=%v", answers, err)
			}
			if len(records) != 1 || records[0].Outcome != ResultDeferred {
				t.Fatalf("records=%+v", records)
			}
		})
	}
}
