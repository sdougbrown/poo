package main

import (
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/sdougbrown/poo/pootest"
)

// TestMain runs the tests under a settings file of the tests' own, so a
// run a test starts cannot discover the operator's settings.
func TestMain(m *testing.M) {
	// A sign-in probes the endpoints its key unlocks, and no test reaches a
	// provider. The seam refuses every request; a test that wants a fixture
	// replaces it with probesThrough.
	probeClient = &http.Client{Transport: refusingTransport{}}
	os.Exit(pootest.Main(m))
}

// refusingTransport refuses every request a probe makes.
type refusingTransport struct{}

func (refusingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("no endpoint is reached from a test: %s", req.URL)
}
