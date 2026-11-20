package catalog

import (
	"fmt"

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
// A key is read from the environment or it is written in the settings. An
// endpoint that reads an environment variable also takes the credential the
// operator stored for the provider declaring that variable, and config's
// EndpointKey is the one place that order is applied.
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

// Endpoint is a service that serves models.
type Endpoint struct {
	// Name is the endpoint's name: the settings' key, or a hosted
	// provider's id.
	Name string
	// Endpoint is the endpoint's base URL.
	Endpoint string
	// API is the wire API the endpoint speaks, which a model no layer
	// named inherits.
	API poo.API
	// SessionHeader is the name of a header that carries the session's id, for
	// a gateway that routes one conversation's requests together. Empty means
	// the endpoint asks for none.
	SessionHeader string
	// SkipProbe says the endpoint is not probed for its models, because its
	// models are declared rather than discovered.
	SkipProbe bool
	// InstructionsPerQuestion says the endpoint answers what each question
	// asks and rejects a request-level instructions field, so a decision
	// call folds its framing into the questions. False sends the
	// instructions as the request's own field.
	InstructionsPerQuestion bool
	// Credential is how the endpoint's key is read.
	Credential Credential
}

// OpError is a model that does not serve what its role needs.
//
// The error names the role and the operations, so the reader knows what the model
// cannot do.
type OpError struct {
	// Role is the role the model was assigned to.
	Role Role
	// Model is the model's name.
	Model string
	// Ops are the operations the role accepts: a model serves the role with any
	// one of them.
	Ops []poo.Op
}

func (e *OpError) Error() string {
	if len(e.Ops) == 1 {
		return fmt.Sprintf("catalog: the model %q does not serve the %q operation the %q role needs",
			e.Model, e.Ops[0], e.Role)
	}
	names := make([]string, 0, len(e.Ops))
	for _, op := range e.Ops {
		names = append(names, fmt.Sprintf("%q", op))
	}
	return fmt.Sprintf("catalog: the model %q does not serve any of the %s operations the %q role needs",
		e.Model, quoteList(names), e.Role)
}

// servesRole reports whether a model serves what a role needs.
//
// A role with no operations needs nothing. A role that names several is served by a
// model that serves any one of them, which is what the decider does: it answers
// through the systemone API or through chat, whichever the model has.
func servesRole(info poo.ModelInfo, role Role) bool {
	ops := opsForRole(role)
	if len(ops) == 0 {
		return true
	}
	for _, op := range ops {
		if info.HasOp(op) {
			return true
		}
	}
	return false
}

// UnresolvedError is a model the catalog cannot resolve.
//
// The message says what is known, not only what is missing: the role, the name
// as it was written, and the models that would have served the role. A typo, an
// unqualified name and a models file that was not read are told apart by that
// list.
type UnresolvedError struct {
	// Name is the model's name, as the settings wrote it.
	Name string
	// Role is the role the model was assigned to. It is empty when the
	// model is not assigned to one.
	Role Role
	// Known are the models in the catalog that serve the role's operations,
	// lowest name first. It is empty for a model no role names, and for a role
	// no model in the catalog can serve.
	Known []string
}

func (e *UnresolvedError) Error() string {
	if e.Role == "" {
		return fmt.Sprintf("catalog: the model %q is not in the catalog", e.Name)
	}
	known := ""
	switch len(e.Known) {
	case 0:
		known = fmt.Sprintf(", and no model in the catalog serves the %q role", e.Role)
	default:
		known = fmt.Sprintf(", and the models that serve the %q role are %s", e.Role, quoteList(e.Known))
	}
	return fmt.Sprintf("catalog: the model %q assigned to the %q role is not in the catalog%s",
		e.Name, e.Role, known)
}

// quoteList renders names for an error, quoted and separated.
func quoteList(names []string) string {
	out := ""
	for i, name := range names {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%q", name)
	}
	return out
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
