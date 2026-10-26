// Package live holds the live evaluation of the permission asker.
//
// The evaluation asks a real decision model about real calls: it is
// skipped unless the environment names an endpoint, and it is kept
// apart from the stub tests, which cover the shapes and the routing.
//
// The cases are the ones a permission ask has to get right: a call the
// user asked for is allowed, and a call that reaches outside the work
// is refused or deferred. A decision model that is unavailable is a
// failure of the evaluation, never a refusal.
package live
