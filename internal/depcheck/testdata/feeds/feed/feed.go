// Package feed is a fixture: the feed package, which may import the exec
// package and no other package of the module but the ones its rule lists.
package feed

import (
	"example.com/poo/depcheck/feeds"
	"example.com/poo/depcheck/feeds/exec"
	"example.com/poo/depcheck/feeds/helper"
)

// Use returns what the core, the executor and the helper hold.
func Use() string { return feeds.Use + exec.Use + helper.Text }
