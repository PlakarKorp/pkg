package manifest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// wantPrefix starts the first line of every invalid gold file and names the
// error it must fail with, so a file cannot pass by failing for another reason.
const wantPrefix = "# error: "

// wantError returns the error named on the first line of an invalid gold file.
func wantError(t *testing.T, data []byte) string {
	t.Helper()
	first, _, _ := strings.Cut(string(data), "\n")
	want, ok := strings.CutPrefix(strings.TrimSpace(first), wantPrefix)
	require.True(t, ok, "first line must start with %q", wantPrefix)
	return want
}

func TestValidate(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join("testdata", dir, "*.yaml"))
		require.NoError(t, err)
		require.NotEmpty(t, files, "no gold files in testdata/%s", dir)

		for _, path := range files {
			t.Run(dir+"/"+filepath.Base(path), func(t *testing.T) {
				t.Parallel()
				data, err := os.ReadFile(path)
				require.NoError(t, err)

				_, err = ReadAndValidate(bytes.NewReader(data))
				if dir == "valid" {
					require.NoError(t, err)
					return
				}
				require.ErrorContains(t, err, wantError(t, data))
			})
		}
	}
}

func TestSchemas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		connectors string
		want       []string
	}{
		{
			"one per connector",
			`[{type: importer, executable: x, protocols: [x], validator: ./importer/schema.json},
			  {type: exporter, executable: x, protocols: [x], validator: ./exporter/schema.json}]`,
			[]string{"./importer/schema.json", "./exporter/schema.json"},
		},
		{
			"drops duplicates, keeps connector order",
			`[{type: importer, executable: x, protocols: [x], validator: b.json},
			  {type: exporter, executable: x, protocols: [x], validator: a.json},
			  {type: storage, executable: x, protocols: [x], validator: b.json}]`,
			[]string{"b.json", "a.json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := "name: x\ndisplay_name: X\ndescription: x\nconnectors: " + tt.connectors + "\n"
			m, err := ReadAndValidate(strings.NewReader(doc))
			require.NoError(t, err)
			require.Equal(t, tt.want, Schemas(m))
		})
	}
}
