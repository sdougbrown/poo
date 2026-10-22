package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

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

// runModels executes `poo models`.
//
// The command lists the effective catalog: the models the settings
// offer, the models the binary knows about, and what the endpoints
// report.
func runModels(args []string) int {
	fs := flag.NewFlagSet("poo models", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	var o modelsOptions
	o.register(fs)
	fs.BoolVar(&o.json, "json", false, "Print the models as JSON")
	fs.StringVar(&o.explain, "explain", "", "Explain one model: the fields and the layer that set each")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	settings, err := o.settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo models: %v\n", err)
		return 2
	}
	cat := catalog.New(catalog.Options{Settings: settings.CatalogSettings()})
	// A probe that fails falls back to the cache and then to the
	// curated catalog, with a warning: the list is still the list.
	if err := cat.Refresh(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "poo models: %v\n", err)
		return 1
	}

	if o.explain != "" {
		return explainModel(cat, o.explain)
	}
	models := cat.List()
	if o.json {
		encoded, err := json.MarshalIndent(models, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "poo models: %v\n", err)
			return 1
		}
		fmt.Println(string(encoded))
		return 0
	}
	printModels(models)
	return 0
}

// printModels prints the catalog as a table.
func printModels(models []poo.ModelInfo) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
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
func explainModel(cat *catalog.Catalog, name string) int {
	info, fields, err := cat.Explain(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo models: %v\n", err)
		return 2
	}
	fmt.Printf("%s (%s)\n", info.Name, info.ModelID)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FIELD\tVALUE\tLAYER")
	for _, field := range fields {
		fmt.Fprintf(w, "%s\t%s\t%s\n", field.Field, field.Value, field.Layer)
	}
	_ = w.Flush()
	return 0
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
