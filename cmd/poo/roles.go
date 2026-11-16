package main

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo/provider/catalog"
)

// rolesOptions are the flags of `poo roles`.
type rolesOptions struct {
	sharedFlags
}

// bind reads the roles command's flags out of a parsed invocation.
func (o *rolesOptions) bind(inv clicontract.Invocation) {
	o.sharedFlags.bind(inv)
}

// handleRoles executes `poo roles`.
//
// The command reports what each role the settings assign resolves to: the model,
// the provider that serves it, and where that provider's key comes from. It is
// the check an operator runs without starting a session: it builds the catalog
// the way a session does, prints no key, trades no credential, and exits
// non-zero when an assigned role does not resolve.
func handleRoles(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o rolesOptions
	o.bind(inv)
	settings, err := o.settings("")
	if err != nil {
		fmt.Fprintf(stderr, "poo roles: %v\n", err)
		return exit{usageExitCode}
	}
	cat := catalog.New(settings.CatalogOptions())

	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ROLE\tMODEL\tPROVIDER\tCREDENTIAL")
	failed := false
	// The roles the catalog holds are the ones a session answers to, and they are what
	// this reports on: a role assigned in the settings but unknown to the catalog would
	// be enforced by neither.
	assigned := cat.Roles()
	names := make([]string, 0, len(assigned)+2)
	for role := range assigned {
		names = append(names, string(role))
	}
	for _, role := range []catalog.Role{catalog.RoleChat, catalog.RoleDecider} {
		if _, ok := assigned[role]; !ok {
			names = append(names, string(role))
		}
	}
	sort.Strings(names)
	for _, name := range names {
		role := catalog.Role(name)
		chosen, ok := assigned[role]
		if !ok {
			// A role the settings do not assign is a role a session does
			// without: the run's flags may name the chat model instead, and the
			// decider is optional.
			fmt.Fprintf(w, "%s\t-\t-\t-\n", role)
			continue
		}
		resolved, err := cat.Role(role)
		if err != nil {
			fmt.Fprintf(w, "%s\t%s\t-\t-\n", role, chosen)
			fmt.Fprintf(stderr, "poo roles: %v\n", err)
			failed = true
			continue
		}
		endpoint := cat.Endpoints()[resolved.Provider]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", role, resolved.Name, resolved.Provider, credential(endpoint.Credential))
	}
	_ = w.Flush()
	if failed {
		return exit{operationalExitCode}
	}
	return exit{}
}

// credential renders where an endpoint's key comes from, and never the key.
func credential(c catalog.Credential) string {
	switch c.Source {
	case catalog.CredentialEnv:
		return "env:" + c.Env
	case catalog.CredentialStatic:
		return "static"
	}
	return "none"
}
