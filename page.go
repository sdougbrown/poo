package poo

// The byte-budget rule the paged responses share.
//
// A page is one line the wire's output queue takes, and the queue
// refuses a line over its budget even on an empty queue. The rule
// keeps a page under the budget while keeping a client paging: the
// page holds the items nearest its paging position that fit, and
// always holds at least one, so paging never stalls.

// PageByteBudget is the byte budget for one paged response, over the
// page's own JSON — a history page, a page of events in the wire. The
// budget keeps a full page far under the wire's queue line budget.
const PageByteBudget = 8 << 20

// PageWithinByteBudget returns the longest suffix of items whose sizes
// fit the byte budget, and reports whether it dropped any. A history
// page pages backwards, so it keeps the newest entries.
func PageWithinByteBudget[T any](items []T, size func(T) int, budget int) ([]T, bool) {
	return pageWithin(items, size, budget, true)
}

// PageHeadWithinByteBudget returns the longest prefix of items whose
// sizes fit the byte budget, and reports whether it dropped any. A
// page of events pages forward, so it keeps the oldest events.
func PageHeadWithinByteBudget[T any](items []T, size func(T) int, budget int) ([]T, bool) {
	return pageWithin(items, size, budget, false)
}

// pageWithin carries the rule both pagers share: walking the items
// from the paging end, it keeps the run that fits the byte budget.
//
// The item at the paging end always stays, so a client paging always
// moves; even a page of one oversized item still comes back. The one
// byte per item covers the separator the JSON array puts between the
// items. A budget below that item's own size keeps it alone.
func pageWithin[T any](items []T, size func(T) int, budget int, backwards bool) ([]T, bool) {
	n := len(items)
	at := func(k int) int {
		if backwards {
			return n - 1 - k
		}
		return k
	}
	bytes := 0
	kept := 0
	for k := 0; k < n; k++ {
		i := at(k)
		if k > 0 && bytes+size(items[i])+1 > budget {
			break
		}
		bytes += size(items[i]) + 1
		kept = k + 1
	}
	if backwards {
		return items[n-kept:], kept < n
	}
	return items[:kept], kept < n
}
