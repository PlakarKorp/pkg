package pkg

import (
	"encoding/json"
	"testing"
)

func TestIntegrationHasConnectorType(t *testing.T) {
	i := &Integration{
		Connectors: []Connector{
			{Type: "importer"},
			{Type: "storage"},
		},
	}
	if !i.HasConnectorType("importer") {
		t.Error("HasConnectorType(importer) = false, want true")
	}
	if !i.HasConnectorType("storage") {
		t.Error("HasConnectorType(storage) = false, want true")
	}
	if i.HasConnectorType("exporter") {
		t.Error("HasConnectorType(exporter) = true, want false")
	}
}

func TestIntegrationHasConnectorTypeEmpty(t *testing.T) {
	var i Integration
	if i.HasConnectorType("importer") {
		t.Error("HasConnectorType on empty integration = true, want false")
	}
}

// The index JSON is the wire format the manager decodes in Query; make
// sure the struct tags line up with the documented field names.
func TestIntegrationIndexJSONDecode(t *testing.T) {
	const doc = `{
		"version": "v1.0.0",
		"timestamp": "2026-01-01T00:00:00Z",
		"integrations": [
			{
				"name": "s3",
				"display_name": "Amazon S3",
				"edition": "community",
				"api": "v1.1.0",
				"version": "v1.2.3",
				"tags": ["cloud"],
				"connectors": [{"type": "storage", "class": "object-storage", "subclass": "s3"}]
			}
		]
	}`

	var idx IntegrationIndex
	if err := json.Unmarshal([]byte(doc), &idx); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if idx.Version != "v1.0.0" {
		t.Errorf("Version = %q", idx.Version)
	}
	if len(idx.Integrations) != 1 {
		t.Fatalf("len(Integrations) = %d, want 1", len(idx.Integrations))
	}
	in := idx.Integrations[0]
	if in.Name != "s3" || in.DisplayName != "Amazon S3" {
		t.Errorf("decoded integration = %+v", in)
	}
	if in.API != "v1.1.0" || in.Version != "v1.2.3" {
		t.Errorf("api/version = %q/%q", in.API, in.Version)
	}
	if !in.HasConnectorType("storage") {
		t.Error("expected storage connector")
	}
}
