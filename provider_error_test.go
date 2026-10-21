package poo_test

import (
	"testing"

	"github.com/sdougbrown/poo"
)

func TestAProviderErrorShowsItsStatusWhenItHasOne(t *testing.T) {
	with := &poo.ProviderError{Kind: poo.ProviderKindServer, Message: "overloaded", Status: 503}
	if got, want := with.Error(), "overloaded (status 503)"; got != want {
		t.Errorf("the error reads %q, want %q", got, want)
	}
	without := &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: "connection reset"}
	if got := without.Error(); got != "connection reset" {
		t.Errorf("the error reads %q, want the message alone", got)
	}
}
