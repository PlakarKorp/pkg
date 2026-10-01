package jsonschema

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	js "github.com/santhosh-tekuri/jsonschema/v6"
)

var (
	ErrRootNotObject = errors.New("root type must be object")
	ErrUntyped       = errors.New("property must declare a type")
	ErrNested        = errors.New("nested fields are not allowed")
)

// checkFlat checks that schema does not contain nested objects
func checkFlat(root *js.Schema) error {
	if root.Types == nil || !slices.Equal(root.Types.ToStrings(), []string{"object"}) {
		return ErrRootNotObject
	}

	// Properties declared on the root are the configuration keys; they
	// must say what they hold. Properties inside combinators only add
	// constraints, such as a const in an if, and need no type.
	for _, p := range root.Properties {
		if p.Types == nil && p.Ref == nil {
			return fmt.Errorf("%s: %w", pointer(p), ErrUntyped)
		}
	}

	return checkObject(root, visited{}, visited{})
}

// visited used to prevent $ref infinite loop.
type visited map[*js.Schema]bool

func (v visited) first(s *js.Schema) bool {
	if s == nil || v[s] {
		return false
	}
	v[s] = true
	return true
}

// checkObject checks a schema that applies to the configuration object
// itself: its properties must be scalar values.
func checkObject(s *js.Schema, objects, values visited) error {
	if !objects.first(s) {
		return nil
	}
	for _, p := range s.Properties {
		if err := checkValue(p, values); err != nil {
			return err
		}
	}
	for _, p := range s.PatternProperties {
		if err := checkValue(p, values); err != nil {
			return err
		}
	}
	if p, ok := s.AdditionalProperties.(*js.Schema); ok {
		if err := checkValue(p, values); err != nil {
			return err
		}
	}
	for _, sub := range sameInstance(s) {
		if err := checkObject(sub, objects, values); err != nil {
			return err
		}
	}
	return nil
}

// checkValue checks a schema that applies to the value of one property.
func checkValue(s *js.Schema, values visited) error {
	if !values.first(s) {
		return nil
	}
	if s.Types != nil {
		for _, t := range s.Types.ToStrings() {
			if t == "object" || t == "array" {
				return fmt.Errorf("%s: type %s: %w", pointer(s), t, ErrNested)
			}
		}
	}
	if hasStructure(s) {
		return fmt.Errorf("%s: %w", pointer(s), ErrNested)
	}
	for _, sub := range sameInstance(s) {
		if err := checkValue(sub, values); err != nil {
			return err
		}
	}
	return nil
}

// sameInstance returns the subschemas that apply to the same instance as s.
func sameInstance(s *js.Schema) []*js.Schema {
	subs := []*js.Schema{s.Ref, s.Not, s.If, s.Then, s.Else}
	subs = append(subs, s.AllOf...)
	subs = append(subs, s.AnyOf...)
	return append(subs, s.OneOf...)
}

// hasStructure reports whether s describes the members of an object or the
// elements of an array.
func hasStructure(s *js.Schema) bool {
	_, additional := s.AdditionalProperties.(*js.Schema)
	return len(s.Properties) > 0 || len(s.PatternProperties) > 0 || additional ||
		s.Items != nil || s.Items2020 != nil || len(s.PrefixItems) > 0
}

// pointer returns the location of s inside the validated document, as a
// URI fragment: "#" for the root, "#/properties/x" below it.
func pointer(s *js.Schema) string {
	if p, ok := strings.CutPrefix(s.Location, schemaURL); ok {
		return p
	}
	return s.Location
}
