// Package emitter builds and serializes the OpenAPI v3 document.
package emitter

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/Thomasparsley/peroapi/internal/converter"
)

// Options configure the OpenAPI document metadata.
type Options struct {
	Title   string
	Version string
}

// Document is the top-level OpenAPI v3 object.
type Document struct {
	OpenAPI    string                 `yaml:"openapi"`
	Info       Info                   `yaml:"info"`
	Paths      map[string]*PathObject `yaml:"paths"`
	Components *ComponentsObject      `yaml:"components,omitempty"`
}

// ContentMap is a map of media type strings to MediaTypeObject objects.
type ContentMap map[string]*MediaTypeObject

// ComponentsObject holds reusable OpenAPI schemas.
type ComponentsObject struct {
	Schemas converter.SchemaMap `yaml:"schemas,omitempty"`
}

// Info holds OpenAPI metadata.
type Info struct {
	Title   string `yaml:"title"`
	Version string `yaml:"version"`
}

// PathObject maps HTTP methods to operation objects.
type PathObject struct {
	Get  *OperationObject `yaml:"get,omitempty"`
	Post *OperationObject `yaml:"post,omitempty"`
}

// OperationObject is an OpenAPI operation.
type OperationObject struct {
	OperationID string                     `yaml:"operationId"`
	Parameters  []*converter.Parameter     `yaml:"parameters,omitempty"`
	RequestBody *RequestBodyObject         `yaml:"requestBody,omitempty"`
	Responses   map[string]*ResponseObject `yaml:"responses"`
}

// RequestBodyObject is the OpenAPI request body.
type RequestBodyObject struct {
	Required bool       `yaml:"required"`
	Content  ContentMap `yaml:"content"`
}

// ResponseObject is an OpenAPI response entry.
type ResponseObject struct {
	Description string     `yaml:"description"`
	Content     ContentMap `yaml:"content,omitempty"`
}

// MediaTypeObject holds a schema for a media type.
type MediaTypeObject struct {
	Schema *converter.Schema `yaml:"schema"`
}

// Build constructs the OpenAPI Document from converted path items and collected enum schemas.
func Build(items []*converter.PathItem, components converter.SchemaMap, opts Options) *Document {
	doc := &Document{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:   opts.Title,
			Version: opts.Version,
		},
		Paths: make(map[string]*PathObject),
	}

	if len(components) > 0 {
		doc.Components = &ComponentsObject{Schemas: components}
	}

	for _, item := range items {
		po := doc.Paths[item.Path]
		if po == nil {
			po = &PathObject{}
			doc.Paths[item.Path] = po
		}

		op := buildOperation(item)
		switch item.Method {
		case "POST":
			po.Post = op
		default:
			po.Get = op
		}
	}

	return doc
}

// buildOperation converts a single converted PathItem into an OpenAPI operation object.
func buildOperation(item *converter.PathItem) *OperationObject {
	op := &OperationObject{
		OperationID: item.OperationName,
		Parameters:  item.Parameters,
		Responses:   make(map[string]*ResponseObject),
	}

	if item.RequestBody != nil {
		op.RequestBody = &RequestBodyObject{
			Required: item.RequestBody.Required,
			Content:  toContent(item.RequestBody.Content),
		}
	}

	if item.Response != nil {
		op.Responses["200"] = &ResponseObject{
			Description: item.Response.Description,
			Content:     toContent(item.Response.Content),
		}
	}

	return op
}

// toContent copies a converter content map into the emitter's representation.
func toContent(src converter.ContentMap) ContentMap {
	dst := make(ContentMap, len(src))
	for mt, mto := range src {
		dst[mt] = &MediaTypeObject{Schema: mto.Schema}
	}
	return dst
}

// Write serializes the document to YAML. If path is empty it writes to stdout.
func Write(doc *Document, path string) error {
	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling OpenAPI document: %w", err)
	}

	if path == "" {
		_, err = os.Stdout.Write(data)
		return err
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing output file %q: %w", path, err)
	}
	return nil
}
