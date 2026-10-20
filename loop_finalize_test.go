package poo_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// cancellingSink is a record sink that honours the context it is handed.
//
// A sink that stores to a network or a database refuses the write once
// the context ends, so the final records of a cancelled run have to be
// written with a context of their own.
type cancellingSink struct {
	// records holds the records that were written.
	records []poo.Record
	// refused counts the writes the sink turned away.
	refused int
}

// Append writes the record, unless the context has ended.
func (s *cancellingSink) Append(ctx context.Context, rec poo.Record) error {
	if err := ctx.Err(); err != nil {
		s.refused++
		return err
	}
	s.records = append(s.records, rec)
	return nil
}

// Flush does nothing.
func (s *cancellingSink) Flush(ctx context.Context) error { return nil }

// count returns how many records carry a name.
func (s *cancellingSink) count(name string) int {
	n := 0
	for _, rec := range s.records {
		if rec.Meta().Record == name {
			n++
		}
	}
	return n
}

// TestACancelledRunSettlesIntoACancellingSink covers a run that is
// cancelled while the provider is answering: the settle record is still
// written, because the finalization of a run does not depend on the
// run's context.
func TestACancelledRunSettlesIntoACancellingSink(t *testing.T) {
	srv := stallingEndpoint(t)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	sink := &cancellingSink{}
	tool := &echoTool{result: "echoed"}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(ctx, "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1; the sink refused %d writes",
			sink.count("run.settled"), sink.refused)
	}
}

// TestAFailedRequestStillSettlesIntoACancellingSink covers a run whose
// provider request failed: the usage it produced and the settle record
// are both written.
func TestAFailedRequestStillSettlesIntoACancellingSink(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"content":"hi"}}]}` + "\n\n" +
		"data: " + `{"error":{"message":"the model crashed","type":"server_error"}}` + "\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	sink := &cancellingSink{}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if sink.count("usage") != 1 {
		t.Errorf("the log holds %d usage records, want 1", sink.count("usage"))
	}
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1", sink.count("run.settled"))
	}
}

// TestAFailingUsageWriteDoesNotStopTheSettle covers a sink that refuses
// the usage write: the run still settles, because the accounting is
// worth less than the record that says the run ended.
func TestAFailingUsageWriteDoesNotStopTheSettle(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"content":"hi"}}]}` + "\n\n" +
		"data: " + `{"error":{"message":"the model crashed","type":"server_error"}}` + "\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	sink := &failingUsageSink{}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1", sink.count("run.settled"))
	}
}

// failingUsageSink refuses the usage record and takes everything else.
type failingUsageSink struct {
	// records holds the records that were written.
	records []poo.Record
}

// Append refuses a usage record.
func (s *failingUsageSink) Append(ctx context.Context, rec poo.Record) error {
	if rec.Meta().Record == "usage" {
		return errors.New("the store is full")
	}
	s.records = append(s.records, rec)
	return nil
}

// Flush does nothing.
func (s *failingUsageSink) Flush(ctx context.Context) error { return nil }

// count returns how many records carry a name.
func (s *failingUsageSink) count(name string) int {
	n := 0
	for _, rec := range s.records {
		if rec.Meta().Record == name {
			n++
		}
	}
	return n
}
