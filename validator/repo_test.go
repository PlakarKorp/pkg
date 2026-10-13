package validator

import (
	"bytes"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const repoManifest = "name: test\ndisplay_name: Test\ndescription: Test.\nconnectors:\n" + importerA

func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(data), 0o644))
	}
	return dir
}

func TestValidateRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		files   map[string]string
		wantErr []string // substrings the error must contain, nil for success
		wantIs  error
	}{
		{
			name:  "valid",
			files: map[string]string{"README.md": "# Test\n", "manifest.yaml": repoManifest, "a.json": goodSchema},
		},
		{
			name:    "missing README",
			files:   map[string]string{"manifest.yaml": repoManifest, "a.json": goodSchema},
			wantErr: []string{"README.md"},
			wantIs:  fs.ErrNotExist,
		},
		{
			name:    "README is a directory",
			files:   map[string]string{"README.md/x": "x", "manifest.yaml": repoManifest, "a.json": goodSchema},
			wantErr: []string{"README.md", "not a regular file"},
		},
		{
			name:    "README not at the root",
			files:   map[string]string{"docs/README.md": "# Test\n", "manifest.yaml": repoManifest, "a.json": goodSchema},
			wantErr: []string{"README.md"},
			wantIs:  fs.ErrNotExist,
		},
		{
			name:    "reports README and schema together",
			files:   map[string]string{"manifest.yaml": repoManifest, "a.json": badSchema},
			wantErr: []string{"README.md", "./a.json", "compile schema"},
		},
		{
			name:    "missing manifest",
			files:   map[string]string{"README.md": "# Test\n"},
			wantErr: []string{"manifest.yaml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := new(Validator).ValidateRepo(writeRepo(t, tt.files))
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			for _, want := range tt.wantErr {
				require.ErrorContains(t, err, want)
			}
			if tt.wantIs != nil {
				require.ErrorIs(t, err, tt.wantIs)
			}
		})
	}
}

func TestValidateRepoLogsReadme(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	v := Validator{Logger: log.New(&out, "", 0)}
	dir := writeRepo(t, map[string]string{"README.md": "# Test\n", "manifest.yaml": repoManifest, "a.json": goodSchema})

	require.NoError(t, v.ValidateRepo(dir))
	require.Contains(t, out.String(), "validating README.md\n")
	require.Contains(t, out.String(), "validating schema ./a.json\n")
}
