package wire_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// TestAnExplicitNullFieldIsInvalidParams covers the contract's rule:
// an explicit null is invalid, at every depth the shapes define.
func TestAnExplicitNullFieldIsInvalidParams(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	for _, raw := range []string{
		`{"session_id":null}`,
		`{"working_dir":".","models":null}`,
		`{"working_dir":".","models":{"chat":null}}`,
		`{"working_dir":".","prompt_overrides":{"sections":{"house":null}}}`,
	} {
		method := protocol.MethodSessionGet
		if raw != `{"session_id":null}` {
			method = protocol.MethodSessionCreate
		}
		_, rerr := h.callRaw(method, raw)
		if rerr == nil {
			t.Errorf("%s accepted %s", method, raw)
			continue
		}
		if rerr.Data == nil || rerr.Data.Code != protocol.DataInvalidParams {
			t.Errorf("%s refused %s with %q, want %q", method, raw, fix1DataCode(rerr), protocol.DataInvalidParams)
		}
	}
}

// TestARequestWithAnotherJSONRPCVersionIsInvalid covers the line's
// jsonrpc field: the contract's requests say "2.0".
func TestARequestWithAnotherJSONRPCVersionIsInvalid(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	h.mu.Lock()
	id := "wrong-version"
	ch := make(chan *protocol.Response, 1)
	h.pending[id] = ch
	h.mu.Unlock()
	h.write(map[string]any{"jsonrpc": "1.0", "id": id, "method": protocol.MethodConfigGet})
	resp := <-ch
	if resp.Error == nil {
		t.Fatal("the request with jsonrpc 1.0 was served")
	}
	if resp.Error.Data == nil || resp.Error.Data.Code != protocol.DataInvalidRequest {
		t.Errorf("the data code = %q, want %q", fix1DataCode(resp.Error), protocol.DataInvalidRequest)
	}
}

// TestModelsListReturnsTheCatalog covers models/list: the effective
// catalog is what the factory offers.
func TestModelsListReturnsTheCatalog(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	raw, rerr := h.call(protocol.MethodModelsList, protocol.ModelsParams{})
	if rerr != nil {
		t.Fatalf("models/list was refused: %v", rerr)
	}
	var result protocol.ModelsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decoding the models: %v", err)
	}
	if len(result.Models) != 1 || result.Models[0].Name != "test-model" {
		t.Errorf("the models are %+v, want the factory's own", result.Models)
	}
}

// TestConfigureDuringARunIsBusy covers session/configure: a session
// that is running refuses the change.
func TestConfigureDuringARunIsBusy(t *testing.T) {
	transport := newBlockingTransport()
	factory := newTestFactory(t, func() poo.ChatTransport { return transport })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	if _, rerr := h.call(protocol.MethodAgentPrompt, protocol.PromptParams{SessionID: sessionID, Text: "hi"}); rerr != nil {
		t.Fatalf("the prompt was refused: %v", rerr)
	}
	transport.waitStarted(t)

	_, rerr := h.call(protocol.MethodSessionConfigure, protocol.ConfigureParams{SessionID: sessionID})
	if rerr == nil {
		t.Fatal("the configure during a run was answered")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataBusy {
		t.Errorf("the data code = %q, want %q", fix1DataCode(rerr), protocol.DataBusy)
	}
	transport.release()
}

// TestCloseReportsAFlushError covers session/close: the flush failure
// reaches the client instead of a successful empty result.
func TestCloseReportsAFlushError(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	factory.flushErr = errors.New("the flush failed")
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	sessionID := createSession(t, h)
	_, rerr := h.call(protocol.MethodSessionClose, protocol.CloseParams{SessionID: sessionID})
	if rerr == nil {
		t.Fatal("the close whose flush failed was answered with a result")
	}
	if rerr.Data == nil || rerr.Data.Code != protocol.DataStoreError {
		t.Errorf("the data code = %q, want %q", fix1DataCode(rerr), protocol.DataStoreError)
	}
}

// fix1DataCode returns the data code an error carries.
func fix1DataCode(rerr *protocol.Error) string {
	if rerr == nil || rerr.Data == nil {
		return ""
	}
	return rerr.Data.Code
}

// TestLoadWithAnInvalidSessionIDIsInvalidParams covers the id rule at
// the wire: a client cannot make the server read outside the session
// directory.
func TestLoadWithAnInvalidSessionIDIsInvalidParams(t *testing.T) {
	factory := newTestFactory(t, func() poo.ChatTransport { return pootest.NewTransport() })
	h := newHarness(t, factory, wire.Bounds{})
	h.initialize()

	for _, id := range []string{"../x", "a/b", "..", `a\b`} {
		_, rerr := h.call(protocol.MethodSessionLoad, protocol.LoadParams{SessionID: id})
		if rerr == nil {
			t.Errorf("the load of %q was answered", id)
			continue
		}
		if rerr.Data == nil || rerr.Data.Code != protocol.DataInvalidParams {
			t.Errorf("the load of %q failed with %q, want %q", id, fix1DataCode(rerr), protocol.DataInvalidParams)
		}
	}
}
