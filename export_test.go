package poo

import "time"

// SetRecordWriteBudget changes how long one record write may take, and
// returns a function that restores the previous budget.
func SetRecordWriteBudget(d time.Duration) func() {
	prev := recordWriteBudget
	recordWriteBudget = d
	return func() { recordWriteBudget = prev }
}
