package jsonschema

import (
	"errors"
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
// The schema cannot contain nested objects
func validate(doc any) error {
	c := js.NewCompiler()
	c.DefaultDraft(js.Draft2020)
	// The default loader reads file:// refs from disk. An empty scheme map
	// refuses every URL; meta-schemas are embedded and still resolve.
	c.UseLoader(js.SchemeURLLoader{})
	if err := c.AddResource(schemaURL, doc); err != nil {
		return fmt.Errorf("add schema: %w", err)
	}
	sch, err := c.Compile(schemaURL)
	if err != nil {
		return fmt.Errorf("compile schema: %w", err)
	}
	if err := checkFlat(sch); err != nil {
		return err
	}
	return checkEnums(sch)
}

// ErrEnumNotString reports a root property whose enum holds a value that is
// not a string.
var ErrEnumNotString = errors.New("enum values must be strings")

// checkEnums requires string enum values on the root's properties: plakman
// builds its forms from them and rejects any other type.
func checkEnums(root *js.Schema) error {
	for _, p := range root.Properties {
		if p.Enum == nil {
			continue
		}
		for _, v := range p.Enum.Values {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("%s: %v: %w", pointer(p), v, ErrEnumNotString)
			}
		}
	}
	return nil
}
