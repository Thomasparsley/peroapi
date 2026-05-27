package parser_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Thomasparsley/peroapi/internal/parser"
)

const testSchema = `
type Query {
  user(id: ID!): User
  users: [User!]!
}
type Mutation {
  createUser(name: String!): User!
}
type User {
  id: ID!
  name: String!
  email: String
}
`

func loadTestSchema(t *testing.T) interface{ /* *ast.Schema */ } {
	t.Helper()
	f := writeTempFile(t, "schema.graphql", testSchema)
	s, err := parser.LoadSchema(f)
	require.NoError(t, err)
	return s
}

// TestLoadOperations_Valid verifies that three valid operations (two queries and one mutation)
// are all parsed, validated against the schema, and returned without error.
func TestLoadOperations_Valid(t *testing.T) {
	// Arrange
	f := writeTempFile(t, "schema.graphql", testSchema)
	schema, err := parser.LoadSchema(f)
	require.NoError(t, err)

	ops := map[string]string{
		"GetUser":  `query GetUser($id: ID!) { user(id: $id) { id name } }`,
		"ListAll":  `query ListAll { users { id name } }`,
		"MakeUser": `mutation MakeUser($name: String!) { createUser(name: $name) { id name } }`,
	}
	data, _ := json.Marshal(ops)
	opFile := writeTempFile(t, "ops.json", string(data))

	// Act
	result, err := parser.LoadOperations(opFile, schema)

	// Assert
	require.NoError(t, err)
	assert.Len(t, result, 3)

	names := make(map[string]bool)
	for _, o := range result {
		names[o.Name] = true
	}
	assert.True(t, names["GetUser"])
	assert.True(t, names["ListAll"])
	assert.True(t, names["MakeUser"])
}

// TestLoadOperations_InvalidJSON verifies that a malformed JSON operations file
// produces an error whose message contains "parsing operations JSON".
func TestLoadOperations_InvalidJSON(t *testing.T) {
	// Arrange
	f := writeTempFile(t, "schema.graphql", testSchema)
	schema, err := parser.LoadSchema(f)
	require.NoError(t, err)

	opFile := writeTempFile(t, "ops.json", `not json at all`)

	// Act
	_, err = parser.LoadOperations(opFile, schema)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing operations JSON")
}

// TestLoadOperations_MissingFile verifies that a path to a non-existent operations file
// produces an error whose message contains "reading operations file".
func TestLoadOperations_MissingFile(t *testing.T) {
	// Arrange
	f := writeTempFile(t, "schema.graphql", testSchema)
	schema, err := parser.LoadSchema(f)
	require.NoError(t, err)

	// Act
	_, err = parser.LoadOperations("/no/such/ops.json", schema)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading operations file")
}

// TestLoadOperations_InvalidOperation verifies that an operation referencing a field that
// does not exist in the schema produces an "operation validation errors" error.
// Validation errors are accumulated across all operations rather than failing on the first.
func TestLoadOperations_InvalidOperation(t *testing.T) {
	// Arrange
	f := writeTempFile(t, "schema.graphql", testSchema)
	schema, err := parser.LoadSchema(f)
	require.NoError(t, err)

	ops := map[string]string{
		"Bad": `query Bad { nonExistentField { id } }`,
	}
	data, _ := json.Marshal(ops)
	opFile := writeTempFile(t, "ops.json", string(data))

	// Act
	_, err = parser.LoadOperations(opFile, schema)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "operation validation errors")
}
