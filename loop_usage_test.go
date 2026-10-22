package poo_test

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/sdougbrown/poo"
)

// usageReader is a stream reader that fails and reports the usage of
// the request it read.
//
// It is the shape a transport has when its stream breaks before the
// endpoint's usage chunk arrives.
type usageReader struct {
	// usage is the report the reader holds.
	usage *poo.UsageReport
	// sent reports whether the reader already gave its usage out.
	sent bool
}

// Next fails the stream without yielding an event.
func (r *usageReader) Next() (poo.StreamEvent, error) {
	if r.sent {
		return poo.StreamEvent{}, io.EOF
	}
	r.sent = true
	return poo.StreamEvent{}, &poo.ProviderError{
		Kind:    poo.ProviderKindNetwork,
		Message: "the stream broke",
	}
}

// Close releases the reader.
func (r *usageReader) Close() error { return nil }

// ReportUsage returns the usage the request produced.
func (r *usageReader) ReportUsage() *poo.UsageReport { return r.usage }

// usageTransport answers with a reader that reports its usage.
type usageTransport struct {
	// reader is the reader the transport hands out.
	reader *usageReader
}

// Stream hands out the reader.
func (t *usageTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	return t.reader, poo.Rendered{API: poo.APIChatCompletions}, nil
}

// Fork is not implemented by the fake: the compaction seam arrives with
// the fork.
func (t *usageTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return nil, fmt.Errorf("usageTransport: no fork")
}

// TestTheUsageFallbackFillsTheUsageRecord covers a transport whose stream
// breaks before the endpoint's usage arrives: the request still has a
// usage record, taken from the transport's own report.
func TestTheUsageFallbackFillsTheUsageRecord(t *testing.T) {
	cached := 7
	tr := &usageTransport{reader: &usageReader{usage: &poo.UsageReport{
		InputTokens:     120,
		OutputTokens:    8,
		CacheReadTokens: &cached,
		TTFTMS:          42.5,
	}}}
	sink := &recordingSink{}
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
		t.Fatalf("the log holds %d usage records, want 1", sink.count("usage"))
	}
	var usage *poo.Usage
	for _, rec := range sink.records {
		if u, ok := rec.(*poo.Usage); ok {
			usage = u
		}
	}
	if usage == nil {
		t.Fatal("the log holds no usage record")
	}
	if usage.InputTokens != 120 || usage.OutputTokens != 8 {
		t.Errorf("the usage is %d/%d, want 120/8", usage.InputTokens, usage.OutputTokens)
	}
	if usage.CacheReadTokens == nil || *usage.CacheReadTokens != 7 {
		t.Errorf("the cache-read count is %v, want 7", usage.CacheReadTokens)
	}
	if usage.TTFTMS != 42.5 {
		t.Errorf("the time to the first token is %v, want 42.5", usage.TTFTMS)
	}
}
