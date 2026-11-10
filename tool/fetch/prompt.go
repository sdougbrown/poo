package fetch

import (
	_ "embed"

	"github.com/sdougbrown/poo/prompt"
)

//go:embed prompt.md
var sectionText string

// section is the guidance the tool brings to the system prompt.
//
// The tool says how to read the result and where its refusals live,
// so the model can carry on when a fetch comes back capped or
// refused.
var section = prompt.MustCompile("fetch", sectionText, nil)
