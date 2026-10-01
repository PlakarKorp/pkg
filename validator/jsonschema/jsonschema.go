package jsonschema

import (
	"fmt"
	"io"

	js "github.com/santhosh-tekuri/jsonschema/v6"
)

// schemaURL is only an identifier for the in-memory document; nothing is
// fetched from it.
const schemaURL = "file:///schema.json"

// ReadAndValidate reads and validates an integration JSON Schema
func ReadAndValidate(r io.Reader) error {
	doc, err := js.UnmarshalJSON(r)
	if err != nil {
		return fmt.Errorf("parse schema: %w", err)
	}
	return validate(doc)
}

// validate checks a JSON Schema against the plakar rules.
// It uses its meta-schema $schema or draft 2020-12 if absent.
// External $ref are not allowed
func validate(doc any) error {
	c := js.NewCompiler()
	c.DefaultDraft(js.Draft2020)
	// The default loader reads file:// refs from disk. An empty scheme map
	// refuses every URL; meta-schemas are embedded and still resolve.
	c.UseLoader(js.SchemeURLLoader{})
	if err := c.AddResource(schemaURL, doc); err != nil {
		return fmt.Errorf("add schema: %w", err)
	}
	if _, err := c.Compile(schemaURL); err != nil {
		return fmt.Errorf("compile schema: %w", err)
	}
	return nil
}
