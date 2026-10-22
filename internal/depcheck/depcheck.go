// Package depcheck enforces the poo dependency rules on non-test imports.
//
//   - the core package may import the standard library, plus the prompt and
//     decide packages
//   - the prompt, decide and wire/protocol packages may import the standard
//     library only
//
// The import graph comes from "go list -e -deps -json", and the toolchain
// decides what counts as the standard library. The rules cover each checked
// package's whole dependency closure. A violation is reported against the
// package that makes the offending direct import.
//
// The check reads the host build configuration: files excluded by GOOS,
// GOARCH, CGO_ENABLED or build tags (taken from GOFLAGS) are not seen, and
// neither are files marked "//go:build ignore".
//
// A checked package whose directory does not exist, or holds no non-test Go
// files, is skipped.
package depcheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Checker enforces the dependency rules of one module.
type Checker struct {
	// ModuleRoot is the directory that holds the module's go.mod.
	ModuleRoot string
	// ModulePath is the module's import path, e.g. github.com/sdougbrown/poo.
	ModulePath string
}

// Result is what a check found.
type Result struct {
	// Findings holds one sorted, deduplicated message per problem:
	//
	//	P imports X
	//	cannot resolve X (imported by P)
	//	cannot check P: reason
	Findings []string
	// Skipped names the checked packages that were not checked, and why.
	Skipped []string
}

// rule limits what one package, and everything it pulls in, may import.
type rule struct {
	path    string
	allowed []string // module-internal imports permitted besides the standard library
}

func (c Checker) rules() []rule {
	m := c.ModulePath
	return []rule{
		{path: m, allowed: []string{m + "/prompt", m + "/decide"}},
		{path: m + "/prompt"},
		{path: m + "/decide"},
		{path: m + "/wire/protocol"},
	}
}

// Check applies the rules and reports what it finds.
func (c Checker) Check() (Result, error) {
	var res Result
	root, err := filepath.Abs(c.ModuleRoot)
	if err != nil {
		return res, fmt.Errorf("resolving the module root %s: %w", c.ModuleRoot, err)
	}
	c.ModuleRoot = root

	rules := c.rules()
	checked := make([]string, len(rules))
	for i, r := range rules {
		checked[i] = r.path
	}

	for _, r := range rules {
		reason, err := c.skipReason(r.path)
		if err != nil {
			return res, err
		}
		if reason != "" {
			res.Skipped = append(res.Skipped, fmt.Sprintf("%s (%s)", r.path, reason))
			continue
		}
		pkgs, err := c.list(r.path)
		if err != nil {
			return res, err
		}
		res.Findings = append(res.Findings, c.check(r, checked, pkgs)...)
	}

	slices.Sort(res.Findings)
	res.Findings = slices.Compact(res.Findings)
	slices.Sort(res.Skipped)
	return res, nil
}

// check applies r to every package in the closure of r.path. Another checked
// package in the closure is left to its own rule.
func (c Checker) check(r rule, checked []string, pkgs []listPackage) []string {
	byPath := make(map[string]*listPackage, len(pkgs))
	for i := range pkgs {
		byPath[pkgs[i].ImportPath] = &pkgs[i]
	}

	var findings []string
	for _, p := range pkgs {
		if p.Standard {
			continue
		}
		if p.Error != nil {
			// A package whose every file is build-ignored is not seen when
			// it is the rule's own package: the doc promises that a
			// //go:build ignore file does not reach the check. The imports
			// are empty, so there is nothing to scan.
			if p.ImportPath == r.path && len(p.Imports) == 0 && strings.Contains(c.localize(p.Error.Err), "build constraints exclude all Go files") {
				continue
			}
			// A package that does not exist has no directory; it is
			// reported against its importers below.
			if p.Dir == "" {
				continue
			}
			// Its imports are still scanned: an import cycle, which any
			// forbidden import of the core package closes, sets this
			// error and leaves the offending import in Imports.
			findings = append(findings, fmt.Sprintf("cannot check %s: %s", p.ImportPath, c.localize(p.Error.Err)))
		}
		if p.ImportPath != r.path && slices.Contains(checked, p.ImportPath) {
			continue
		}
		for _, imp := range p.Imports {
			target := byPath[imp]
			switch {
			case target == nil || target.Standard:
			case target.Error != nil:
				if target.Dir == "" {
					findings = append(findings, fmt.Sprintf("cannot resolve %s (imported by %s)", imp, p.ImportPath))
				}
			case !slices.Contains(r.allowed, imp):
				finding := fmt.Sprintf("%s imports %s", p.ImportPath, imp)
				if p.ImportPath != r.path {
					finding += fmt.Sprintf(" (in the closure of %s)", r.path)
				}
				findings = append(findings, finding)
			}
		}
	}
	return findings
}

// skipReason returns why a package is not checked, or "" when it is.
func (c Checker) skipReason(pkgPath string) (string, error) {
	rel := strings.TrimPrefix(strings.TrimPrefix(pkgPath, c.ModulePath), "/")
	dir := filepath.Join(c.ModuleRoot, filepath.FromSlash(rel))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "does not exist yet", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") &&
			!strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_") {
			return "", nil
		}
	}
	return "no non-test Go files", nil
}

// list runs "go list -e -deps -json" for a package and decodes its stream.
// The child never reaches the network or rewrites go.mod.
func (c Checker) list(pkgPath string) ([]listPackage, error) {
	cmd := exec.Command("go", "list", "-e", "-deps", "-json", pkgPath)
	cmd.Dir = c.ModuleRoot
	cmd.Env = append(os.Environ(), "GOFLAGS="+goflags(os.Getenv("GOFLAGS")), "GOPROXY=off", "GOWORK=off")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list -e -deps -json %s: %w: %s", pkgPath, err, strings.Join(strings.Fields(stderr.String()), " "))
	}

	dec := json.NewDecoder(&stdout)
	var pkgs []listPackage
	for {
		var p listPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			return pkgs, nil
		} else if err != nil {
			return nil, fmt.Errorf("decoding the output of go list for %s: %w", pkgPath, err)
		}
		pkgs = append(pkgs, p)
	}
}

// localize makes the module's absolute paths in a message relative and puts
// the message on one line.
func (c Checker) localize(message string) string {
	message = strings.ReplaceAll(message, c.ModuleRoot+string(filepath.Separator), "")
	return strings.Join(strings.Fields(message), " ")
}

// listPackage is the part of "go list -json" the check reads.
type listPackage struct {
	Dir        string // empty for a package that could not be resolved
	ImportPath string
	Standard   bool
	Imports    []string
	Error      *struct{ Err string }
}

// goflags returns an inherited GOFLAGS value with its -mod flag replaced by
// -mod=readonly, so the check never rewrites go.mod and keeps flags such as
// -tags.
func goflags(inherited string) string {
	var flags []string
	for _, f := range strings.Fields(inherited) {
		if !strings.HasPrefix(f, "-mod=") {
			flags = append(flags, f)
		}
	}
	return strings.Join(append(flags, "-mod=readonly"), " ")
}
