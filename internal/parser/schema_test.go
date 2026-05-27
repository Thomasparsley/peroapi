package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Thomasparsley/peroapi/internal/parser"
)

// TestLoadSchema_Valid verifies that a well-formed GraphQL schema file is parsed
// successfully and the returned Schema has a Query root type.
func TestLoadSchema_Valid(t *testing.T) {
	// Arrange
	schema := `type Query { hello: String }`
	f := writeTempFile(t, "schema.graphql", schema)

	// Act
	s, err := parser.LoadSchema(f)

	// Assert
	require.NoError(t, err)
	assert.NotNil(t, s)
	assert.NotNil(t, s.Query)
}

// TestLoadSchema_InvalidGraphQL verifies that a syntactically invalid GraphQL file
// returns an error whose message contains "parsing schema".
func TestLoadSchema_InvalidGraphQL(t *testing.T) {
	// Arrange
	f := writeTempFile(t, "schema.graphql", "this is not graphql %%%")

	// Act
	_, err := parser.LoadSchema(f)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing schema")
}

// TestLoadSchema_MissingFile verifies that a path to a non-existent file produces
// an error whose message contains "reading schema file".
func TestLoadSchema_MissingFile(t *testing.T) {
	// Arrange — no file is created; the path is intentionally invalid.

	// Act
	_, err := parser.LoadSchema("/no/such/file.graphql")

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading schema file")
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}
