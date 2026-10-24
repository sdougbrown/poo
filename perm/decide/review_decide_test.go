package decide_test

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/perm"
	permissive "github.com/sdougbrown/poo/perm/decide"
)

func TestReviewInvalidProbabilitiesDefer(t *testing.T) {
	for name, probs := range map[string]map[string]float64{
		"above_one":       {"yes": 2, "no": -1},
		"NaN_other_label": {"yes": 0.99, "no": math.NaN()},
		"infinite":        {"yes": math.Inf(1), "no": 0},
		"missing_no":      {"yes": 0.99},
		"not_normalized":  {"yes": 0.99, "no": 0.99},
	} {
		t.Run(name, func(t *testing.T) {
			d := decide.DeciderFunc(func(context.Context, any, string, []decide.Question) ([]decide.Answer, error) {
				return []decide.Answer{{ID: "allow", Probabilities: probs}}, nil
			})
			a := permissive.New(d, permissive.Options{})
			got, err := a.Ask(context.Background(), perm.Ask{})
			if got != perm.DecisionDefer {
				t.Fatalf("malformed distribution %v => %s (%v); want defer", probs, got, err)
			}
		})
	}
}

func TestReviewPendingCallIdentifiesDecisionTarget(t *testing.T) {
	var requests []string
	d := decide.DeciderFunc(func(_ context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
		raw, err := json.Marshal([]any{state, instructions, qs})
		if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, string(raw))
		return []decide.Answer{{ID: "allow", Probabilities: map[string]float64{"yes": 0.99, "no": 0.01}}}, nil
	})
	history := poo.NewHistoryView([]poo.Message{poo.NewTextMessage(poo.RoleUser, "clean build artifacts")}, nil, nil)
	a := permissive.New(d, permissive.Options{})
	for _, summary := range []string{"rm -rf ./dist", "rm -rf ~"} {
		_, err := a.Ask(context.Background(), perm.Ask{Tool: "bash", Summary: summary, Class: poo.ClassSideEffecting, Reason: poo.ReasonPolicy, HistoryView: history})
		if err != nil {
			t.Fatal(err)
		}
	}
	if requests[0] == requests[1] {
		t.Fatalf("different pending calls sent IDENTICAL decision requests: %s", requests[0])
	}
}
