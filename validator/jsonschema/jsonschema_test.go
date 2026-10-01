package jsonschema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join("testdata", dir, "*.json"))
		require.NoError(t, err)
		require.NotEmpty(t, files, "no gold files in testdata/%s", dir)

		for _, path := range files {
			t.Run(dir+"/"+filepath.Base(path), func(t *testing.T) {
				t.Parallel()
				f, err := os.Open(path)
				require.NoError(t, err)
				t.Cleanup(func() { _ = f.Close() })

				err = ReadAndValidate(f)
				if dir == "valid" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}

func TestValidateRefusesFileRef(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "target.json")
	require.NoError(t, os.WriteFile(target, []byte(`{"type": "string"}`), 0o600))

	doc := `{"properties": {"location": {"$ref": "file://` + filepath.ToSlash(target) + `"}}}`
	require.Error(t, ReadAndValidate(strings.NewReader(doc)))
}

func TestValidateFlat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  string
		want error
	}{
		{"scalars", `{"type": "object", "properties": {
			"s": {"type": "string"}, "i": {"type": "integer"},
			"n": {"type": "number"}, "b": {"type": "boolean"}}}`, nil},
		{"untyped property in a combinator", `{"type": "object",
			"properties": {"mode": {"type": "string"}},
			"if": {"properties": {"mode": {"const": "a"}}}, "then": {"required": ["mode"]}}`, nil},
		{"scalar through ref", `{"type": "object", "properties": {"s": {"$ref": "#/$defs/s"}},
			"$defs": {"s": {"type": "string"}}}`, nil},
		{"closed object", `{"type": "object", "additionalProperties": false}`, nil},
		{"scalar additional properties", `{"type": "object", "additionalProperties": {"type": "string"}}`, nil},

		{"no root type", `{"properties": {"s": {"type": "string"}}}`, ErrRootNotObject},
		{"array root", `{"type": "array"}`, ErrRootNotObject},
		{"nullable root", `{"type": ["object", "null"]}`, ErrRootNotObject},

		{"untyped property", `{"type": "object", "properties": {"s": {"description": "x"}}}`, ErrUntyped},

		{"object property", `{"type": "object", "properties": {"o": {"type": "object"}}}`, ErrNested},
		{"array property", `{"type": "object", "properties": {"a": {"type": "array"}}}`, ErrNested},
		{"object among types", `{"type": "object", "properties": {"o": {"type": ["string", "object"]}}}`, ErrNested},
		{"items on a scalar", `{"type": "object", "properties": {"a": {"type": "string", "items": {"type": "string"}}}}`, ErrNested},
		{"object through ref", `{"type": "object", "properties": {"o": {"$ref": "#/$defs/o"}},
			"$defs": {"o": {"type": "object"}}}`, ErrNested},
		{"object in a property allOf", `{"type": "object", "properties": {"o": {"type": "string", "allOf": [{"type": "object"}]}}}`, ErrNested},
		{"object in a root allOf", `{"type": "object", "allOf": [{"properties": {"o": {"type": "object"}}}]}`, ErrNested},
		{"object pattern property", `{"type": "object", "patternProperties": {"^x": {"type": "object"}}}`, ErrNested},
		{"object additional properties", `{"type": "object", "additionalProperties": {"type": "array"}}`, ErrNested},
		{"recursive ref", `{"type": "object", "properties": {"self": {"$ref": "#"}}}`, ErrNested},

		// Every keyword describing the same instance is followed, in both modes.
		{"object in a property anyOf", `{"type": "object", "properties": {"o": {"type": "string", "anyOf": [{"type": "object"}]}}}`, ErrNested},
		{"object in a property oneOf", `{"type": "object", "properties": {"o": {"type": "string", "oneOf": [{"type": "array"}]}}}`, ErrNested},
		{"object in a property not", `{"type": "object", "properties": {"o": {"type": "string", "not": {"properties": {"x": {}}}}}}`, ErrNested},
		{"object in a property then", `{"type": "object", "properties": {"o": {"type": "string",
			"if": {"minLength": 1}, "then": {"type": "object"}}}}`, ErrNested},
		{"object in a root anyOf", `{"type": "object", "anyOf": [{"properties": {"o": {"type": "object"}}}]}`, ErrNested},
		{"object in a root oneOf", `{"type": "object", "oneOf": [{"properties": {"o": {"type": "array"}}}]}`, ErrNested},
		{"object in a root if", `{"type": "object", "if": {"properties": {"o": {"type": "object"}}}, "then": {}}`, ErrNested},
		{"object in a root else", `{"type": "object", "properties": {"m": {"type": "string"}},
			"if": {"properties": {"m": {"const": "a"}}}, "else": {"properties": {"o": {"type": "object"}}}}`, ErrNested},
		{"object behind a root ref", `{"type": "object", "$ref": "#/$defs/base",
			"$defs": {"base": {"properties": {"o": {"type": "object"}}}}}`, ErrNested},

		// Structure keywords on a scalar still declare nested fields.
		{"properties on a scalar", `{"type": "object", "properties": {"s": {"type": "string", "properties": {"x": {"type": "string"}}}}}`, ErrNested},
		{"pattern properties on a scalar", `{"type": "object", "properties": {"s": {"type": "string", "patternProperties": {"^x": {}}}}}`, ErrNested},
		{"additional properties on a scalar", `{"type": "object", "properties": {"s": {"type": "string", "additionalProperties": {"type": "string"}}}}`, ErrNested},
		{"prefix items on a scalar", `{"type": "object", "properties": {"s": {"type": "string", "prefixItems": [{"type": "string"}]}}}`, ErrNested},
		{"draft-07 items on a scalar", `{"$schema": "http://json-schema.org/draft-07/schema#", "type": "object",
			"properties": {"s": {"type": "string", "items": {"type": "string"}}}}`, ErrNested},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ReadAndValidate(strings.NewReader(tt.doc))
			if tt.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.want)
			}
		})
	}
}

func TestValidateFlatNamesTheField(t *testing.T) {
	t.Parallel()

	doc := `{"type": "object", "properties": {"opts": {"type": "object"}}}`
	require.ErrorContains(t, ReadAndValidate(strings.NewReader(doc)), "#/properties/opts")
}
