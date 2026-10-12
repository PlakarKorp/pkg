package pkg

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntegrationHasConnectorType(t *testing.T) {
	i := &Integration{
		Connectors: []Connector{
			{Type: "importer"},
			{Type: "storage"},
		},
	}
	require.True(t, i.HasConnectorType("importer"))
	require.True(t, i.HasConnectorType("storage"))
	require.False(t, i.HasConnectorType("exporter"))
}

func TestIntegrationHasConnectorTypeEmpty(t *testing.T) {
	var i Integration
	require.False(t, i.HasConnectorType("importer"))
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
	require.NoError(t, json.Unmarshal([]byte(doc), &idx))
	require.Equal(t, "v1.0.0", idx.Version)
	require.Len(t, idx.Integrations, 1)

	in := idx.Integrations[0]
	require.Equal(t, "s3", in.Name)
	require.Equal(t, "Amazon S3", in.DisplayName)
	require.Equal(t, "v1.1.0", in.API)
	require.Equal(t, "v1.2.3", in.Version)
	require.True(t, in.HasConnectorType("storage"))
}
