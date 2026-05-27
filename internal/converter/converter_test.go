package converter_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/Thomasparsley/peroapi/internal/converter"
	"github.com/Thomasparsley/peroapi/internal/parser"
)

const fullSchema = `
scalar DateTime

enum Status { ACTIVE INACTIVE }

input CreateInput {
  name: String!
  count: Int
  flag: Boolean
  score: Float
  ref: ID
  status: Status
}

type Item {
  id: ID!
  name: String!
  count: Int!
  flag: Boolean!
  score: Float!
  status: Status!
  createdAt: DateTime!
}

type Result {
  success: Boolean!
}

type Query {
  getItem(id: ID!): Item
  listItems: [Item!]!
  search(name: String!, limit: Int): [Item!]!
}

type Mutation {
  create(input: CreateInput!): Item!
  delete(id: ID!): Result!
}
`

func setup(t *testing.T, schema string) (*ast.Schema, []*parser.Operation) {
	t.Helper()
	dir := t.TempDir()

	sf := filepath.Join(dir, "schema.graphql")
	require.NoError(t, os.WriteFile(sf, []byte(schema), 0o644))
	s, err := parser.LoadSchema(sf)
	require.NoError(t, err)
	return s, nil
}

func loadOps(t *testing.T, schema *ast.Schema, ops map[string]string) []*parser.Operation {
	t.Helper()
	dir := t.TempDir()
	data, _ := json.Marshal(ops)
	of := filepath.Join(dir, "ops.json")
	require.NoError(t, os.WriteFile(of, data, 0o644))
	result, err := parser.LoadOperations(of, schema)
	require.NoError(t, err)
	return result
}

// TestConvert_QueryVariablesBecomesParams verifies that GraphQL query variables are converted
// to OpenAPI GET query parameters. Non-null variables must be required; nullable ones optional.
func TestConvert_QueryVariablesBecomesParams(t *testing.T) {
	// Arrange
	s, _ := setup(t, fullSchema)
	ops := loadOps(t, s, map[string]string{
		"Search": `query Search($name: String!, $limit: Int) { search(name: $name, limit: $limit) { id name } }`,
	})

	// Act
	conv := converter.New(s)
	items, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)
	require.Len(t, items, 1)

	item := items[0]
	assert.Equal(t, "GET", item.Method)
	assert.Nil(t, item.RequestBody)
	require.Len(t, item.Parameters, 2)

	paramsByName := make(map[string]*converter.Parameter)
	for _, p := range item.Parameters {
		paramsByName[p.Name] = p
	}

	assert.Equal(t, "string", paramsByName["name"].Schema.Type)
	assert.True(t, paramsByName["name"].Required)
	assert.Equal(t, "integer", paramsByName["limit"].Schema.Type)
	assert.False(t, paramsByName["limit"].Required)
}

// TestConvert_MutationVariablesBecomesBody verifies that GraphQL mutation variables are packed
// into a JSON request body object. Input object types must be extracted to $ref components.
func TestConvert_MutationVariablesBecomesBody(t *testing.T) {
	// Arrange
	s, _ := setup(t, fullSchema)
	ops := loadOps(t, s, map[string]string{
		"Create": `mutation Create($input: CreateInput!) { create(input: $input) { id name } }`,
	})

	// Act
	conv := converter.New(s)
	items, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)
	require.Len(t, items, 1)

	item := items[0]
	assert.Equal(t, "POST", item.Method)
	assert.NotNil(t, item.RequestBody)
	assert.Nil(t, item.Parameters)

	body := item.RequestBody.Content["application/json"].Schema
	require.NotNil(t, body)
	assert.Equal(t, "object", body.Type)
	assert.Contains(t, body.Properties, "variables")
	assert.Contains(t, body.Required, "variables")

	variablesObj := body.Properties["variables"]
	require.NotNil(t, variablesObj)
	assert.Equal(t, "object", variablesObj.Type)
	assert.Contains(t, variablesObj.Properties, "input")

	// input is a non-null InputObject → extracted to a $ref component
	inputRef := variablesObj.Properties["input"]
	assert.Equal(t, "#/components/schemas/CreateInput", inputRef.Ref)

	// The actual fields live in the component
	createInputComp := conv.Components()["CreateInput"]
	require.NotNil(t, createInputComp)
	assert.Equal(t, "object", createInputComp.Type)
	assert.Contains(t, createInputComp.Properties, "name")
}

// TestConvert_TypeMappings verifies the full mapping of GraphQL built-in scalars, enums,
// and custom scalars to their OpenAPI equivalents within a response selection set.
func TestConvert_TypeMappings(t *testing.T) {
	// Arrange
	s, _ := setup(t, fullSchema)
	ops := loadOps(t, s, map[string]string{
		"GetItem": `query GetItem($id: ID!) { getItem(id: $id) { id name count flag score status createdAt } }`,
	})

	// Act
	conv := converter.New(s)
	items, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)
	require.Len(t, items, 1)

	resp := items[0].Response.Content["application/json"].Schema
	data := resp.Properties["data"]
	require.NotNil(t, data)

	// getItem (nullable) → allOf + nullable wrapping a $ref
	getItemRef := data.Properties["getItem"]
	require.NotNil(t, getItemRef)
	assert.True(t, getItemRef.Nullable)
	require.Len(t, getItemRef.AllOf, 1)
	assert.Equal(t, "#/components/schemas/GetItemItem", getItemRef.AllOf[0].Ref)

	// The actual field schemas live in the component
	itemComp := conv.Components()["GetItemItem"]
	require.NotNil(t, itemComp)
	props := itemComp.Properties

	assert.Equal(t, "string", props["id"].Type)     // ID → string
	assert.Equal(t, "string", props["name"].Type)   // String → string
	assert.Equal(t, "integer", props["count"].Type) // Int → integer
	assert.Equal(t, "boolean", props["flag"].Type)  // Boolean → boolean
	assert.Equal(t, "number", props["score"].Type)  // Float → number
	assert.Equal(t, "#/components/schemas/Status", props["status"].Ref) // Enum → $ref
	require.Contains(t, conv.Components(), "Status")
	assert.NotEmpty(t, conv.Components()["Status"].Enum)
	assert.Equal(t, "string", props["createdAt"].Type)           // Scalar → string
	assert.Equal(t, "DateTime", props["createdAt"].XGraphQLScalar)
}

// TestConvert_ListType verifies that a non-null list return type ([Item!]!) produces
// an OpenAPI array schema in the response body.
func TestConvert_ListType(t *testing.T) {
	// Arrange
	s, _ := setup(t, fullSchema)
	ops := loadOps(t, s, map[string]string{
		"ListItems": `query ListItems { listItems { id name } }`,
	})

	// Act
	conv := converter.New(s)
	items, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)
	require.Len(t, items, 1)

	resp := items[0].Response.Content["application/json"].Schema
	data := resp.Properties["data"]
	require.NotNil(t, data)
	// data has shape {listItems: [Item!]!}
	listSchema := data.Properties["listItems"]
	require.NotNil(t, listSchema)
	assert.Equal(t, "array", listSchema.Type)
	assert.NotNil(t, listSchema.Items)
}

// TestConvert_PostConvention verifies that the "post" method convention forces all operations
// (including queries) to use the POST HTTP method.
func TestConvert_PostConvention(t *testing.T) {
	// Arrange
	s, _ := setup(t, fullSchema)
	ops := loadOps(t, s, map[string]string{
		"ListItems": `query ListItems { listItems { id name } }`,
	})

	// Act
	conv := converter.New(s)
	items, err := conv.Convert(ops, "post", "/api")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "POST", items[0].Method)
}

// TestConvert_GetConvention verifies that the "get" method convention forces all operations
// (including mutations) to use the GET HTTP method.
func TestConvert_GetConvention(t *testing.T) {
	// Arrange
	s, _ := setup(t, fullSchema)
	ops := loadOps(t, s, map[string]string{
		"Delete": `mutation Delete($id: ID!) { delete(id: $id) { success } }`,
	})

	// Act
	conv := converter.New(s)
	items, err := conv.Convert(ops, "get", "/api")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "GET", items[0].Method)
}

// TestConvert_PathGeneration verifies that the generated path is the base path joined with
// the operation name, without trailing slash duplication.
func TestConvert_PathGeneration(t *testing.T) {
	// Arrange
	s, _ := setup(t, fullSchema)
	ops := loadOps(t, s, map[string]string{
		"ListItems": `query ListItems { listItems { id name } }`,
	})

	// Act
	conv := converter.New(s)
	items, err := conv.Convert(ops, "rest", "/api/graphql")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "/api/graphql/ListItems", items[0].Path)
}

const scalarsSchema = `
scalar UnsignedShort
scalar Byte
scalar LocalDate

type Item {
  port: UnsignedShort!
  flags: Byte!
  birthday: LocalDate!
}

type Query {
  getItem: Item
}
`

// TestConvert_NewScalars verifies that custom domain scalars (UnsignedShort, Byte, LocalDate)
// are mapped to constrained integer or date-format string schemas rather than plain string.
func TestConvert_NewScalars(t *testing.T) {
	// Arrange
	s, _ := setup(t, scalarsSchema)
	ops := loadOps(t, s, map[string]string{
		"GetItem": `query GetItem { getItem { port flags birthday } }`,
	})

	// Act
	conv := converter.New(s)
	items, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)
	require.Len(t, items, 1)

	// getItem (nullable) is extracted to a component; access fields from there
	itemComp := conv.Components()["GetItemItem"]
	require.NotNil(t, itemComp)
	props := itemComp.Properties

	// UnsignedShort → integer/int32 [0, 65535]
	assert.Equal(t, "integer", props["port"].Type)
	assert.Equal(t, "int32", props["port"].Format)
	require.NotNil(t, props["port"].Minimum)
	require.NotNil(t, props["port"].Maximum)
	assert.Equal(t, int64(0), *props["port"].Minimum)
	assert.Equal(t, int64(65535), *props["port"].Maximum)

	// Byte → integer/int32 [0, 255]
	assert.Equal(t, "integer", props["flags"].Type)
	assert.Equal(t, "int32", props["flags"].Format)
	require.NotNil(t, props["flags"].Minimum)
	require.NotNil(t, props["flags"].Maximum)
	assert.Equal(t, int64(0), *props["flags"].Minimum)
	assert.Equal(t, int64(255), *props["flags"].Maximum)

	// LocalDate → string/date
	assert.Equal(t, "string", props["birthday"].Type)
	assert.Equal(t, "date", props["birthday"].Format)
	assert.Empty(t, props["birthday"].XGraphQLScalar)
}

const nullableSchema = `
enum Color { RED GREEN BLUE }

input FilterInput {
  name: String
  color: Color
}

type Item {
  id: ID!
  name: String!
  tag: String
  color: Color!
}

type Query {
  getItem(id: ID!): Item
  listItems(filter: FilterInput): [Item!]!
}
`

// TestConvert_NullableScalarField verifies that nullable scalar fields in a response type carry
// nullable: true, while non-null fields do not.
func TestConvert_NullableScalarField(t *testing.T) {
	// Arrange
	s, _ := setup(t, nullableSchema)
	ops := loadOps(t, s, map[string]string{
		"GetItem": `query GetItem($id: ID!) { getItem(id: $id) { id name tag } }`,
	})

	// Act
	conv := converter.New(s)
	_, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	// getItem (nullable) is extracted to a component
	itemComp := conv.Components()["GetItemItem"]
	require.NotNil(t, itemComp)
	props := itemComp.Properties
	assert.False(t, props["id"].Nullable, "id is non-null, must not be nullable")
	assert.False(t, props["name"].Nullable, "name is non-null, must not be nullable")
	assert.True(t, props["tag"].Nullable, "tag is nullable, must have nullable: true")
}

// TestConvert_NullableReturnType verifies that a nullable root return type (e.g. getItem: Item)
// produces a nullable schema in the data envelope (allOf + nullable wrapper around the $ref).
func TestConvert_NullableReturnType(t *testing.T) {
	// Arrange
	s, _ := setup(t, nullableSchema)
	ops := loadOps(t, s, map[string]string{
		"GetItem": `query GetItem($id: ID!) { getItem(id: $id) { id } }`,
	})

	// Act
	conv := converter.New(s)
	items, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	data := items[0].Response.Content["application/json"].Schema.Properties["data"]
	getItemSchema := data.Properties["getItem"]
	assert.True(t, getItemSchema.Nullable, "getItem return type is nullable, object schema must have nullable: true")
}

// TestConvert_NullableEnumField_UsesAllOf verifies that nullable $ref schemas (enums, input objects)
// use the allOf+nullable wrapper instead of placing nullable directly on a $ref node.
// OAS 3.0 does not allow $ref and nullable at the same level.
func TestConvert_NullableEnumField_UsesAllOf(t *testing.T) {
	// Arrange
	s, _ := setup(t, nullableSchema)
	ops := loadOps(t, s, map[string]string{
		// use FilterInput which has nullable Color enum
		"ListItems": `query ListItems($filter: FilterInput) { listItems(filter: $filter) { id } }`,
	})

	// Act
	conv := converter.New(s)
	items, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	// nullable FilterInput → allOf + nullable wrapping a $ref
	filterParamSchema := items[0].Parameters[0].Schema
	assert.True(t, filterParamSchema.Nullable)
	require.Len(t, filterParamSchema.AllOf, 1)
	assert.Equal(t, "#/components/schemas/FilterInput", filterParamSchema.AllOf[0].Ref)

	// The nullable Color enum field lives inside the FilterInput component
	filterInputComp := conv.Components()["FilterInput"]
	require.NotNil(t, filterInputComp)
	colorSchema := filterInputComp.Properties["color"]
	// nullable $ref must not sit bare; it must use allOf + nullable
	assert.Empty(t, colorSchema.Ref, "nullable enum must not have bare $ref")
	require.Len(t, colorSchema.AllOf, 1)
	assert.Equal(t, "#/components/schemas/Color", colorSchema.AllOf[0].Ref)
	assert.True(t, colorSchema.Nullable)
}

// TestConvert_NullableInputParam verifies that a nullable query parameter backed by an input
// object is marked optional (required: false) and carries nullable: true.
func TestConvert_NullableInputParam(t *testing.T) {
	// Arrange
	s, _ := setup(t, nullableSchema)
	ops := loadOps(t, s, map[string]string{
		"ListItems": `query ListItems($filter: FilterInput) { listItems(filter: $filter) { id } }`,
	})

	// Act
	conv := converter.New(s)
	items, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	param := items[0].Parameters[0]
	assert.False(t, param.Required)
	assert.True(t, param.Schema.Nullable, "nullable input object param must have nullable: true")
}

// TestConvert_RequiredResponseFields verifies that only non-null schema fields appear in the
// required array of the generated component schema.
// Item has id: ID! (required), name: String! (required), tag: String (optional).
func TestConvert_RequiredResponseFields(t *testing.T) {
	// Arrange
	s, _ := setup(t, nullableSchema)
	ops := loadOps(t, s, map[string]string{
		"GetItem": `query GetItem($id: ID!) { getItem(id: $id) { id name tag } }`,
	})

	// Act
	conv := converter.New(s)
	_, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	// getItem (nullable) is extracted to a component; required lives there
	getItemComp := conv.Components()["GetItemItem"]
	require.NotNil(t, getItemComp)
	assert.ElementsMatch(t, []string{"id", "name"}, getItemComp.Required)
}

// fragmentSchema mirrors a realistic schema with fragments: a Team type selected
// via a named fragment, and nested fragments (as in gql-persisted operations).
const fragmentSchema = `
type District {
  id: ID!
  name: String!
  stateCode: String!
}

type Team {
  id: ID!
  name: String!
  district: District!
}

type Result {
  id: ID!
  position: Int!
  team: Team!
}

type Query {
  results: [Result!]!
  teamById(id: ID!): Team
}
`

// TestConvert_FragmentSpreadExpandsFields verifies that named fragment spreads are inlined —
// their fields appear in the component as if written directly in the selection set.
func TestConvert_FragmentSpreadExpandsFields(t *testing.T) {
	// Arrange
	s, _ := setup(t, fragmentSchema)
	ops := loadOps(t, s, map[string]string{
		"GetTeam": `
			query GetTeam($id: ID!) { teamById(id: $id) { ...TeamFragment } }
			fragment TeamFragment on Team { id name district { id name stateCode } }
		`,
	})

	// Act
	conv := converter.New(s)
	_, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	teamComp := conv.Components()["GetTeamTeam"]
	require.NotNil(t, teamComp, "Team component must be created for fragment-spread sub-selection")
	assert.Equal(t, "object", teamComp.Type)
	assert.Contains(t, teamComp.Properties, "id")
	assert.Contains(t, teamComp.Properties, "name")
	assert.Contains(t, teamComp.Properties, "district")
}

// TestConvert_NestedFragmentSpreadsExpand verifies that deeply nested named fragment spreads
// (ResultFragment → TeamFragment) are fully expanded: each type gets its own component with
// the correct fields.
func TestConvert_NestedFragmentSpreadsExpand(t *testing.T) {
	// Arrange
	s, _ := setup(t, fragmentSchema)
	ops := loadOps(t, s, map[string]string{
		"GetResults": `
			query GetResults {
				results {
					...ResultFragment
				}
			}
			fragment ResultFragment on Result { id position team { ...TeamFragment } }
			fragment TeamFragment on Team { id name district { id name stateCode } }
		`,
	})

	// Act
	conv := converter.New(s)
	_, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	resultComp := conv.Components()["GetResultsResult"]
	require.NotNil(t, resultComp, "Result component must be created")
	assert.Contains(t, resultComp.Properties, "id")
	assert.Contains(t, resultComp.Properties, "position")
	assert.Contains(t, resultComp.Properties, "team")

	// team field inside Result is itself a component ref
	teamRef := resultComp.Properties["team"]
	assert.Equal(t, "#/components/schemas/GetResultsTeam", teamRef.Ref)

	teamComp := conv.Components()["GetResultsTeam"]
	require.NotNil(t, teamComp, "Team component must be created from nested fragment")
	assert.Contains(t, teamComp.Properties, "id")
	assert.Contains(t, teamComp.Properties, "name")
	assert.Contains(t, teamComp.Properties, "district")
}

// TestConvert_InlineFragmentExpandsFields verifies that an inline fragment on the same concrete
// type is merged into a single flat component schema (not a oneOf).
func TestConvert_InlineFragmentExpandsFields(t *testing.T) {
	// Arrange
	s, _ := setup(t, fragmentSchema)
	ops := loadOps(t, s, map[string]string{
		"GetTeam": `query GetTeam($id: ID!) { teamById(id: $id) { ... on Team { id name } } }`,
	})

	// Act
	conv := converter.New(s)
	_, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	teamComp := conv.Components()["GetTeamTeam"]
	require.NotNil(t, teamComp, "Team component must be created from inline fragment")
	assert.Contains(t, teamComp.Properties, "id")
	assert.Contains(t, teamComp.Properties, "name")
}

// interfaceSchema defines an interface with two concrete implementations and a sub-interface,
// used to verify oneOf generation and interface-fragment field filtering.
const interfaceSchema = `
interface Node {
  id: ID!
}

interface HasMeta implements Node {
  id: ID!
  metadata: String!
}

type Article implements HasMeta & Node {
  id: ID!
  metadata: String!
  title: String!
  body: String!
}

type Video implements Node {
  id: ID!
  title: String!
  duration: Int!
}

type Query {
  items: [Node!]!
}
`

// TestConvert_AbstractType_GeneratesOneOf verifies that selecting concrete-type inline fragments
// on an interface produces a oneOf schema — one variant per concrete type — with base-type
// fields merged into each variant and no inline properties on the oneOf wrapper itself.
func TestConvert_AbstractType_GeneratesOneOf(t *testing.T) {
	// Arrange
	s, _ := setup(t, interfaceSchema)
	ops := loadOps(t, s, map[string]string{
		"GetItems": `query GetItems {
			items {
				id
				... on Article { title body }
				... on Video { duration }
			}
		}`,
	})

	// Act
	conv := converter.New(s)
	_, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	// The abstract Node component becomes a oneOf, not a flat object.
	nodeComp := conv.Components()["GetItemsNode"]
	require.NotNil(t, nodeComp)
	require.Len(t, nodeComp.OneOf, 2)
	assert.Nil(t, nodeComp.Properties, "oneOf schema must not have inline properties")

	// Article variant: id (from Node) + title + body (from Article fragment).
	articleComp := conv.Components()["GetItemsArticle"]
	require.NotNil(t, articleComp, "Article component must exist")
	assert.Contains(t, articleComp.Properties, "id")
	assert.Contains(t, articleComp.Required, "id")
	assert.Contains(t, articleComp.Properties, "title")
	assert.Contains(t, articleComp.Required, "title")
	assert.Contains(t, articleComp.Properties, "body")
	assert.Contains(t, articleComp.Required, "body")
	assert.NotContains(t, articleComp.Properties, "duration")

	// Video variant: id (from Node) + duration (from Video fragment).
	videoComp := conv.Components()["GetItemsVideo"]
	require.NotNil(t, videoComp, "Video component must exist")
	assert.Contains(t, videoComp.Properties, "id")
	assert.Contains(t, videoComp.Required, "id")
	assert.Contains(t, videoComp.Properties, "duration")
	assert.Contains(t, videoComp.Required, "duration")
	assert.NotContains(t, videoComp.Properties, "title")
}

// TestConvert_AbstractType_OnlySingleConcreteType_StillOneOf verifies that even a single
// concrete-type inline fragment produces a oneOf (not a flat schema).
func TestConvert_AbstractType_OnlySingleConcreteType_StillOneOf(t *testing.T) {
	// Arrange
	s, _ := setup(t, interfaceSchema)
	ops := loadOps(t, s, map[string]string{
		"GetItems": `query GetItems { items { id ... on Article { title } } }`,
	})

	// Act
	conv := converter.New(s)
	_, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	nodeComp := conv.Components()["GetItemsNode"]
	require.NotNil(t, nodeComp)
	require.Len(t, nodeComp.OneOf, 1)

	articleComp := conv.Components()["GetItemsArticle"]
	require.NotNil(t, articleComp)
	assert.Contains(t, articleComp.Required, "id")
	assert.Contains(t, articleComp.Required, "title")
}

// TestConvert_AbstractType_WithTypename_EnumAndDiscriminator verifies that selecting __typename
// adds an OAS discriminator to the oneOf wrapper and pins each variant's __typename field to a
// single-value enum of the concrete type name.
func TestConvert_AbstractType_WithTypename_EnumAndDiscriminator(t *testing.T) {
	// Arrange
	s, _ := setup(t, interfaceSchema)
	ops := loadOps(t, s, map[string]string{
		"GetItems": `query GetItems {
			items {
				__typename
				id
				... on Article { title }
				... on Video { duration }
			}
		}`,
	})

	// Act
	conv := converter.New(s)
	_, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	// oneOf wrapper gets a discriminator pointing at __typename.
	nodeComp := conv.Components()["GetItemsNode"]
	require.NotNil(t, nodeComp)
	require.NotNil(t, nodeComp.Discriminator)
	assert.Equal(t, "__typename", nodeComp.Discriminator.PropertyName)

	// Each concrete variant has __typename as a single-value enum.
	articleComp := conv.Components()["GetItemsArticle"]
	require.NotNil(t, articleComp)
	typenameSchema := articleComp.Properties["__typename"]
	require.NotNil(t, typenameSchema)
	assert.Equal(t, "string", typenameSchema.Type)
	assert.Equal(t, []string{"Article"}, typenameSchema.Enum)
	assert.Contains(t, articleComp.Required, "__typename")

	videoComp := conv.Components()["GetItemsVideo"]
	require.NotNil(t, videoComp)
	assert.Equal(t, []string{"Video"}, videoComp.Properties["__typename"].Enum)
}

// TestConvert_AbstractType_AliasedTypename_DiscriminatorUsesAlias verifies that when
// __typename is aliased (e.g. kind: __typename), the discriminator property and each
// variant's enum key both use the alias name instead of "__typename".
func TestConvert_AbstractType_AliasedTypename_DiscriminatorUsesAlias(t *testing.T) {
	// Arrange — __typename aliased as "kind"
	s, _ := setup(t, interfaceSchema)
	ops := loadOps(t, s, map[string]string{
		"GetItems": `query GetItems { items { kind: __typename id ... on Article { title } } }`,
	})

	// Act
	conv := converter.New(s)
	_, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	nodeComp := conv.Components()["GetItemsNode"]
	require.NotNil(t, nodeComp)
	require.NotNil(t, nodeComp.Discriminator)
	assert.Equal(t, "kind", nodeComp.Discriminator.PropertyName)

	articleComp := conv.Components()["GetItemsArticle"]
	require.NotNil(t, articleComp)
	assert.Contains(t, articleComp.Properties, "kind")
	assert.Equal(t, []string{"Article"}, articleComp.Properties["kind"].Enum)
}

// TestConvert_AbstractType_NoTypename_NoDiscriminator verifies that omitting __typename from
// the selection produces no discriminator on the oneOf schema.
func TestConvert_AbstractType_NoTypename_NoDiscriminator(t *testing.T) {
	// Arrange
	s, _ := setup(t, interfaceSchema)
	ops := loadOps(t, s, map[string]string{
		"GetItems": `query GetItems { items { id ... on Article { title } } }`,
	})

	// Act
	conv := converter.New(s)
	_, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	nodeComp := conv.Components()["GetItemsNode"]
	require.NotNil(t, nodeComp)
	assert.Nil(t, nodeComp.Discriminator, "no __typename selected → no discriminator")
}

// TestConvert_AbstractType_FragmentSpread_OnConcreteType_TriggersOneOf verifies that a named
// fragment spread targeting a concrete type (not just an inline fragment) also triggers oneOf
// generation, equivalent to an inline fragment on that concrete type.
func TestConvert_AbstractType_FragmentSpread_OnConcreteType_TriggersOneOf(t *testing.T) {
	// Arrange
	s, _ := setup(t, interfaceSchema)
	ops := loadOps(t, s, map[string]string{
		"GetItems": `
			query GetItems {
				items {
					__typename
					id
					...ArticleFragment
					... on Video { duration }
				}
			}
			fragment ArticleFragment on Article { title body }
		`,
	})

	// Act
	conv := converter.New(s)
	_, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	nodeComp := conv.Components()["GetItemsNode"]
	require.NotNil(t, nodeComp)
	require.Len(t, nodeComp.OneOf, 2)
	require.NotNil(t, nodeComp.Discriminator)
	assert.Equal(t, "__typename", nodeComp.Discriminator.PropertyName)

	articleComp := conv.Components()["GetItemsArticle"]
	require.NotNil(t, articleComp, "Article component must be created from named fragment spread")
	assert.Contains(t, articleComp.Properties, "title")
	assert.Contains(t, articleComp.Properties, "body")
	assert.Equal(t, []string{"Article"}, articleComp.Properties["__typename"].Enum)
}

// TestConvert_InterfaceFragment_AppliesOnlyToImplementors verifies that fields from an interface
// fragment are only included in concrete types that implement that interface. Video does not
// implement HasMeta, so it must not receive the metadata field.
func TestConvert_InterfaceFragment_AppliesOnlyToImplementors(t *testing.T) {
	// Arrange — HasMeta is only implemented by Article, not Video.
	s, _ := setup(t, interfaceSchema)
	ops := loadOps(t, s, map[string]string{
		"GetItems": `query GetItems {
			items {
				id
				... on HasMeta { metadata }
				... on Article { title }
				... on Video { duration }
			}
		}`,
	})

	// Act
	conv := converter.New(s)
	_, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	// Article implements HasMeta → gets metadata (required because metadata: String!).
	articleComp := conv.Components()["GetItemsArticle"]
	require.NotNil(t, articleComp)
	assert.Contains(t, articleComp.Properties, "metadata")
	assert.Contains(t, articleComp.Required, "metadata")

	// Video does NOT implement HasMeta → no metadata field.
	videoComp := conv.Components()["GetItemsVideo"]
	require.NotNil(t, videoComp)
	assert.NotContains(t, videoComp.Properties, "metadata")
}

// TestConvert_Typename_NoConcreteFragments_FlatSchema verifies that selecting __typename
// without any concrete-type inline fragments produces a flat schema (not oneOf).
// __typename is a string field in the flat schema rather than a discriminator.
func TestConvert_Typename_NoConcreteFragments_FlatSchema(t *testing.T) {
	// Arrange
	s, _ := setup(t, interfaceSchema)
	ops := loadOps(t, s, map[string]string{
		"GetItems": `query GetItems { items { __typename id } }`,
	})

	// Act
	conv := converter.New(s)
	_, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	nodeComp := conv.Components()["GetItemsNode"]
	require.NotNil(t, nodeComp)
	assert.Nil(t, nodeComp.OneOf, "no concrete fragments → flat schema, not oneOf")
	assert.Contains(t, nodeComp.Properties, "__typename")
	assert.Equal(t, "string", nodeComp.Properties["__typename"].Type)
}

// TestConvert_NonNullableListIsNotNullable verifies that a non-null list ([Item!]!) and its
// non-null item type are produced without nullable: true anywhere in the schema.
func TestConvert_NonNullableListIsNotNullable(t *testing.T) {
	// Arrange — listItems returns [Item!]!, non-null list of non-null items.
	s, _ := setup(t, nullableSchema)
	ops := loadOps(t, s, map[string]string{
		"ListItems": `query ListItems($filter: FilterInput) { listItems(filter: $filter) { id } }`,
	})

	// Act
	conv := converter.New(s)
	items, err := conv.Convert(ops, "rest", "/api")

	// Assert
	require.NoError(t, err)

	data := items[0].Response.Content["application/json"].Schema.Properties["data"]
	listSchema := data.Properties["listItems"]
	assert.Equal(t, "array", listSchema.Type)
	assert.False(t, listSchema.Nullable, "[Item!]! list must not be nullable")
	assert.False(t, listSchema.Items.Nullable, "Item! items must not be nullable")
}
