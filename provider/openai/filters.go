package openai

import (
	"log/slog"
	"strings"

	"github.com/sdougbrown/poo"
)

// The text filters.
//
// A filter sits between the endpoint and the consumer: it takes the text
// a model sends and returns the events that text produced. The filters
// run in the order the quirks added them, and each filter's text goes to
// the next one.

// textFilter is a filter over the text a model sends.
//
// The filter takes the text as it arrives and returns the events it
// produced. A filter that needs more text than has arrived holds it
// back until it can decide.
type textFilter interface {
	// feed takes a text delta and returns the events it produced.
	feed(text string) []poo.StreamItem
	// flush returns the events the filter holds at the end of a stream.
	flush() []poo.StreamItem
}

// applyFilters runs the text through the filters, in order.
//
// Each filter's text goes to the next filter, and the events are queued
// as they are made, so a filter's own events keep their place in the
// answer.
func applyFilters(filters []textFilter, text string) []poo.StreamItem {
	if len(filters) == 0 {
		return []poo.StreamItem{textEvent(text)}
	}
	return feedFilter(filters, text, 0)
}

// feedFilter feeds the text to the filter at the index, and the
// filter's text to the one after it.
func feedFilter(filters []textFilter, text string, idx int) []poo.StreamItem {
	events := filters[idx].feed(text)
	if idx+1 >= len(filters) {
		return events
	}
	var out []poo.StreamItem
	for _, ev := range events {
		if ev.Kind == "delta" && ev.Delta != nil && ev.Delta.Kind == "text" {
			out = append(out, feedFilter(filters, ev.Delta.Text, idx+1)...)
			continue
		}
		out = append(out, ev)
	}
	return out
}

// flushFilters flushes the filters, in order.
func flushFilters(filters []textFilter) []poo.StreamItem {
	var out []poo.StreamItem
	for _, f := range filters {
		out = append(out, f.flush()...)
	}
	return out
}

// The thinking tags a model writes its reasoning in.
const (
	// thinkOpen opens a block of thinking.
	thinkOpen = "<think>"
	// thinkClose closes a block of thinking.
	thinkClose = "</think>"
)

// thinkFilter strips the thinking a model writes in its own tags.
//
// The model writes <think>...</think> around its reasoning. The
// thinking becomes a thinking block, and the answer is what is left.
type thinkFilter struct {
	// buf holds the text seen so far.
	buf string
	// thinking reports that the filter is inside a thinking block.
	thinking bool
	// inside holds the thinking of the open block.
	inside strings.Builder
}

// newThinkFilter returns a filter that strips the thinking tags.
func newThinkFilter() *thinkFilter {
	return &thinkFilter{}
}

// feed takes a text delta and returns the events it produced.
//
// The thinking is held until its block closes, because the closing tag
// may be split over two deltas.
func (f *thinkFilter) feed(text string) []poo.StreamItem {
	f.buf += text
	var out []poo.StreamItem
	for {
		if f.thinking {
			i := strings.Index(f.buf, thinkClose)
			if i < 0 {
				// The block has not closed yet. The thinking waits.
				break
			}
			f.inside.WriteString(f.buf[:i])
			f.buf = f.buf[i+len(thinkClose):]
			out = append(out, thinkingEvent(f.inside.String()))
			f.inside.Reset()
			f.thinking = false
			continue
		}
		i := strings.Index(f.buf, thinkOpen)
		if i < 0 {
			// The text may be the start of a tag, so the tail is held
			// back until it is known.
			keep := len(thinkOpen) - 1
			if keep > len(f.buf) {
				keep = len(f.buf)
			}
			split := len(f.buf) - keep
			if split > 0 {
				out = append(out, textEvent(f.buf[:split]))
				f.buf = f.buf[split:]
			}
			break
		}
		if i > 0 {
			out = append(out, textEvent(f.buf[:i]))
			f.buf = f.buf[i:]
		}
		f.thinking = true
		f.buf = f.buf[len(thinkOpen):]
	}
	return out
}

// flush returns the events the filter holds at the end of a stream.
//
// A thinking block that never closed is still thinking, and the text
// after it is the answer.
func (f *thinkFilter) flush() []poo.StreamItem {
	var out []poo.StreamItem
	if f.thinking {
		f.inside.WriteString(f.buf)
		out = append(out, thinkingEvent(f.inside.String()))
		f.inside.Reset()
		f.thinking = false
		f.buf = ""
		return out
	}
	if f.buf != "" {
		out = append(out, textEvent(f.buf))
		f.buf = ""
	}
	return out
}

// degenerateRepeats is how many times a model has to send the same text
// in a row before the stream is noted.
const degenerateRepeats = 3

// degenerateFilter notes a stream that repeats itself.
//
// A model that answers with the same text over and over is not making
// progress. The filter notes it and lets the stream run: a guard that
// aborts loses the answer, which is the pony lesson.
type degenerateFilter struct {
	// logger receives the note.
	logger *slog.Logger
	// model names the model, for the note.
	model string
	// last holds the text of the last delta.
	last string
	// repeats counts the deltas that repeated the last one.
	repeats int
	// noted reports that the note was made.
	noted bool
}

// newDegenerateFilter returns a filter that notes a repeated stream.
func newDegenerateFilter(logger *slog.Logger, model string) *degenerateFilter {
	return &degenerateFilter{logger: logger, model: model}
}

// feed takes a text delta and returns the events it produced.
//
// The filter never holds the text back: the answer reaches the consumer
// as it arrives.
func (f *degenerateFilter) feed(text string) []poo.StreamItem {
	if text != "" && text == f.last {
		f.repeats++
	} else {
		f.last = text
		f.repeats = 0
	}
	if !f.noted && f.repeats >= degenerateRepeats {
		f.noted = true
		f.logger.Warn("the model repeated the same text",
			"model", f.model,
			"repeats", f.repeats,
		)
	}
	return []poo.StreamItem{textEvent(text)}
}

// flush returns the events the filter holds at the end of a stream.
func (f *degenerateFilter) flush() []poo.StreamItem {
	return nil
}
