// Package parser handles loading and parsing GraphQL schema and operation files.
package parser

import (
	"fmt"
	"os"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// LoadSchema reads and parses a GraphQL schema file.
func LoadSchema(path string) (*ast.Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading schema file %q: %w", path, err)
	}

	source := &ast.Source{Name: path, Input: string(data)}
	schema, gqlErr := gqlparser.LoadSchema(source)
	if gqlErr != nil {
		return nil, fmt.Errorf("parsing schema: %w", gqlErr)
	}
	return schema, nil
}
