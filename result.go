package poo

// Result is the outcome of a run.
type Result struct {
	// Reason is the settle reason.
	Reason Reason
	// Detail explains the outcome.
	Detail SettleDetail
	// Turns is the number of provider requests the run made.
	Turns int
}
