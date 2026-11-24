// Package scopeschema holds the rules a feed's scope schema answers to,
// beside the two validators that check it: the manifest loader, which
// refuses a schema it cannot read, and the session's declaration check,
// which refuses one it cannot honour.
package scopeschema

// ScopeNamePattern is the expression a scope field's name must match. The
// name is what an `scope:NAME` binding and a subscription both spell, and
// the prompt prints it, so it stays a word.
const ScopeNamePattern = `^[a-z][a-z0-9_]*$`

// ScopeStringType is the only scope field type a scope schema may
// declare.
//
// A scope value reaches the feed as an environment variable's value, so it
// is a string: a field of another type would have no way to arrive.
const ScopeStringType = "string"
