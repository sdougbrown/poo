// The genmodelsdev command generates the hosted part of the curated
// catalog from the models.dev data.
//
// Usage:
//
//	go run ./internal/genmodelsdev/cmd/genmodelsdev -input testdata/models.dev.json -output modelsdev_gen.go
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sdougbrown/poo/provider/catalog/internal/genmodelsdev"
)

func main() {
	input := flag.String("input", "", "the models.dev JSON file to read")
	output := flag.String("output", "", "the Go file to write")
	flag.Parse()

	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "genmodelsdev: -input and -output are required")
		os.Exit(2)
	}
	if err := genmodelsdev.Generate(*input, *output); err != nil {
		fmt.Fprintf(os.Stderr, "genmodelsdev: %v\n", err)
		os.Exit(1)
	}
}
