// Command convert-to-swagger converts an OpenAPI 3.0 YAML file to Swagger 2.0
// (OpenAPI 2.0) YAML.
//
// Usage: convert-to-swagger <input-openapi3.yaml> <output-swagger2.yaml>
//
// It replaces the previous Node-based scripts/convert-to-swagger.mjs (which used
// the unmaintained api-spec-converter package) with the maintained
// github.com/getkin/kin-openapi converter, matching the Go toolchain already
// used to generate the API client from the OpenAPI 3.0 spec.
package main

import (
	"fmt"
	"os"

	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
	"sigs.k8s.io/yaml"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "Usage: convert-to-swagger <input-openapi3.yaml> <output-swagger2.yaml>")
		os.Exit(1)
	}

	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "convert-to-swagger: %v\n", err)
		os.Exit(1)
	}
}

func run(input, output string) error {
	loader := openapi3.NewLoader()

	doc3, err := loader.LoadFromFile(input)
	if err != nil {
		return fmt.Errorf("loading OpenAPI 3.0 spec %q: %w", input, err)
	}

	doc2, err := openapi2conv.FromV3(doc3)
	if err != nil {
		return fmt.Errorf("converting to Swagger 2.0: %w", err)
	}

	out, err := yaml.Marshal(doc2)
	if err != nil {
		return fmt.Errorf("marshaling Swagger 2.0 YAML: %w", err)
	}

	if err := os.WriteFile(output, out, 0o644); err != nil {
		return fmt.Errorf("writing %q: %w", output, err)
	}

	return nil
}
