//go:build !conformanceext

package wire

import (
	"github.com/sdougbrown/poo/wire/protocol"
)

// The extension build adds to the protocol: a capability, a result
// field and an event type. The hooks are here, so the build without
// the extension is the plain one.

// initializeResult returns the handshake's result.
func (s *server) initializeResult(version int) any {
	return protocol.InitializeResult{
		ProtocolVersion: version,
		Build:           s.opts.Build,
		Profiles:        s.factory.Profiles(),
		Models:          s.factory.Roles(),
		Capabilities:    s.factory.Capabilities(),
	}
}

// getResult returns the session's state.
func (s *server) getResult(result protocol.GetResult) any {
	return result
}

// sessionCreated is called when a session is created.
func (s *server) sessionCreated(entry *session) {}

// sessionClosed is called when a session is closed.
func (s *server) sessionClosed(entry *session) {}
