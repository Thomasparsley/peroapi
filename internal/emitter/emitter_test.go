package emitter_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Thomasparsley/peroapi/internal/converter"
	"github.com/Thomasparsley/peroapi/internal/emitter"
)

func sampleItems() []*converter.PathItem {
	return []*converter.PathItem{
		{
			OperationName: "GetUser",
			Path:          "/api/graphql/GetUser",
			Method:        "GET",
			Parameters: []*converter.Parameter{
				{Name: "id", In: "query", Required: true, Schema: &converter.Schema{Type: "string"}},
			},
			Response: &converter.Response{
				Description: "Successful response",
				Content: map[string]*converter.MediaTypeObject{
					"application/json": {Schema: &converter.Schema{Type: "object"}},
				},
			},
		},
		{
			OperationName: "CreateUser",
			Path:          "/api/graphql/CreateUser",
			Method:        "POST",
			RequestBody: &converter.RequestBody{
				Required: true,
				Content: map[string]*converter.MediaTypeObject{
					"application/json": {Schema: &converter.Schema{Type: "object"}},
				},
			},
			Response: &converter.Response{
				Description: "Successful response",
				Content: map[string]*converter.MediaTypeObject{
					"application/json": {Schema: &converter.Schema{Type: "object"}},
				},
			},
		},
	}
}

// TestBuild_OpenAPIVersion verifies that the emitted document always declares OpenAPI version 3.0.3.
func TestBuild_OpenAPIVersion(t *testing.T) {
	// Arrange + Act
	doc := emitter.Build(sampleItems(), nil, emitter.Options{Title: "Test", Version: "1.0.0"})

	// Assert
	assert.Equal(t, "3.0.3", doc.OpenAPI)
}

// TestBuild_Info verifies that the title and version from Options are propagated to the
// document's info section unchanged.
func TestBuild_Info(t *testing.T) {
	// Arrange + Act
	doc := emitter.Build(sampleItems(), nil, emitter.Options{Title: "My API", Version: "2.1.0"})

	// Assert
	assert.Equal(t, "My API", doc.Info.Title)
	assert.Equal(t, "2.1.0", doc.Info.Version)
}

// TestBuild_Paths verifies that each PathItem produces the correct path entry and that GET
// and POST operations are placed under their respective method keys (not swapped or duplicated).
func TestBuild_Paths(t *testing.T) {
	// Arrange + Act
	doc := emitter.Build(sampleItems(), nil, emitter.Options{Title: "T", Version: "1"})

	// Assert
	assert.Contains(t, doc.Paths, "/api/graphql/GetUser")
	assert.Contains(t, doc.Paths, "/api/graphql/CreateUser")
	assert.NotNil(t, doc.Paths["/api/graphql/GetUser"].Get)
	assert.Nil(t, doc.Paths["/api/graphql/GetUser"].Post)
	assert.NotNil(t, doc.Paths["/api/graphql/CreateUser"].Post)
	assert.Nil(t, doc.Paths["/api/graphql/CreateUser"].Get)
}

// TestBuild_GetOperationHasParameters verifies that query parameters from a GET PathItem
// are carried through to the emitted OperationObject without loss or reordering.
func TestBuild_GetOperationHasParameters(t *testing.T) {
	// Arrange + Act
	doc := emitter.Build(sampleItems(), nil, emitter.Options{Title: "T", Version: "1"})

	// Assert
	op := doc.Paths["/api/graphql/GetUser"].Get
	require.Len(t, op.Parameters, 1)
	assert.Equal(t, "id", op.Parameters[0].Name)
}

// TestBuild_PostOperationHasRequestBody verifies that the request body from a POST PathItem
// is carried through to the emitted OperationObject with required: true.
func TestBuild_PostOperationHasRequestBody(t *testing.T) {
	// Arrange + Act
	doc := emitter.Build(sampleItems(), nil, emitter.Options{Title: "T", Version: "1"})

	// Assert
	op := doc.Paths["/api/graphql/CreateUser"].Post
	require.NotNil(t, op.RequestBody)
	assert.True(t, op.RequestBody.Required)
}

// TestWrite_Stdout verifies that Write produces a YAML file whose content includes the
// "openapi: 3.0.3" header when given a file path.
func TestWrite_Stdout(t *testing.T) {
	// Arrange
	doc := emitter.Build(sampleItems(), nil, emitter.Options{Title: "T", Version: "1"})
	dir := t.TempDir()
	out := filepath.Join(dir, "out.yaml")

	// Act
	err := emitter.Write(doc, out)

	// Assert
	require.NoError(t, err)

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(data), "openapi: 3.0.3")
}

// TestWrite_ValidYAML verifies that the written output is valid YAML that round-trips through
// a YAML parser and yields the correct openapi key.
func TestWrite_ValidYAML(t *testing.T) {
	// Arrange
	doc := emitter.Build(sampleItems(), nil, emitter.Options{Title: "T", Version: "1"})
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

// TestWrite_ContainsOperationID verifies that all operation IDs (set from OperationName)
// are present in the serialized YAML output.
func TestWrite_ContainsOperationID(t *testing.T) {
	// Arrange
	doc := emitter.Build(sampleItems(), nil, emitter.Options{Title: "T", Version: "1"})
	dir := t.TempDir()
	out := filepath.Join(dir, "out.yaml")

	// Act
	require.NoError(t, emitter.Write(doc, out))

	// Assert
	data, _ := os.ReadFile(out)
	content := string(data)
	assert.True(t, strings.Contains(content, "GetUser"))
	assert.True(t, strings.Contains(content, "CreateUser"))
}
