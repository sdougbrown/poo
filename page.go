package poo

// The byte-budget rule the paged responses share.
//
// A page is one line the wire's output queue takes, and the queue
// refuses a line over its budget even when the queue is empty. The
// rule keeps a page under the budget while keeping a client paging:
// the page holds the items nearest its paging position that fit, and
// always holds at least one, so paging never stalls.

// PageByteBudget is the byte budget for one paged response, over the
// page's own JSON — a history page, a page of events in the wire. The
// budget keeps a full page far under the wire's queue line budget.
const PageByteBudget = 8 << 20

// PageWithinByteBudget returns the longest suffix of items whose sizes
// fit the byte budget, and reports whether it dropped any. A history
// page pages backwards, so it keeps the newest entries.
//
// The last item always stays: a page of one oversized item still comes
// back, so a client paging backwards always moves. The one byte per
// item covers the separator the JSON array puts between the items. A
// budget below the last item's own size keeps that item alone.
func PageWithinByteBudget[T any](items []T, size func(T) int, budget int) ([]T, bool) {
	bytes := 0
	for i := len(items) - 1; i >= 0; i-- {
		if bytes > 0 && bytes+size(items[i])+1 > budget {
			return items[i+1:], true
		}
		bytes += size(items[i]) + 1
	}
	return items, false
}

// PageHeadWithinByteBudget returns the longest prefix of items whose
// sizes fit the byte budget, and reports whether it dropped any. A
// page of events pages forward, so it keeps the oldest events.
//
// The first item always stays, so a client paging forward always
// moves. The one byte per item covers the separator the JSON array
// puts between the items. A budget below the first item's own size
// keeps that item alone.
func PageHeadWithinByteBudget[T any](items []T, size func(T) int, budget int) ([]T, bool) {
	bytes := 0
	for i, item := range items {
		if i > 0 && bytes+size(item)+1 > budget {
			return items[:i], true
		}
		bytes += size(item) + 1
	}
	return items, false
}
