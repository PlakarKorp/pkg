package manifest

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/PlakarKorp/pkg"
	"go.yaml.in/yaml/v3"
)

var (
	ErrTrailingData = errors.New("trailing data after manifest")
	ErrNoConnectors = errors.New("no connectors")
)

// ReadAndValidate reads and validates an integration manifest.yaml
// It is deliberately strict (forces known fields) since it is meant for validation in development.
func ReadAndValidate(r io.Reader) (*pkg.Manifest, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	var m pkg.Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, ErrTrailingData
	}

	if err := validate(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Schemas returns a JSON schema path for each connector "validator" field.
// If several connectors share a schema, later occurrences are skipped
func Schemas(m *pkg.Manifest) []string {
	// using a map for dedup, a list for ordering
	seen := make(map[string]struct{})
	var paths []string
	for _, c := range m.Connectors {
		if _, ok := seen[c.Validator]; !ok {
			seen[c.Validator] = struct{}{}
			paths = append(paths, c.Validator)
		}
	}
	return paths
}

// validate validates a manifest against plakar's rules
func validate(m *pkg.Manifest) error {
	type field struct {
		name  string
		value string
	}

	for _, f := range []field{
		{"name", m.Name},
		{"display_name", m.DisplayName},
		{"description", m.Description},
	} {
		if f.value == "" {
			return fmt.Errorf("%s is required", f.name)
		}
	}

	if len(m.Connectors) == 0 {
		return ErrNoConnectors
	}
	for i, c := range m.Connectors {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("connector #%d: %w", i, err)
		}
		if len(c.Protocols) == 0 {
			return fmt.Errorf("connector #%d: protocols is required", i)
		}
		if c.Validator == "" {
			return fmt.Errorf("connector #%d: validator is required", i)
		}
		if !filepath.IsLocal(c.Validator) {
			return fmt.Errorf("connector #%d: validator %q escapes the integration directory", i, c.Validator)
		}
	}
	return nil
}
