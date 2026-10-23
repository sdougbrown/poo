package pootest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// TestAStoppedJobCallNamesTheSentinelAndTheCause pins the stop error's
// shape: a cancelled job call names ErrJobStopped and still recognizes
// the cancellation underneath it.
func TestAStoppedJobCallNamesTheSentinelAndTheCause(t *testing.T) {
	tool := pootest.NewJobTool("jobs", poo.BackgroundAllowed)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var prepared poo.Prepared
	_, err := tool.Call(ctx, prepared, poo.ToolEnv{})
	if !errors.Is(err, pootest.ErrJobStopped) {
		t.Errorf("the stop error does not name ErrJobStopped: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the stop error does not name the cancellation: %v", err)
	}
}