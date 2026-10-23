package poo

import (
	"context"
	"testing"
)

func TestOnlyTheFinalizationsOwnCancelIsNotACandidate(t *testing.T) {
	for name, tc := range map[string]struct {
		cause error
		want  bool
	}{
		"finalize":   {errFinalizing, true},
		"interrupt":  {errInterrupted, false},
		"consumer":   {nil, false},
		"cancelling": {context.Canceled, false},
	} {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(tc.cause)
		if got := cancelledByFinalize(&runState{ctx: ctx}); got != tc.want {
			t.Errorf("%s: cancelledByFinalize is %v, want %v", name, got, tc.want)
		}
	}
	live, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	if cancelledByFinalize(&runState{ctx: live}) {
		t.Error("a live context reads as cancelled by finalize")
	}
}
