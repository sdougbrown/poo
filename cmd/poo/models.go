package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/catalog"
)

// modelsOptions are the flags of `poo models`.
type modelsOptions struct {
	sharedFlags
	// json prints the models as JSON.
	json bool
	// explain names the model to explain.
	explain string
}

// bind reads the models command's flags out of a parsed invocation.
func (o *modelsOptions) bind(inv clicontract.Invocation) {
	o.sharedFlags.bind(inv)
	o.json = inv.Bools["json"]
	o.explain = inv.Flags["explain"]
}

// handleModels executes `poo models`.
//
// The command lists the effective catalog: the models the settings
// offer, the models the binary knows about, and what the endpoints
// report.
func handleModels(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o modelsOptions
	o.bind(inv)

	settings, err := o.settings("")
	if err != nil {
		fmt.Fprintf(stderr, "poo models: %v\n", err)
		return exit{usageExitCode}
	}
	// The command asks, so the discovery layer comes from a reprobe and
	// the cached layer is not built: the ask applies it once, and does
	// not warn twice when the cache is unreadable. The wire's
	// models/list reprobes too when its caller asks for a refresh.
	options := settings.CatalogOptions()
	options.Cached = false
	options.UserAgent = userAgent()
	cat := catalog.New(options)
	// A probe that fails falls back to the cache and then to the
	// curated catalog, with a warning: the list is still the list.
	if err := cat.Reprobe(context.Background()); err != nil {
		fmt.Fprintf(stderr, "poo models: %v\n", err)
		return exit{operationalExitCode}
	}

	if o.explain != "" {
		return explainModel(cat, o.explain, stdout, stderr)
	}
	models := cat.List()
	if o.json {
		encoded, err := json.MarshalIndent(models, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "poo models: %v\n", err)
			return exit{operationalExitCode}
		}
		fmt.Fprintln(stdout, string(encoded))
		return exit{}
	}
	printModels(models, stdout)
	return exit{}
}

// printModels prints the catalog as a table.
func printModels(models []poo.ModelInfo, stdout io.Writer) {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tMODEL\tENDPOINT\tAPI\tCONTEXT\tOPS")
	for _, model := range models {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
			model.Name,
			model.ModelID,
			model.Endpoint,
			model.API,
			model.ContextLimit,
			joinOps(model.Ops))
	}
	_ = w.Flush()
}

// explainModel prints the fields of a model and the layer that set
// each of them.
func explainModel(cat *catalog.Catalog, name string, stdout, stderr io.Writer) exit {
	info, fields, err := cat.Explain(name)
	if err != nil {
		fmt.Fprintf(stderr, "poo models: %v\n", err)
		return exit{usageExitCode}
	}
	fmt.Fprintf(stdout, "%s (%s)\n", info.Name, info.ModelID)
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FIELD\tVALUE\tLAYER")
	for _, field := range fields {
		fmt.Fprintf(w, "%s\t%s\t%s\n", field.Field, field.Value, field.Layer)
	}
	_ = w.Flush()
	return exit{}
}

// joinOps renders the operations a model serves.
func joinOps(ops []poo.Op) string {
	out := ""
	for i, op := range ops {
		if i > 0 {
			out += ","
		}
		out += string(op)
	}
	return out
}
