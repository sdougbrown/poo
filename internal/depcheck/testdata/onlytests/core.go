// Package onlytests is a fixture: the decide package of the module holds no
// non-test Go files, so it is skipped rather than checked.
package onlytests

// Use returns a constant.
func Use() string { return "onlytests" }
