package support

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/sdougbrown/poo"
)

// The retry policy.
//
// The plan: three attempts per request, the first try and two retries,
// with a backoff of one second and then two. A Retry-After overrides
// the backoff, capped at thirty seconds. A rate limit that asks for
// longer than that is not retried at all: it settles the run. The wait
// is cancellable, and a request is only retried while the attempt has
// carried no delta.

// Policy is the plan's retry policy.
type Policy struct {
	// Attempts is the number of attempts one request gets.
	Attempts int
	// Backoff holds the wait before the second and the third attempt.
	Backoff []time.Duration
	// MaxRetryAfter is the longest wait a rate limit may ask for and
	// still be retried.
	MaxRetryAfter time.Duration
}

// DefaultPolicy is the plan's policy: three attempts, a backoff of one
// second and then two, and a wait of thirty seconds at the most.
func DefaultPolicy() Policy {
	return Policy{
		Attempts:      3,
		Backoff:       []time.Duration{time.Second, 2 * time.Second},
		MaxRetryAfter: poo.MaxRetryAfter,
	}
}

// With returns the policy with the consumer's overrides applied.
//
// A zero attempts or an empty backoff keeps the default, because a
// consumer that names neither means the plan's own.
func (p Policy) With(attempts int, backoff []time.Duration) Policy {
	if attempts > 0 {
		p.Attempts = attempts
	}
	if len(backoff) > 0 {
		p.Backoff = backoff
	}
	return p
}

// Delay returns the wait before the next attempt, and whether there is
// one.
//
// The failed attempt is the attempt number, counting from one. A
// failure that is not worth another attempt has no wait: the request
// settles with it.
func (p Policy) Delay(attempt int, err error) (time.Duration, bool) {
	if attempt >= p.Attempts {
		return 0, false
	}
	var pe *poo.ProviderError
	if !errors.As(err, &pe) {
		return 0, false
	}
	// The failure's own rule decides. A rate limit is worth another attempt
	// only when the endpoint asked for a wait of thirty seconds or less. A
	// server failure and a network failure always are worth one, and a
	// rejection never is.
	if !pe.Retryable() {
		return 0, false
	}
	wait := p.backoffWait(attempt)
	if pe.RetryAfter > 0 {
		wait = pe.RetryAfter
	}
	if wait > p.MaxRetryAfter {
		wait = p.MaxRetryAfter
	}
	return wait, true
}

// backoffWait returns the wait before the attempt after the failed one.
func (p Policy) backoffWait(attempt int) time.Duration {
	idx := attempt - 1
	if idx < 0 {
		idx = 0
	}
	if len(p.Backoff) == 0 {
		return 0
	}
	if idx >= len(p.Backoff) {
		idx = len(p.Backoff) - 1
	}
	return p.Backoff[idx]
}

// WaitFor waits for the given time, or until the context ends.
//
// A cancelled wait ends the request: the caller sees the context's
// error, and the run settles as cancelled.
func WaitFor(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Reader is the stream a request reads.
//
// A transport's own reader is one: it hands out the API's events, its
// close, and its usage. The retry wrapper composes it so a retried
// request and a watched one report the same things. The seams are the
// core's, embedded rather than restated: a member the core's interfaces
// grow is a member here too.
type Reader interface {
	poo.StreamReader
	poo.UsageReporter
}

// ReportAttempt tells the request's reporter about an attempt that
// failed.
//
// The retried flag says whether another attempt followed: the last
// attempt of a request is reported too, and nothing follows it.
func ReportAttempt(report func(poo.Attempt), attempt int, err error, delay time.Duration, retried bool) {
	if report == nil {
		return
	}
	report(poo.Attempt{Number: attempt, Err: err, Delay: delay, Retried: retried})
}

// Opener opens one attempt of a request.
type Opener func(ctx context.Context) (Reader, error)

// OpenWithRetry opens the request and returns the reader of the attempt
// that answered, and the number of that attempt.
//
// A failure that is worth another attempt is retried within the budget,
// and the wait between the attempts is cancellable. The opener resolves
// everything an attempt needs — a credential included — so a failure
// that is worth another attempt is asked again.
func OpenWithRetry(ctx context.Context, policy Policy, open Opener, report func(poo.Attempt)) (Reader, int, error) {
	attempt := 0
	for {
		attempt++
		reader, err := open(ctx)
		if err == nil {
			return reader, attempt, nil
		}
		wait, ok := policy.Delay(attempt, err)
		if !ok {
			// The last attempt of the request is reported too: the
			// record holds every attempt the request made.
			ReportAttempt(report, attempt, err, 0, false)
			return nil, attempt, err
		}
		// The attempt is reported once the wait is over, so a wait a
		// cancellation ends leaves an attempt that says no retry
		// followed it.
		if werr := WaitFor(ctx, wait); werr != nil {
			ReportAttempt(report, attempt, err, 0, false)
			return nil, attempt, werr
		}
		ReportAttempt(report, attempt, err, wait, true)
	}
}

// Retrying reads a response whose attempt may be retried.
//
// A failure before the response carried a delta is retried within the
// budget. A failure after a delta is returned as it is: the model has
// already seen part of the answer, so the request cannot be repeated.
type Retrying struct {
	// policy is the retry policy.
	policy Policy
	// ctx is the request's context.
	ctx context.Context
	// open opens an attempt the reader lost.
	open Opener
	// report is the request's own reporter: it hears every attempt that
	// failed.
	report func(poo.Attempt)
	// attempt is the number of the attempt in flight.
	attempt int
	// current is the reader of the attempt in flight.
	current Reader
	// seen reports that the attempt in flight has carried something the
	// model has seen: a delta or a tool call. Such an attempt is not
	// repeated, because the core assembles the calls by their index and
	// a repeated call would join the discarded one.
	seen bool
	// usage is the total the attempts spent, so a request that is
	// retried reports every attempt's cost and not only the answer's.
	usage *poo.UsageReport
	// counted is the attempt's usage report that is already in usage.
	counted *poo.UsageReport
}

// NewRetrying returns a wrapper around the attempt that answered.
func NewRetrying(ctx context.Context, policy Policy, open Opener, report func(poo.Attempt), current Reader, attempt int) *Retrying {
	return &Retrying{
		policy:  policy,
		ctx:     ctx,
		open:    open,
		report:  report,
		attempt: attempt,
		current: current,
	}
}

// Next returns the next event of the response.
//
// A failure that arrives before the response carried a delta is retried,
// so the caller sees the events of the attempt that answered.
func (s *Retrying) Next() (poo.StreamItem, error) {
	for {
		if s.current == nil {
			reader, err := s.open(s.ctx)
			if err != nil {
				wait, ok := s.policy.Delay(s.attempt, err)
				if !ok {
					ReportAttempt(s.report, s.attempt, err, 0, false)
					return poo.StreamItem{}, err
				}
				if werr := WaitFor(s.ctx, wait); werr != nil {
					ReportAttempt(s.report, s.attempt, err, 0, false)
					return poo.StreamItem{}, werr
				}
				ReportAttempt(s.report, s.attempt, err, wait, true)
				s.attempt++
				continue
			}
			s.current = reader
			s.counted = nil
		}

		ev, err := s.current.Next()
		if err == nil {
			if ev.Kind == "usage" && ev.Usage != nil {
				ev.Usage = s.addUsage(ev.Usage)
			}
			if ev.Kind == "delta" || ev.Kind == "tool_call" {
				s.seen = true
			}
			ev.Attempt = s.attempt
			return ev, nil
		}
		if errors.Is(err, io.EOF) {
			return poo.StreamItem{}, io.EOF
		}
		// The request's own context ended: the run is stopping, and the
		// failure is the context's.
		if s.ctx.Err() != nil {
			return poo.StreamItem{}, err
		}
		if s.seen {
			// The attempt carried part of the answer, so the request
			// cannot be repeated. A failure that would be retryable
			// before the first delta is an interrupted stream now. The
			// record and the settle reason therefore name the real
			// condition.
			err = InterruptedStreamError(err)
		}
		if !s.seen {
			if wait, ok := s.policy.Delay(s.attempt, err); ok {
				s.current.Close()
				s.current = nil
				if werr := WaitFor(s.ctx, wait); werr != nil {
					ReportAttempt(s.report, s.attempt, err, 0, false)
					return poo.StreamItem{}, werr
				}
				ReportAttempt(s.report, s.attempt, err, wait, true)
				s.attempt++
				continue
			}
		}
		// The attempt that failed and was not tried again: it is the
		// last one, and the record holds it too.
		ReportAttempt(s.report, s.attempt, err, 0, false)
		return poo.StreamItem{}, err
	}
}

// InterruptedStreamError reclassifies a retryable failure that arrived
// after the stream carried part of the answer.
//
// The original kind stays in the error's code, where the plan keeps it.
func InterruptedStreamError(err error) error {
	var pe *poo.ProviderError
	if !errors.As(err, &pe) || !pe.Retryable() {
		return err
	}
	out := *pe
	out.Code = string(pe.Kind)
	out.Kind = poo.ProviderKindInterruptedStream
	return &out
}

// addUsage adds one attempt's usage to the request's total and returns
// a snapshot of the total.
func (s *Retrying) addUsage(report *poo.UsageReport) *poo.UsageReport {
	if s.usage == nil {
		s.usage = &poo.UsageReport{}
	}
	s.usage.InputTokens += report.InputTokens
	s.usage.OutputTokens += report.OutputTokens
	if report.CacheReadTokens != nil {
		total := *report.CacheReadTokens
		if s.usage.CacheReadTokens != nil {
			total += *s.usage.CacheReadTokens
		}
		s.usage.CacheReadTokens = &total
	}
	if s.usage.TTFTMS == 0 {
		s.usage.TTFTMS = report.TTFTMS
	}
	s.counted = report
	return CloneUsage(s.usage)
}

// CloneUsage returns a copy of a usage report.
func CloneUsage(report *poo.UsageReport) *poo.UsageReport {
	if report == nil {
		return nil
	}
	out := *report
	if report.CacheReadTokens != nil {
		cached := *report.CacheReadTokens
		out.CacheReadTokens = &cached
	}
	return &out
}

// Close releases the response.
func (s *Retrying) Close() error {
	if s.current == nil {
		return nil
	}
	return s.current.Close()
}

// ReportUsage returns the usage of the attempt in flight.
//
// A request that failed before its usage arrived still produced tokens,
// so the wrapper reports what it knows.
func (s *Retrying) ReportUsage() *poo.UsageReport {
	if s.current != nil {
		if usage := s.current.ReportUsage(); usage != nil && usage != s.counted {
			return s.addUsage(usage)
		}
	}
	return CloneUsage(s.usage)
}

// ReportProviderBlocks forwards the blocks the attempt produced.
//
// The core asks the reader the transport handed it, so a wrapper that
// did not forward the report would lose the response's reasoning.
func (s *Retrying) ReportProviderBlocks() []poo.ProviderBlock {
	if s.current == nil {
		return nil
	}
	reporter, ok := s.current.(poo.ProviderBlockReporter)
	if !ok {
		return nil
	}
	return reporter.ReportProviderBlocks()
}
