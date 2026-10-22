package perm_test

import (
 "context"
 "fmt"
 "strings"
 "testing"
 "time"

 "github.com/sdougbrown/poo"
 "github.com/sdougbrown/poo/perm"
 "github.com/sdougbrown/poo/pootest"
 readtool "github.com/sdougbrown/poo/tool/read"
)

func reviewPolicy(t *testing.T,opts perm.Options)*perm.Policy {t.Helper();p,err:=perm.New(opts);if err!=nil{t.Fatal(err)};return p}

func TestReviewPrepareFailureTriggersNearAction(t *testing.T) {
 a := &fakeAsker{available:true,decision:perm.DecisionDeny}
 p := reviewPolicy(t,perm.Options{Askers:[]perm.Asker{a},DoomLoop:perm.DoomLoop{Threshold:100,NearThreshold:5}})
 tool := &readTool{prepareErr:poo.InvalidArgsf("invalid offset")}
 var turns []pootest.Turn
 for i:=1;i<=5;i++ {turns=append(turns,pootest.Turn{Calls:[]pootest.Call{readCall(fmt.Sprint(i),"a.go",i)}})}
 turns=append(turns,pootest.Turn{Text:"done"})
 s:=newSession(t,turns,map[string]poo.Tool{"read":tool},[]string{"read"},p);s.run(t)
 if a.Asks()!=1 {t.Fatalf("fifth Prepare failure: counts=%+v, asks=%d, last result=%q; want doom_loop ask",s.session.RepeatCountsFor("r5"),a.Asks(),s.lastResult(t))}
}

func TestReviewRepeatsAreConsecutive(t *testing.T) {
 for _,acrossTurns:=range []bool{false,true} {t.Run(fmt.Sprint(acrossTurns),func(t *testing.T){
  p:=reviewPolicy(t,perm.Options{DoomLoop:perm.DoomLoop{Action:perm.ActionDeny}})
  tool:=&readTool{}
  var turns []pootest.Turn
  var calls []pootest.Call
  for i,path:=range []string{"a.go","b.go","a.go","b.go","a.go"} {c:=readCall(fmt.Sprint(i),path,0);if acrossTurns{turns=append(turns,pootest.Turn{Calls:[]pootest.Call{c}})}else{calls=append(calls,c)}}
  if !acrossTurns{turns=append(turns,pootest.Turn{Calls:calls})};turns=append(turns,pootest.Turn{Text:"done"})
  s:=newSession(t,turns,map[string]poo.Tool{"read":tool},[]string{"read"},p);s.run(t)
  if tool.Calls()!=5 {t.Fatalf("alternating paths: executed=%d, fifth counts=%+v; want 5 and exact=near=1",tool.Calls(),s.session.RepeatCountsFor("r5"))}
 })}
}

func TestReviewAllowResetWithinBatch(t *testing.T) {
 p:=reviewPolicy(t,perm.Options{Unmatched:perm.ActionAllow,DoomLoop:perm.DoomLoop{Action:perm.ActionAllow}})
 tool:=&bashTool{}
 var calls []pootest.Call
 for i:=1;i<=4;i++ {calls=append(calls,bashCall(fmt.Sprint(i),"echo ok"))}
 s:=newSession(t,[]pootest.Turn{{Calls:calls},{Text:"done"}},map[string]poo.Tool{"bash":tool},[]string{"bash"},p);s.run(t)
 if got:=s.session.RepeatCountsFor("r4").Exact;got!=1{t.Fatalf("fourth same-batch call count=%d; want 1 after third call reset",got)}
}

func TestReviewPreparedDefaultsShareExactKey(t *testing.T) {
 p:=reviewPolicy(t,perm.Options{DoomLoop:perm.DoomLoop{Action:perm.ActionDeny}})
 tool:=readtool.New()
 // All three calls Prepare to the same path, offset=1, limit=2000.
 args:=[]string{`{"path":"does-not-exist"}`,`{"path":"does-not-exist","offset":1}`,`{"path":"does-not-exist","offset":1,"limit":2000}`}
 var calls []pootest.Call
 for i,arg:=range args{calls=append(calls,pootest.Call{ID:fmt.Sprint(i),Name:"read",Arguments:[]byte(arg)})}
 s:=newSession(t,[]pootest.Turn{{Calls:calls},{Text:"done"}},map[string]poo.Tool{"read":tool},[]string{"read"},p);s.run(t)
 if got:=s.session.RepeatCountsFor("r3").Exact;got!=3{t.Fatalf("equivalent Prepared calls: third exact=%d, near=%d; want exact=3; third result=%q",got,s.session.RepeatCountsFor("r3").Near,s.lastResult(t))}
}

func TestReviewDoomActionsEmitAnswer(t *testing.T) {
 for _,action:=range []perm.Action{perm.ActionAllow,perm.ActionDeny}{t.Run(string(action),func(t *testing.T){
  p:=reviewPolicy(t,perm.Options{DoomLoop:perm.DoomLoop{Action:action}});tool:=&readTool{}
  s:=newSession(t,[]pootest.Turn{{Calls:[]pootest.Call{readCall("1","a",0),readCall("2","a",0),readCall("3","a",0)}},{Text:"done"}},map[string]poo.Tool{"read":tool},[]string{"read"},p);s.run(t)
  if got:=len(s.events(t,poo.EventPermissionAnswered));got!=1{t.Fatalf("doom_loop action=%s: permission.answered=%d; want 1",action,got)}
 })}
}

func TestReviewPermissionsReachRecordSink(t *testing.T) {
 p:=reviewPolicy(t,perm.Options{});tool:=&bashTool{}
 s:=newSession(t,[]pootest.Turn{{Calls:[]pootest.Call{bashCall("1","echo ok")}},{Text:"done"}},map[string]poo.Tool{"bash":tool},[]string{"bash"},p);s.run(t)
 names:=s.sink.Names();count:=0
 for _,name:=range names{if strings.HasPrefix(name,"permission."){count++}}
 if count!=2{t.Fatalf("permission events=%d/%d but permission records=%d; records=%v",len(s.events(t,poo.EventPermissionAsked)),len(s.events(t,poo.EventPermissionAnswered)),count,names)}
}

type reviewPausedAvailability struct{ started,release chan struct{} }
func(a *reviewPausedAvailability)Available()bool{close(a.started);<-a.release;return true}
func(*reviewPausedAvailability)Budget()time.Duration{return time.Minute}
func(*reviewPausedAvailability)Ask(context.Context,perm.Ask)(perm.Decision,error){return perm.DecisionAllow,nil}

func TestReviewPermissionAnswerBeforeSettled(t *testing.T) {
 a:=&reviewPausedAvailability{started:make(chan struct{}),release:make(chan struct{})}
 p:=reviewPolicy(t,perm.Options{Askers:[]perm.Asker{a},Fallback:perm.ActionAllow});tool:=&bashTool{}
 s:=newSession(t,[]pootest.Turn{{Calls:[]pootest.Call{bashCall("1","echo ok")}},{Text:"done"}},map[string]poo.Tool{"bash":tool},[]string{"bash"},p)
 if _,err:=s.session.Prompt(context.Background(),"start");err!=nil{t.Fatal(err)}
 <-a.started
 s.session.Cancel("review")
 ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
 if _,err:=s.session.Wait(ctx);err!=nil{close(a.release);t.Fatal(err)}
 close(a.release)
 results:=0
 for _,rec:=range s.sink.Records(){if result,ok:=rec.(*poo.ToolResultRecord);ok{results++;t.Logf("sealed pending call outcome=%s text=%q",result.Outcome,result.Text)}}
 if results!=1{t.Errorf("cancelled call has %d result records; want exactly 1",results)}
 answer:=s.waitForEvent(t,poo.EventPermissionAnswered)
 for _,ev:=range s.session.Events(){if ev.Name==poo.EventRunSettled&&answer.Seq>ev.Seq{t.Fatalf("permission.answered seq=%d after run.settled seq=%d (decision=%v provenance=%v)",answer.Seq,ev.Seq,answer.Payload["decision"],answer.Payload["provenance"])}}
}

func TestReviewMalformedClassRejected(t *testing.T) {
 p,err:=perm.New(perm.Options{Rules:[]perm.Rule{{Class:"side_effectng",Action:perm.ActionDeny}},Unmatched:perm.ActionAllow})
 if err==nil{d,_:=p.BeforeTool(poo.HookContext{Context:context.Background()},poo.ToolCallInfo{Spec:poo.ToolSpec{Name:"bash",Class:poo.ClassSideEffecting}});t.Fatalf("unknown Class accepted; side-effecting call allowed=%v; want construction error",d.Allowed)}
}
