package converter_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Thomasparsley/peroapi/internal/converter"
)

// abstractSchema exercises interfaces, unions, and repeated selections of the same type —
// the shapes the shared fixtures never cover.
const abstractSchema = `
scalar Byte

enum LocalDate { TODAY TOMORROW }

interface Node { id: ID! }

type User implements Node {
  id: ID!
  name: String!
  email: String!
  friend: User
}

type Product implements Node {
  id: ID!
  title: String!
}

union SearchResult = User | Product

type Query {
  me: User
  user(id: ID!): User
  node(id: ID!): Node
  search: [SearchResult!]!
  aByte: Byte
  when: LocalDate
}
`

// dataSchema returns the schema of the "data" property inside the response envelope.
func dataSchema(t *testing.T, item *converter.PathItem) *converter.Schema {
	t.Helper()
	require.NotNil(t, item.Response)
	env := item.Response.Content["application/json"].Schema
	require.NotNil(t, env)
	require.Contains(t, env.Properties, "data")
	return env.Properties["data"]
}

// refName extracts the component name a schema $refs, unwrapping the allOf-nullable wrapper
// that withNullable applies to nullable ref fields.
func refName(t *testing.T, s *converter.Schema) string {
	t.Helper()
	require.NotNil(t, s)
	ref := s.Ref
	if ref == "" {
		require.Len(t, s.AllOf, 1, "expected a $ref or allOf-wrapped $ref")
		ref = s.AllOf[0].Ref
	}
	require.NotEmpty(t, ref)
	return strings.TrimPrefix(ref, "#/components/schemas/")
}

// TestConvert_SameTypeDifferentSelections verifies that two fields returning the same GraphQL
// type but selecting different sub-fields in one operation get distinct components rather than
// colliding onto the first field's schema.
func TestConvert_SameTypeDifferentSelections(t *testing.T) {
	s, _ := setup(t, abstractSchema)
	ops := loadOps(t, s, map[string]string{
		"Q": `query Q { me { id } other: user(id: "1") { id name email } }`,
	})

	conv := converter.New(s)
	items, err := conv.Convert(ops, "post", "/api")
	require.NoError(t, err)
	require.Len(t, items, 1)

	data := dataSchema(t, items[0])
	meRef := refName(t, data.Properties["me"])
	otherRef := refName(t, data.Properties["other"])
	require.NotEqual(t, meRef, otherRef, "distinct selections must not share a component")

	comps := conv.Components()
	assert.ElementsMatch(t, []string{"id"}, keys(comps[meRef].Properties))
	assert.ElementsMatch(t, []string{"id", "name", "email"}, keys(comps[otherRef].Properties))
}

// TestConvert_DuplicateAliasMerges verifies that selecting the same field twice with different
// sub-selections merges the sub-selections instead of dropping the later one.
func TestConvert_DuplicateAliasMerges(t *testing.T) {
	s, _ := setup(t, abstractSchema)
	ops := loadOps(t, s, map[string]string{
		"D": `query D { me { id } me { name } }`,
	})

	conv := converter.New(s)
	items, err := conv.Convert(ops, "post", "/api")
	require.NoError(t, err)

	data := dataSchema(t, items[0])
	comps := conv.Components()
	meComp := comps[refName(t, data.Properties["me"])]
	require.NotNil(t, meComp)
	assert.ElementsMatch(t, []string{"id", "name"}, keys(meComp.Properties),
		"both selections of me must be merged")
}

// TestConvert_UnionOneOfDiscriminator verifies that a union selection with concrete inline
// fragments and __typename produces a oneOf with a discriminator mapping from the runtime
// __typename value to the generated variant component.
func TestConvert_UnionOneOfDiscriminator(t *testing.T) {
	s, _ := setup(t, abstractSchema)
	ops := loadOps(t, s, map[string]string{
		"S": `query S { search { __typename ... on User { name } ... on Product { title } } }`,
	})

	conv := converter.New(s)
	items, err := conv.Convert(ops, "post", "/api")
	require.NoError(t, err)

	data := dataSchema(t, items[0])
	search := data.Properties["search"]
	require.NotNil(t, search)
	require.Equal(t, "array", search.Type)

	comps := conv.Components()
	union := comps[refName(t, search.Items)]
	require.NotNil(t, union)
	require.Len(t, union.OneOf, 2)
	require.NotNil(t, union.Discriminator)
	assert.Equal(t, "__typename", union.Discriminator.PropertyName)

	// Every raw GraphQL type name must map to an existing variant component.
	require.Contains(t, union.Discriminator.Mapping, "User")
	require.Contains(t, union.Discriminator.Mapping, "Product")
	for typeName, ref := range union.Discriminator.Mapping {
		comp := strings.TrimPrefix(ref, "#/components/schemas/")
		assert.Contains(t, comps, comp, "discriminator mapping for %q points at missing component", typeName)
	}
}

// TestConvert_SchemaTypeNotOverriddenByCustomScalarName verifies that a schema-defined enum
// named like a known custom scalar (LocalDate) is treated as the enum, not silently replaced
// by the hardcoded date-scalar schema.
func TestConvert_SchemaTypeNotOverriddenByCustomScalarName(t *testing.T) {
	s, _ := setup(t, abstractSchema)
	ops := loadOps(t, s, map[string]string{
		"W": `query W { when }`,
	})

	conv := converter.New(s)
	_, err := conv.Convert(ops, "post", "/api")
	require.NoError(t, err)

	local := conv.Components()["LocalDate"]
	require.NotNil(t, local, "schema-defined enum LocalDate must be registered as a component")
	assert.ElementsMatch(t, []string{"TODAY", "TOMORROW"}, local.Enum)
}

// TestConvert_DeclaredCustomScalarMapping verifies that a scalar the schema actually declares
// still receives its richer format mapping (Byte → integer 0..255).
func TestConvert_DeclaredCustomScalarMapping(t *testing.T) {
	s, _ := setup(t, abstractSchema)
	ops := loadOps(t, s, map[string]string{
		"B": `query B { aByte }`,
	})

	conv := converter.New(s)
	items, err := conv.Convert(ops, "post", "/api")
	require.NoError(t, err)

	b := dataSchema(t, items[0]).Properties["aByte"]
	require.NotNil(t, b)
	assert.Equal(t, "integer", b.Type)
	require.NotNil(t, b.Maximum)
	assert.Equal(t, int64(255), *b.Maximum)
}

// TestConvert_MultipleOperationsPerEntryErrors verifies that a persisted entry containing more
// than one GraphQL operation is rejected rather than silently dropping the extras.
func TestConvert_MultipleOperationsPerEntryErrors(t *testing.T) {
	s, _ := setup(t, abstractSchema)
	ops := loadOps(t, s, map[string]string{
		"Multi": `query A { me { id } } query B { user(id: "1") { id } }`,
	})

	conv := converter.New(s)
	_, err := conv.Convert(ops, "post", "/api")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one")
}

// TestValidateConvention verifies the accepted and rejected method-convention values.
func TestValidateConvention(t *testing.T) {
	for _, v := range []string{"rest", "post", "get"} {
		assert.NoError(t, converter.ValidateConvention(v), "%q should be valid", v)
	}
	for _, v := range []string{"GET", "Post", "", "bogus"} {
		assert.Error(t, converter.ValidateConvention(v), "%q should be invalid", v)
	}
}

// keys returns the keys of a SchemaMap for order-independent comparison.
func keys(m converter.SchemaMap) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
