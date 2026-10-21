// Package broken is a fixture: a package the toolchain cannot load. Its
// directory holds two package clauses, so the go tool cannot decide which
// package the directory holds, and the package fails to load.
//
// The files are gofmt-clean, so the repository's format check is unaffected.
package broken

// Value is the value the core package reaches for.
const Value = "broken"
