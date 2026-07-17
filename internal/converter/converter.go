// Package converter transforms parsed GraphQL operations into OpenAPI path definitions.
package converter

import (
	"fmt"
	"os"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/Thomasparsley/peroapi/internal/parser"
)

// SchemaMap is a map of schema names to Schema objects.
type SchemaMap map[string]*Schema

// ContentMap is a map of media type strings to MediaTypeObject objects.
type ContentMap map[string]*MediaTypeObject

// Discriminator is the OpenAPI discriminator object.
type Discriminator struct {
	PropertyName string `yaml:"propertyName"`
}

// Schema is the OpenAPI schema object representation.
type Schema struct {
	Ref                  string         `yaml:"$ref,omitempty"`
	Type                 string         `yaml:"type,omitempty"`
	Format               string         `yaml:"format,omitempty"`
	Nullable             bool           `yaml:"nullable,omitempty"`
	Minimum              *int64         `yaml:"minimum,omitempty"`
	Maximum              *int64         `yaml:"maximum,omitempty"`
	Enum                 []string       `yaml:"enum,omitempty"`
	AllOf                []*Schema      `yaml:"allOf,omitempty"`
	OneOf                []*Schema      `yaml:"oneOf,omitempty"`
	Discriminator        *Discriminator `yaml:"discriminator,omitempty"`
	Properties           SchemaMap      `yaml:"properties,omitempty"`
	Items                *Schema        `yaml:"items,omitempty"`
	Required             []string       `yaml:"required,omitempty"`
	XGraphQLScalar       string         `yaml:"x-graphql-scalar,omitempty"`
	AdditionalProperties bool           `yaml:"-"`
}

// Parameter represents an OpenAPI query parameter.
type Parameter struct {
	Name     string  `yaml:"name"`
	In       string  `yaml:"in"`
	Required bool    `yaml:"required"`
	Schema   *Schema `yaml:"schema"`
}

// RequestBody represents an OpenAPI request body.
type RequestBody struct {
	Required bool       `yaml:"required"`
	Content  ContentMap `yaml:"content"`
}

// MediaTypeObject holds a schema for a media type.
type MediaTypeObject struct {
	Schema *Schema `yaml:"schema"`
}

// Response represents an OpenAPI response.
type Response struct {
	Description string     `yaml:"description"`
	Content     ContentMap `yaml:"content"`
}

// PathItem is a converted operation ready for OpenAPI emission.
type PathItem struct {
	OperationName string
	Path          string
	Method        string
	Parameters    []*Parameter
	RequestBody   *RequestBody
	Response      *Response
}

// defaultRootTypeNames are the conventional GraphQL root type names used when the
// schema does not explicitly define a root operation type.
const (
	defaultQueryTypeName        = "Query"
	defaultMutationTypeName     = "Mutation"
	defaultSubscriptionTypeName = "Subscription"
)

const (
	graphQLTypeName = "__typename"

	componentsSchemaPrefix = "#/components/schemas/"

	httpMethodGET  = "GET"
	httpMethodPOST = "POST"

	conventionPost = "post"
	conventionGet  = "get"

	mediaTypeJSON = "application/json"

	paramInQuery = "query"

	schemaTypeObject  = "object"
	schemaTypeString  = "string"
	schemaTypeArray   = "array"
	schemaTypeInteger = "integer"
	schemaTypeNumber  = "number"
	schemaTypeBoolean = "boolean"

	schemaFormatInt32 = "int32"
	schemaFormatFloat = "float"
	schemaFormatDate  = "date"

	variablesKey       = "variables"
	successfulResponse = "Successful response"
)

// Converter walks GraphQL operations and produces PathItems.
type Converter struct {
	schema     *ast.Schema
	components SchemaMap
	currentOp  string
}

// New creates a Converter for the given schema.
func New(schema *ast.Schema) *Converter {
	return &Converter{schema: schema}
}

// Components returns the schemas collected during Convert, keyed by component name.
// Call this after Convert to obtain schemas for the OpenAPI components/schemas section.
func (c *Converter) Components() SchemaMap {
	return c.components
}

// Convert processes all operations and returns PathItems.
func (c *Converter) Convert(ops []*parser.Operation, methodConvention, basePath string) ([]*PathItem, error) {
	c.components = make(SchemaMap)
	var items []*PathItem
	for _, op := range ops {
		if len(op.Doc.Operations) == 0 {
			continue
		}
		item, err := c.convertOperation(op, methodConvention, basePath)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// convertOperation converts a single parsed operation into a PathItem.
// GET operations carry their variables as query parameters; everything else
// packs them into a JSON request body.
func (c *Converter) convertOperation(op *parser.Operation, methodConvention, basePath string) (*PathItem, error) {
	gqlOp := op.Doc.Operations[0]
	c.currentOp = op.Name

	item := &PathItem{
		OperationName: op.Name,
		Path:          strings.TrimRight(basePath, "/") + "/" + op.Name,
		Method:        resolveMethod(gqlOp.Operation, methodConvention),
	}

	if item.Method == httpMethodGET {
		params, err := c.variablesToParams(gqlOp.VariableDefinitions)
		if err != nil {
			return nil, fmt.Errorf("operation %q params: %w", op.Name, err)
		}
		item.Parameters = params
	} else {
		body, err := c.variablesToBody(gqlOp.VariableDefinitions)
		if err != nil {
			return nil, fmt.Errorf("operation %q body: %w", op.Name, err)
		}
		item.RequestBody = body
	}

	resp, err := c.selectionSetToResponse(gqlOp.SelectionSet, gqlOp.Operation)
	if err != nil {
		return nil, fmt.Errorf("operation %q response: %w", op.Name, err)
	}
	item.Response = resp

	return item, nil
}

// resolveMethod maps a method convention string and GraphQL operation type to an HTTP method.
// "post" forces POST for everything, "get" forces GET for everything; the default "rest"
// convention maps mutations to POST and queries/subscriptions to GET.
func resolveMethod(opType ast.Operation, convention string) string {
	switch convention {
	case conventionPost:
		return httpMethodPOST
	case conventionGet:
		return httpMethodGET
	default: // "rest"
		if opType == ast.Mutation {
			return httpMethodPOST
		}
		return httpMethodGET
	}
}

// objectSchema builds an object schema, omitting the required list when empty.
func objectSchema(props SchemaMap, required []string) *Schema {
	obj := &Schema{Type: schemaTypeObject, Properties: props}
	if len(required) > 0 {
		obj.Required = required
	}
	return obj
}

// variablesToParams converts GraphQL variable definitions to OpenAPI query parameters.
// Each variable becomes an individual query parameter; non-null variables are marked required.
func (c *Converter) variablesToParams(vars ast.VariableDefinitionList) ([]*Parameter, error) {
	var params []*Parameter
	for _, v := range vars {
		s, err := c.typeRefToSchema(v.Type)
		if err != nil {
			return nil, err
		}
		params = append(params, &Parameter{
			Name:     v.Variable,
			In:       paramInQuery,
			Required: v.Type.NonNull,
			Schema:   s,
		})
	}
	return params, nil
}

// variablesToBody packs all GraphQL variable definitions into a single JSON request body object.
// Returns nil when the operation has no variables (operation requires no input).
func (c *Converter) variablesToBody(vars ast.VariableDefinitionList) (*RequestBody, error) {
	if len(vars) == 0 {
		return nil, nil
	}

	props := make(SchemaMap)
	var required []string
	for _, v := range vars {
		s, err := c.typeRefToSchema(v.Type)
		if err != nil {
			return nil, err
		}
		props[v.Variable] = s
		if v.Type.NonNull {
			required = append(required, v.Variable)
		}
	}

	body := objectSchema(SchemaMap{variablesKey: objectSchema(props, required)}, []string{variablesKey})
	return &RequestBody{
		Required: true,
		Content:  ContentMap{mediaTypeJSON: {Schema: body}},
	}, nil
}

// withNullable marks a schema as nullable. For $ref schemas, OAS 3.0 requires
// wrapping in allOf since $ref and nullable cannot coexist at the same level.
func withNullable(s *Schema) *Schema {
	if s.Ref != "" {
		return &Schema{AllOf: []*Schema{s}, Nullable: true}
	}
	s.Nullable = true
	return s
}

// typeRefToSchema converts a GraphQL type reference to an OpenAPI schema.
func (c *Converter) typeRefToSchema(t *ast.Type) (*Schema, error) {
	var schema *Schema
	if t.Elem != nil {
		items, err := c.typeRefToSchema(t.Elem)
		if err != nil {
			return nil, err
		}
		schema = &Schema{Type: schemaTypeArray, Items: items}
	} else {
		var err error
		schema, err = c.namedTypeToSchema(t.NamedType)
		if err != nil {
			return nil, err
		}
	}
	if !t.NonNull {
		return withNullable(schema), nil
	}
	return schema, nil
}

// refToComponent registers the schema produced by build under name (once) and
// returns a $ref schema pointing at it. A nil sentinel is stored before build
// runs so cyclic types terminate instead of recursing forever.
func (c *Converter) refToComponent(name string, build func() (*Schema, error)) (*Schema, error) {
	if _, exists := c.components[name]; !exists {
		c.components[name] = nil // sentinel: prevents infinite recursion on cyclic types
		s, err := build()
		if err != nil {
			return nil, err
		}
		c.components[name] = s
	}
	return &Schema{Ref: componentsSchemaPrefix + name}, nil
}

// boundedIntSchema returns an int32 schema constrained to [min, max].
func boundedIntSchema(min, max int64) *Schema {
	return &Schema{Type: schemaTypeInteger, Format: schemaFormatInt32, Minimum: &min, Maximum: &max}
}

// builtinScalarSchema maps GraphQL built-in and well-known scalars to OpenAPI schemas.
// The second return value reports whether name was recognized.
func builtinScalarSchema(name string) (*Schema, bool) {
	switch name {
	case "String", "ID":
		return &Schema{Type: schemaTypeString}, true
	case "Int":
		return &Schema{Type: schemaTypeInteger, Format: schemaFormatInt32}, true
	case "Float":
		return &Schema{Type: schemaTypeNumber, Format: schemaFormatFloat}, true
	case "Boolean":
		return &Schema{Type: schemaTypeBoolean}, true
	case "UnsignedShort":
		return boundedIntSchema(0, 65535), true
	case "Byte":
		return boundedIntSchema(0, 255), true
	case "LocalDate":
		return &Schema{Type: schemaTypeString, Format: schemaFormatDate}, true
	default:
		return nil, false
	}
}

// namedTypeToSchema maps a named GraphQL type to its OpenAPI schema equivalent.
// Built-in scalars are converted directly; enums and input objects are registered in
// c.components and returned as $ref pointers so each type is only defined once.
// Unknown scalars fall back to a plain string schema with an x-graphql-scalar extension.
func (c *Converter) namedTypeToSchema(name string) (*Schema, error) {
	if s, ok := builtinScalarSchema(name); ok {
		return s, nil
	}

	typeDef, ok := c.schema.Types[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "warning: unknown scalar %q, treating as string\n", name)
		return &Schema{Type: schemaTypeString, XGraphQLScalar: name}, nil
	}

	switch typeDef.Kind {
	case ast.Scalar:
		return &Schema{Type: schemaTypeString, XGraphQLScalar: name}, nil
	case ast.Enum:
		return c.refToComponent(name, func() (*Schema, error) {
			return enumSchema(typeDef), nil
		})
	case ast.InputObject:
		return c.refToComponent(name, func() (*Schema, error) {
			return c.inputObjectToSchema(typeDef)
		})
	default:
		return &Schema{Type: schemaTypeString}, nil
	}
}

// enumSchema converts a GraphQL enum definition to a string schema with an enum list.
func enumSchema(def *ast.Definition) *Schema {
	values := make([]string, 0, len(def.EnumValues))
	for _, v := range def.EnumValues {
		values = append(values, v.Name)
	}
	return &Schema{Type: schemaTypeString, Enum: values}
}

// inputObjectToSchema converts a GraphQL input object type to an OpenAPI object schema.
// Non-null fields are listed under the schema's required array.
func (c *Converter) inputObjectToSchema(def *ast.Definition) (*Schema, error) {
	props := make(SchemaMap)
	var required []string

	for _, field := range def.Fields {
		s, err := c.typeRefToSchema(field.Type)
		if err != nil {
			return nil, err
		}
		props[field.Name] = s
		if field.Type.NonNull {
			required = append(required, field.Name)
		}
	}
	return objectSchema(props, required), nil
}

// selectionSetToResponse wraps the selection set schema in the standard GraphQL response envelope:
// { data: <selectionSchema>, errors: [{ message, locations, path }] }.
func (c *Converter) selectionSetToResponse(sel ast.SelectionSet, opType ast.Operation) (*Response, error) {
	dataSchema, err := c.selectionSetToSchema(sel, c.rootTypeName(opType))
	if err != nil {
		return nil, err
	}

	envelope := &Schema{
		Type: schemaTypeObject,
		Properties: SchemaMap{
			"data":   dataSchema,
			"errors": graphQLErrorsSchema(),
		},
	}

	return &Response{
		Description: successfulResponse,
		Content:     ContentMap{mediaTypeJSON: {Schema: envelope}},
	}, nil
}

// graphQLErrorsSchema describes the standard GraphQL errors array.
func graphQLErrorsSchema() *Schema {
	return &Schema{
		Type: schemaTypeArray,
		Items: &Schema{
			Type: schemaTypeObject,
			Properties: SchemaMap{
				"message":   {Type: schemaTypeString},
				"locations": {Type: schemaTypeArray, Items: &Schema{Type: schemaTypeObject}},
				"path":      {Type: schemaTypeArray, Items: &Schema{Type: schemaTypeString}},
			},
		},
	}
}

// rootTypeName returns the schema's root type name for the given operation kind.
// Falls back to conventional names ("Query", "Mutation", "Subscription") when the
// schema does not define an explicit root type — handles minimal test schemas.
func (c *Converter) rootTypeName(opType ast.Operation) string {
	switch opType {
	case ast.Mutation:
		if c.schema.Mutation != nil {
			return c.schema.Mutation.Name
		}
		return defaultMutationTypeName
	case ast.Subscription:
		if c.schema.Subscription != nil {
			return c.schema.Subscription.Name
		}
		return defaultSubscriptionTypeName
	default:
		if c.schema.Query != nil {
			return c.schema.Query.Name
		}
		return defaultQueryTypeName
	}
}

// typedField associates a field with the type context it should be resolved in
// and whether it originates from a type-narrowing inline fragment (making it optional).
type typedField struct {
	field      *ast.Field
	typeName   string
	isOptional bool
}

// collectTypedFields recursively collects fields with their type context, preserving
// inline fragment type conditions so fields can be looked up in the correct type.
// Fields inside a narrowing inline fragment (e.g. "... on SubType") are marked
// optional because not every concrete type in the result will carry them.
func collectTypedFields(sel ast.SelectionSet, parentTypeName string, optional bool) []typedField {
	var result []typedField
	for _, s := range sel {
		switch n := s.(type) {
		case *ast.Field:
			result = append(result, typedField{n, parentTypeName, optional})
		case *ast.FragmentSpread:
			if n.Definition != nil {
				result = append(result, collectFragmentFields(n.Definition.SelectionSet, n.Definition.TypeCondition, parentTypeName, optional)...)
			}
		case *ast.InlineFragment:
			result = append(result, collectFragmentFields(n.SelectionSet, n.TypeCondition, parentTypeName, optional)...)
		}
	}
	return result
}

// collectFragmentFields collects fields from a fragment's selection set, resolving
// them against the fragment's type condition (or the parent type when absent).
func collectFragmentFields(sel ast.SelectionSet, typeCondition, parentTypeName string, optional bool) []typedField {
	typeName := typeCondition
	if typeName == "" {
		typeName = parentTypeName
	}
	isNarrowing := typeName != parentTypeName
	return collectTypedFields(sel, typeName, optional || isNarrowing)
}

// collectConcreteTypes returns the distinct concrete (Object) type names that appear
// in type-narrowing inline fragments within sel. The parentTypeName itself is excluded.
func (c *Converter) collectConcreteTypes(sel ast.SelectionSet, parentTypeName string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, tf := range collectTypedFields(sel, parentTypeName, false) {
		if tf.typeName == parentTypeName || seen[tf.typeName] {
			continue
		}
		typeDef, ok := c.schema.Types[tf.typeName]
		if !ok || typeDef.Kind != ast.Object {
			continue
		}
		seen[tf.typeName] = true
		result = append(result, tf.typeName)
	}
	return result
}

// typeImplements reports whether typeName directly implements interfaceName.
func (c *Converter) typeImplements(typeName, interfaceName string) bool {
	typeDef, ok := c.schema.Types[typeName]
	if !ok {
		return false
	}
	for _, iface := range typeDef.Interfaces {
		if iface == interfaceName {
			return true
		}
	}
	return false
}

// fieldDefFor looks up the definition of fieldName on type typeName.
// Returns nil when the type or field is unknown.
func (c *Converter) fieldDefFor(typeName, fieldName string) *ast.FieldDefinition {
	typeDef, ok := c.schema.Types[typeName]
	if !ok {
		return nil
	}
	return typeDef.Fields.ForName(fieldName)
}

// concreteTypeIncludes reports whether a field resolved in the context of
// fieldTypeName belongs in the schema for concreteTypeName.
func (c *Converter) concreteTypeIncludes(concreteTypeName, fieldTypeName, parentTypeName string, concreteTypeSet map[string]bool) bool {
	switch {
	case fieldTypeName == parentTypeName:
		return true // base field — always included
	case fieldTypeName == concreteTypeName:
		return true // this concrete type's own fragment field
	case concreteTypeSet[fieldTypeName]:
		return false // a different concrete type's fragment
	default:
		// Interface fragment — include only if this concrete type implements it.
		return c.typeImplements(concreteTypeName, fieldTypeName)
	}
}

// buildConcreteTypeSchema builds a flat merged schema for a specific concrete type
// from a selection set that targets an abstract parent type. Fields from other concrete
// types' inline fragments are excluded; fields from interface fragments are included
// only if the concrete type implements that interface.
func (c *Converter) buildConcreteTypeSchema(sel ast.SelectionSet, parentTypeName, concreteTypeName string) (*Schema, error) {
	concreteTypeSet := make(map[string]bool)
	for _, ct := range c.collectConcreteTypes(sel, parentTypeName) {
		concreteTypeSet[ct] = true
	}

	props := make(SchemaMap)
	var required []string
	seen := make(map[string]bool)

	for _, tf := range collectTypedFields(sel, parentTypeName, false) {
		alias := tf.field.Alias
		if seen[alias] {
			continue
		}

		if tf.field.Name == graphQLTypeName {
			seen[alias] = true
			props[alias] = &Schema{Type: schemaTypeString, Enum: []string{concreteTypeName}}
			required = append(required, alias)
			continue
		}

		if !c.concreteTypeIncludes(concreteTypeName, tf.typeName, parentTypeName, concreteTypeSet) {
			continue
		}
		seen[alias] = true

		fieldDef := c.fieldDefFor(tf.typeName, tf.field.Name)
		if fieldDef == nil {
			continue
		}

		fieldSchema, err := c.fieldToSchema(fieldDef.Type, tf.field.SelectionSet)
		if err != nil {
			return nil, err
		}
		props[alias] = fieldSchema
		if fieldDef.Type.NonNull {
			required = append(required, alias)
		}
	}
	return objectSchema(props, required), nil
}

// selectionSetToSchema converts a GraphQL selection set to an OpenAPI schema.
// When the selection set targets an abstract type (interface/union) and contains concrete-type
// inline fragments, a oneOf discriminated union is produced — one variant per concrete type.
// Otherwise a flat merged object schema is returned.
func (c *Converter) selectionSetToSchema(sel ast.SelectionSet, parentTypeName string) (*Schema, error) {
	if len(sel) == 0 {
		return &Schema{Type: schemaTypeObject}, nil
	}
	if _, ok := c.schema.Types[parentTypeName]; !ok {
		return &Schema{Type: schemaTypeObject}, nil
	}
	if concreteTypes := c.collectConcreteTypes(sel, parentTypeName); len(concreteTypes) > 0 {
		return c.oneOfSchema(sel, parentTypeName, concreteTypes)
	}
	return c.mergedObjectSchema(sel, parentTypeName)
}

// oneOfSchema produces a discriminated union with one $ref variant per concrete
// type appearing in the selection set's type-narrowing fragments.
func (c *Converter) oneOfSchema(sel ast.SelectionSet, parentTypeName string, concreteTypes []string) (*Schema, error) {
	oneOf := make([]*Schema, 0, len(concreteTypes))
	for _, ct := range concreteTypes {
		ref, err := c.refToComponent(c.currentOp+ct, func() (*Schema, error) {
			return c.buildConcreteTypeSchema(sel, parentTypeName, ct)
		})
		if err != nil {
			return nil, err
		}
		oneOf = append(oneOf, ref)
	}

	result := &Schema{OneOf: oneOf}

	// If __typename (or an alias of it) is selected, add an OAS discriminator so
	// that tooling can map each variant to its concrete type by name.
	for _, tf := range collectTypedFields(sel, parentTypeName, false) {
		if tf.field.Name == graphQLTypeName {
			result.Discriminator = &Discriminator{PropertyName: tf.field.Alias}
			break
		}
	}
	return result, nil
}

// mergedObjectSchema flattens a selection set (including all fragments) into a
// single object schema. Fields from narrowing fragments stay optional.
func (c *Converter) mergedObjectSchema(sel ast.SelectionSet, parentTypeName string) (*Schema, error) {
	props := make(SchemaMap)
	var required []string
	seen := make(map[string]bool)

	for _, tf := range collectTypedFields(sel, parentTypeName, false) {
		alias := tf.field.Alias
		if seen[alias] {
			continue
		}
		seen[alias] = true

		if tf.field.Name == graphQLTypeName {
			props[alias] = &Schema{Type: schemaTypeString}
			continue
		}

		fieldDef := c.fieldDefFor(tf.typeName, tf.field.Name)
		if fieldDef == nil {
			continue
		}

		fieldSchema, err := c.fieldToSchema(fieldDef.Type, tf.field.SelectionSet)
		if err != nil {
			return nil, err
		}
		props[alias] = fieldSchema
		if fieldDef.Type.NonNull && !tf.isOptional {
			required = append(required, alias)
		}
	}
	return objectSchema(props, required), nil
}

// fieldToSchema converts a GraphQL field return type (with optional sub-selection) to an OpenAPI schema.
// Object sub-selections are extracted to named components to avoid deep inline nesting and enable $ref reuse.
func (c *Converter) fieldToSchema(t *ast.Type, sel ast.SelectionSet) (*Schema, error) {
	schema, err := c.fieldBaseSchema(t, sel)
	if err != nil {
		return nil, err
	}
	if !t.NonNull {
		return withNullable(schema), nil
	}
	return schema, nil
}

// fieldBaseSchema resolves a field's type to a schema, ignoring nullability.
func (c *Converter) fieldBaseSchema(t *ast.Type, sel ast.SelectionSet) (*Schema, error) {
	switch {
	case t.Elem != nil:
		items, err := c.fieldToSchema(t.Elem, sel)
		if err != nil {
			return nil, err
		}
		return &Schema{Type: schemaTypeArray, Items: items}, nil
	case len(sel) > 0:
		return c.refToComponent(c.currentOp+t.NamedType, func() (*Schema, error) {
			return c.selectionSetToSchema(sel, t.NamedType)
		})
	default:
		return c.namedTypeToSchema(t.NamedType)
	}
}
