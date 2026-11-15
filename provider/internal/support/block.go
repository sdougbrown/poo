package support

import (
	"fmt"

	"github.com/sdougbrown/poo"
)

// CarryError reports whether a request for one wire API can carry a
// provider block.
//
// A block is opaque data the API produced, so only that API can take it
// back, and only a wire that has a field for it can carry it at all. The
// caller states both: the API its request speaks, and whether its wire
// shape has a place for opaque data.
//
// A required block the request cannot carry makes the request
// impossible, which the run settles as an incompatible session. An
// optional one is simply left out, and the caller decides that by
// looking at CarryError's answer before it drops the block.
//
// A block with no payload is one the endpoint cannot match to what it
// produced, so a required block without one is refused too. That is the
// shape a window item takes when the core installs a provider window: the
// payload has to be there for the item to be worth sending.
func CarryError(requested poo.API, accepts bool, p *poo.ProviderBlock) error {
	if p == nil || !p.Required {
		return nil
	}
	if accepts && p.API == requested && len(p.Data) > 0 {
		return nil
	}
	reason := fmt.Sprintf("a required %q block (%s)", p.Type, p.API)
	if accepts && p.API == requested {
		reason = fmt.Sprintf("a required %q block (%s) with no payload", p.Type, p.API)
	}
	return &poo.ProviderError{
		Kind: poo.ProviderKindSessionIncompatible,
		Message: fmt.Sprintf("the history holds %s, which a %s request cannot carry",
			reason, requested),
	}
}
