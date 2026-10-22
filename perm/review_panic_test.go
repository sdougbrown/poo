package perm

import (
 "context"
 "testing"
 "time"
)

func TestReviewAskerPanicDefers(t *testing.T) {
 a := &reviewFuncAsker{fn:func(context.Context)(Decision,error){panic("review: asker panicked")}}
 c := &chain{askers:[]Asker{a},timeout:time.Hour,fallback:ActionDeny,clock:systemClock{}}
 got := c.ask(context.Background(),Ask{})
 if got.decision != DecisionDeny { t.Fatalf("got %+v; want fallback deny",got) }
}
