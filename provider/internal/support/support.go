// Package support holds the half of an HTTP provider transport that moves
// a request and reads an answer.
//
// A transport's own files hold what its API says: the request shape, the
// response stream's events, and the conversion between the model's view
// and the API's items. The request-shape rules the APIs share are the
// transport package's. What is here is the same for every provider that
// speaks HTTP with an SSE answer: the failure table, the retry policy,
// the SSE line scanner, the opening and closing of a stream, the time to
// the first token, and the configuration errors a transport reports.
//
// It is internal because none of it is a contract a consumer can build
// on: a transport composes it, and the API-specific half stays in the
// transport's own package.
package support
