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
