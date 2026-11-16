// Package support holds the parts of an HTTP provider transport that do
// not depend on the wire API.
//
// A transport's own files hold what its API says: the request shape, the
// response stream's events, and the conversion between the model's view
// and the API's items. Everything around that is the same for every
// provider that speaks HTTP with an SSE answer: the failure table, the
// retry policy, the JSON body a fork rewrites, the SSE line scanner, the
// time to the first token, and the tool order that keeps a prompt prefix
// stable.
//
// It is internal because none of it is a contract a consumer can build
// on: a transport composes it, and the API-specific half stays in the
// transport's own package.
package support
