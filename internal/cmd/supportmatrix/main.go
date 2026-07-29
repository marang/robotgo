// Command supportmatrix checks or updates the generated Runtime Compatibility
// Matrix table from its machine-readable contract.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/marang/robotgo/internal/supportmatrix"
)

const (
	defaultContract = "docs/compatibility/runtime-v1.json"
	defaultMarkdown = "docs/compatibility/runtime-v1.md"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "supportmatrix: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("supportmatrix", flag.ContinueOnError)
	contractPath := flags.String("contract", defaultContract, "support contract JSON")
	markdownPath := flags.String("markdown", defaultMarkdown, "rendered compatibility document")
	write := flags.Bool("write", false, "update the generated Markdown table")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}

	contract, err := supportmatrix.Load(*contractPath)
	if err != nil {
		return err
	}
	document, err := os.ReadFile(*markdownPath)
	if err != nil {
		return fmt.Errorf("read compatibility document: %w", err)
	}
	rendered := supportmatrix.RenderMarkdown(contract)
	expected, err := supportmatrix.ReplaceMarkdown(string(document), rendered)
	if err != nil {
		return err
	}
	if expected == string(document) {
		return nil
	}
	if !*write {
		return fmt.Errorf(
			"%s is stale; run go run ./internal/cmd/supportmatrix -write",
			*markdownPath,
		)
	}
	if err := os.WriteFile(*markdownPath, []byte(expected), 0o644); err != nil {
		return fmt.Errorf("write compatibility document: %w", err)
	}
	return nil
}
