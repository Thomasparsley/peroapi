package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/validator/rules"
)

// persistedOperationsMap maps operation names to their GraphQL source strings,
// matching the gql-persisted.json file format.
type persistedOperationsMap map[string]string

// Operation holds a parsed GraphQL operation and its source text.
type Operation struct {
	Name   string
	Source string
	Doc    *ast.QueryDocument
}

// LoadOperations reads gql-persisted.json and validates each operation against the schema.
// Validation errors are accumulated and returned together.
func LoadOperations(path string, schema *ast.Schema) ([]*Operation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading operations file %q: %w", path, err)
	}

	var raw persistedOperationsMap
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing operations JSON: %w", err)
	}

	defaultRules := rules.NewDefaultRules()

	var ops []*Operation
	var validationErrors []string

	for name, src := range raw {
		doc, gqlErrs := gqlparser.LoadQueryWithRules(schema, src, defaultRules)
		if gqlErrs != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("operation %q: %s", name, gqlErrs.Error()))
			continue
		}
		ops = append(ops, &Operation{
			Name:   name,
			Source: src,
			Doc:    doc,
		})
	}

	if len(validationErrors) > 0 {
		return nil, fmt.Errorf("operation validation errors:\n  %s", strings.Join(validationErrors, "\n  "))
	}

	return ops, nil
}
