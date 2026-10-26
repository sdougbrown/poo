package catalog

import (
	"fmt"
	"os"

	"github.com/sdougbrown/poo"
)

// Role is a job a model is assigned to.
type Role string

const (
	// RoleChat is the chat model of a session.
	RoleChat Role = "chat"
	// RoleDecider is the model that answers the decision questions.
	RoleDecider Role = "decider"
)

// CredentialSource names where an endpoint's key comes from.
//
// A key is read from the environment or it is written in the settings.
// There is no other source in v1.
type CredentialSource string

const (
	// CredentialNone means the endpoint needs no key.
	CredentialNone CredentialSource = "none"
	// CredentialEnv reads the key from an environment variable.
	CredentialEnv CredentialSource = "env"
	// CredentialStatic holds the key in the settings.
	CredentialStatic CredentialSource = "static"
)

// Credential is how an endpoint's key is read.
type Credential struct {
	// Source is where the key comes from.
	Source CredentialSource
	// Env names the environment variable that holds the key, for an env
	// source.
	Env string
	// Value is the key itself, for a static source.
	Value string
}

// EnvNames returns the names of the environment variables the credential
// reads.
//
// A static credential reads none, and a key that is read from the
// environment is scrubbed from the tools' child environment under this
// name.
func (c Credential) EnvNames() []string {
	if c.Source != CredentialEnv || c.Env == "" {
		return nil
	}
	return []string{c.Env}
}

// Key returns the endpoint's key.
//
// An env source reads the variable, and a static source returns the
// value the settings carry. The value is never logged.
func (c Credential) Key() string {
	switch c.Source {
	case CredentialEnv:
		return os.Getenv(c.Env)
	case CredentialStatic:
		return c.Value
	}
	return ""
}

// Endpoint is a service that serves models.
type Endpoint struct {
	// Name is the endpoint's name: the settings' key, or a hosted
	// provider's id.
	Name string
	// Endpoint is the endpoint's base URL.
	Endpoint string
	// Credential is how the endpoint's key is read.
	Credential Credential
}

// OpError is a model that does not serve the operation its role needs.
//
// The error names the role and the operation, so the reader knows what
// the model cannot do.
type OpError struct {
	// Role is the role the model was assigned to.
	Role Role
	// Model is the model's name.
	Model string
	// Op is the operation the role needs.
	Op poo.Op
}

func (e *OpError) Error() string {
	return fmt.Sprintf("catalog: the model %q does not serve the %q operation the %q role needs",
		e.Model, e.Op, e.Role)
}

// UnresolvedError is a model the catalog cannot resolve.
type UnresolvedError struct {
	// Name is the model's name.
	Name string
	// Role is the role the model was assigned to. It is empty when the
	// model is not assigned to one.
	Role Role
}

func (e *UnresolvedError) Error() string {
	if e.Role != "" {
		return fmt.Sprintf("catalog: the model %q assigned to the %q role is not in the catalog",
			e.Name, e.Role)
	}
	return fmt.Sprintf("catalog: the model %q is not in the catalog", e.Name)
}

// opsForRole returns the operations a role needs.
//
// The chat role needs a chat completion. The decider role is served by a
// model that answers systemone questions, or by a model that makes the
// decisions through chat.
func opsForRole(role Role) []poo.Op {
	switch role {
	case RoleChat:
		return []poo.Op{poo.OpChat}
	case RoleDecider:
		return []poo.Op{poo.OpSystemOne, poo.OpDecisions}
	}
	return nil
}
