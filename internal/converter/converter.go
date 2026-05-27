// Package converter transforms parsed GraphQL operations into OpenAPI path definitions.
package converter

import (
	"fmt"
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

// Components returns the enum schemas collected during Convert, keyed by GraphQL enum name.
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
		gqlOp := op.Doc.Operations[0]
		method := resolveMethod(gqlOp.Operation, methodConvention)
		path := strings.TrimRight(basePath, "/") + "/" + op.Name

		c.currentOp = op.Name

		item := &PathItem{
			OperationName: op.Name,
			Path:          path,
			Method:        method,
		}

		if method == httpMethodGET {
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

		items = append(items, item)
	}
	return items, nil
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

	variablesObj := &Schema{
		Type:       schemaTypeObject,
		Properties: props,
	}
	if len(required) > 0 {
		variablesObj.Required = required
	}

	obj := &Schema{
		Type: schemaTypeObject,
		Properties: SchemaMap{
			variablesKey: variablesObj,
		},
		Required: []string{variablesKey},
	}

	return &RequestBody{
		Required: true,
		Content: ContentMap{
			mediaTypeJSON: {Schema: obj},
		},
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

// namedTypeToSchema maps a named GraphQL type to its OpenAPI schema equivalent.
// Built-in scalars are converted directly; enums and input objects are registered in
// c.components and returned as $ref pointers so each type is only defined once.
// Unknown scalars fall back to a plain string schema with an x-graphql-scalar extension.
func (c *Converter) namedTypeToSchema(name string) (*Schema, error) {
	switch name {
	case "String":
		return &Schema{Type: schemaTypeString}, nil
	case "Int":
		return &Schema{Type: schemaTypeInteger, Format: schemaFormatInt32}, nil
	case "Float":
		return &Schema{Type: schemaTypeNumber, Format: schemaFormatFloat}, nil
	case "Boolean":
		return &Schema{Type: schemaTypeBoolean}, nil
	case "ID":
		return &Schema{Type: schemaTypeString}, nil
	case "UnsignedShort":
		max := int64(65535)
		return &Schema{Type: schemaTypeInteger, Format: schemaFormatInt32, Minimum: new(int64), Maximum: &max}, nil
	case "Byte":
		max := int64(255)
		return &Schema{Type: schemaTypeInteger, Format: schemaFormatInt32, Minimum: new(int64), Maximum: &max}, nil
	case "LocalDate":
		return &Schema{Type: schemaTypeString, Format: schemaFormatDate}, nil
	}

	typeDef, ok := c.schema.Types[name]
	if !ok {
		// Unknown — treat as string with extension
		fmt.Printf("warning: unknown scalar %q, treating as string\n", name)
		return &Schema{Type: schemaTypeString, XGraphQLScalar: name}, nil
	}

	switch typeDef.Kind {
	case ast.Scalar:
		return &Schema{Type: schemaTypeString, XGraphQLScalar: name}, nil

	case ast.Enum:
		if _, registered := c.components[name]; !registered {
			var values []string
			for _, v := range typeDef.EnumValues {
				values = append(values, v.Name)
			}
			c.components[name] = &Schema{Type: schemaTypeString, Enum: values}
		}
		return &Schema{Ref: componentsSchemaPrefix + name}, nil

	case ast.InputObject:
		if _, registered := c.components[name]; !registered {
			c.components[name] = nil // sentinel: prevents infinite recursion on cyclic input types
			schema, err := c.inputObjectToSchema(typeDef)
			if err != nil {
				return nil, err
			}
			c.components[name] = schema
		}
		return &Schema{Ref: componentsSchemaPrefix + name}, nil

	default:
		return &Schema{Type: schemaTypeString}, nil
	}
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

	obj := &Schema{
		Type:       schemaTypeObject,
		Properties: props,
	}
	if len(required) > 0 {
		obj.Required = required
	}
	return obj, nil
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
			"data": dataSchema,
			"errors": {
				Type: schemaTypeArray,
				Items: &Schema{
					Type: schemaTypeObject,
					Properties: SchemaMap{
						"message":   {Type: schemaTypeString},
						"locations": {Type: schemaTypeArray, Items: &Schema{Type: schemaTypeObject}},
						"path":      {Type: schemaTypeArray, Items: &Schema{Type: schemaTypeString}},
					},
				},
			},
		},
	}

	return &Response{
		Description: successfulResponse,
		Content: map[string]*MediaTypeObject{
			mediaTypeJSON: {Schema: envelope},
		},
	}, nil
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
				typeName := n.Definition.TypeCondition
				if typeName == "" {
					typeName = parentTypeName
				}
				isNarrowing := typeName != parentTypeName
				result = append(result, collectTypedFields(n.Definition.SelectionSet, typeName, optional || isNarrowing)...)
			}
		case *ast.InlineFragment:
			typeName := n.TypeCondition
			if typeName == "" {
				typeName = parentTypeName
			}
			isNarrowing := typeName != parentTypeName
			result = append(result, collectTypedFields(n.SelectionSet, typeName, optional || isNarrowing)...)
		}
	}
	return result
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

// buildConcreteTypeSchema builds a flat merged schema for a specific concrete type
// from a selection set that targets an abstract parent type. Fields from other concrete
// types' inline fragments are excluded; fields from interface fragments are included
// only if the concrete type implements that interface.
func (c *Converter) buildConcreteTypeSchema(sel ast.SelectionSet, parentTypeName, concreteTypeName string) (*Schema, error) {
	concreteTypes := c.collectConcreteTypes(sel, parentTypeName)
	concreteTypeSet := make(map[string]bool, len(concreteTypes))
	for _, ct := range concreteTypes {
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

		switch {
		case tf.typeName == parentTypeName:
			// Base field — always included.
		case tf.typeName == concreteTypeName:
			// This concrete type's own fragment field — included.
		case concreteTypeSet[tf.typeName]:
			// A different concrete type's fragment — excluded.
			continue
		default:
			// Interface fragment — include only if this concrete type implements it.
			if !c.typeImplements(concreteTypeName, tf.typeName) {
				continue
			}
		}

		seen[alias] = true

		typeDef, ok := c.schema.Types[tf.typeName]
		if !ok {
			continue
		}
		fieldDef := typeDef.Fields.ForName(tf.field.Name)
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

	obj := &Schema{Type: schemaTypeObject, Properties: props}
	if len(required) > 0 {
		obj.Required = required
	}
	return obj, nil
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

	// When concrete type inline fragments are present, emit a oneOf discriminated union —
	// one variant per concrete type — instead of a flat merged schema.
	if concreteTypes := c.collectConcreteTypes(sel, parentTypeName); len(concreteTypes) > 0 {
		oneOf := make([]*Schema, 0, len(concreteTypes))
		for _, ct := range concreteTypes {
			compName := c.currentOp + ct
			if _, exists := c.components[compName]; !exists {
				c.components[compName] = nil // sentinel: prevents cycles
				s, err := c.buildConcreteTypeSchema(sel, parentTypeName, ct)
				if err != nil {
					return nil, err
				}
				c.components[compName] = s
			}
			oneOf = append(oneOf, &Schema{Ref: componentsSchemaPrefix + compName})
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

	// No concrete type discrimination — flat merged schema.
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

		typeDef, ok := c.schema.Types[tf.typeName]
		if !ok {
			continue
		}
		fieldDef := typeDef.Fields.ForName(tf.field.Name)
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

	obj := &Schema{Type: schemaTypeObject, Properties: props}
	if len(required) > 0 {
		obj.Required = required
	}
	return obj, nil
}

// fieldToSchema converts a GraphQL field return type (with optional sub-selection) to an OpenAPI schema.
// Object sub-selections are extracted to named components to avoid deep inline nesting and enable $ref reuse.
func (c *Converter) fieldToSchema(t *ast.Type, sel ast.SelectionSet) (*Schema, error) {
	var schema *Schema
	if t.Elem != nil {
		items, err := c.fieldToSchema(t.Elem, sel)
		if err != nil {
			return nil, err
		}
		schema = &Schema{Type: schemaTypeArray, Items: items}
	} else if len(sel) > 0 {
		compName := c.currentOp + t.NamedType
		if _, exists := c.components[compName]; !exists {
			c.components[compName] = nil // sentinel: prevents cycles
			s, err := c.selectionSetToSchema(sel, t.NamedType)
			if err != nil {
				return nil, err
			}
			c.components[compName] = s
		}
		schema = &Schema{Ref: componentsSchemaPrefix + compName}
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
