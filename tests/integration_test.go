package tests_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Thomasparsley/peroapi/internal/converter"
	"github.com/Thomasparsley/peroapi/internal/emitter"
	"github.com/Thomasparsley/peroapi/internal/parser"
)

const (
	fixtureSchema = "fixtures/schema.graphql"
	fixtureOps    = "fixtures/operations.json"
)

// runPipeline executes the full parse → convert → build pipeline against the shared fixture
// files and returns the resulting OpenAPI Document for assertion.
func runPipeline(t *testing.T, convention string) *emitter.Document {
	t.Helper()
	schema, err := parser.LoadSchema(fixtureSchema)
	require.NoError(t, err)

	ops, err := parser.LoadOperations(fixtureOps, schema)
	require.NoError(t, err)

	conv := converter.New(schema)
	items, err := conv.Convert(ops, convention, "/api/graphql")
	require.NoError(t, err)

	return emitter.Build(items, conv.Components(), emitter.Options{Title: "Test API", Version: "1.0.0"})
}

// TestIntegration_CorrectNumberOfPaths verifies that the full pipeline produces exactly one
// path per operation defined in fixtures/operations.json (8 operations expected).
func TestIntegration_CorrectNumberOfPaths(t *testing.T) {
	// Arrange + Act
	doc := runPipeline(t, "rest")

	// Assert — fixtures/operations.json has 8 operations
	assert.Len(t, doc.Paths, 8)
}

// TestIntegration_RestConvention_Methods verifies that under the "rest" convention, GraphQL
// queries map to GET and mutations map to POST, with no cross-contamination between methods.
func TestIntegration_RestConvention_Methods(t *testing.T) {
	// Arrange + Act
	doc := runPipeline(t, "rest")

	// Assert — Queries → GET
	for _, name := range []string{"GetUser", "ListUsers", "GetMe", "GetOrders"} {
		path := "/api/graphql/" + name
		require.Contains(t, doc.Paths, path, "path %s missing", path)
		assert.NotNil(t, doc.Paths[path].Get, "expected GET for %s", name)
		assert.Nil(t, doc.Paths[path].Post, "expected no POST for %s", name)
	}

	// Assert — Mutations → POST
	for _, name := range []string{"CreateUser", "UpdateUser", "DeleteUser", "PlaceOrder"} {
		path := "/api/graphql/" + name
		require.Contains(t, doc.Paths, path, "path %s missing", path)
		assert.NotNil(t, doc.Paths[path].Post, "expected POST for %s", name)
		assert.Nil(t, doc.Paths[path].Get, "expected no GET for %s", name)
	}
}

// TestIntegration_PostConvention_AllPost verifies that under the "post" convention all
// operations use POST, regardless of whether they are queries or mutations.
func TestIntegration_PostConvention_AllPost(t *testing.T) {
	// Arrange + Act
	doc := runPipeline(t, "post")

	// Assert
	for path, po := range doc.Paths {
		assert.NotNil(t, po.Post, "expected POST for %s", path)
		assert.Nil(t, po.Get, "expected no GET for %s", path)
	}
}

// TestIntegration_GetConvention_AllGet verifies that under the "get" convention all operations
// use GET, regardless of whether they are queries or mutations.
func TestIntegration_GetConvention_AllGet(t *testing.T) {
	// Arrange + Act
	doc := runPipeline(t, "get")

	// Assert
	for path, po := range doc.Paths {
		assert.NotNil(t, po.Get, "expected GET for %s", path)
		assert.Nil(t, po.Post, "expected no POST for %s", path)
	}
}

// TestIntegration_GetUser_Params verifies that the GetUser query's ID variable is converted to a
// required query parameter with a string schema (GraphQL ID scalar → OpenAPI string).
func TestIntegration_GetUser_Params(t *testing.T) {
	// Arrange + Act
	doc := runPipeline(t, "rest")
	op := doc.Paths["/api/graphql/GetUser"].Get
	require.NotNil(t, op)

	// Assert
	paramsByName := make(map[string]*converter.Parameter)
	for _, p := range op.Parameters {
		paramsByName[p.Name] = p
	}

	require.Contains(t, paramsByName, "id")
	assert.Equal(t, "string", paramsByName["id"].Schema.Type) // ID → string
	assert.True(t, paramsByName["id"].Required)
}

// TestIntegration_CreateUser_RequestBody verifies that the CreateUser mutation produces a
// required JSON request body containing an "input" property (the CreateUserInput object).
func TestIntegration_CreateUser_RequestBody(t *testing.T) {
	// Arrange + Act
	doc := runPipeline(t, "rest")
	op := doc.Paths["/api/graphql/CreateUser"].Post

	// Assert
	require.NotNil(t, op)
	require.NotNil(t, op.RequestBody)
	assert.True(t, op.RequestBody.Required)

	body := op.RequestBody.Content["application/json"].Schema
	require.NotNil(t, body)
	assert.Contains(t, body.Properties, "variables")

	variablesObj := body.Properties["variables"]
	require.NotNil(t, variablesObj)
	assert.Contains(t, variablesObj.Properties, "input")
}

// TestIntegration_ResponseEnvelope verifies that every operation response wraps the payload
// in the standard GraphQL envelope: { data: ..., errors: [...] }.
func TestIntegration_ResponseEnvelope(t *testing.T) {
	// Arrange + Act
	doc := runPipeline(t, "rest")
	op := doc.Paths["/api/graphql/GetUser"].Get
	require.NotNil(t, op)

	// Assert
	resp200 := op.Responses["200"]
	require.NotNil(t, resp200)

	schema := resp200.Content["application/json"].Schema
	require.NotNil(t, schema)
	assert.Contains(t, schema.Properties, "data")
	assert.Contains(t, schema.Properties, "errors")
}

// TestIntegration_ValidYAMLOutput verifies that the full pipeline produces valid YAML with
// the correct openapi version key when written to disk.
func TestIntegration_ValidYAMLOutput(t *testing.T) {
	// Arrange
	doc := runPipeline(t, "rest")
	dir := t.TempDir()
	out := filepath.Join(dir, "out.yaml")

	// Act
	require.NoError(t, emitter.Write(doc, out))

	// Assert
	data, err := os.ReadFile(out)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, yaml.Unmarshal(data, &parsed))
	assert.Equal(t, "3.0.3", parsed["openapi"])
}
