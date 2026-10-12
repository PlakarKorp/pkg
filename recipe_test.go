package pkg

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecipeParse(t *testing.T) {
	const doc = `
name: s3
version: v1.2.3
repository: https://github.com/PlakarKorp/integrations
`
	var r Recipe
	require.NoError(t, r.Parse(strings.NewReader(doc)))
	require.Equal(t, Recipe{
		Name:       "s3",
		Version:    "v1.2.3",
		Repository: "https://github.com/PlakarKorp/integrations",
	}, r)
}

func TestRecipeParseEmpty(t *testing.T) {
	// Recipe.Parse does not wrap the decoder error (unlike Manifest.Parse),
	// so an empty document surfaces the raw io.EOF from the yaml decoder.
	var r Recipe
	require.ErrorIs(t, r.Parse(strings.NewReader("")), io.EOF)
	require.Zero(t, r)
}

func TestRecipeParseInvalidYAML(t *testing.T) {
	var r Recipe
	require.Error(t, r.Parse(strings.NewReader("name: [unterminated")))
}

func TestNewRecipeFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recipe.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: ftp\nversion: v0.1.0\n"), 0644))

	r, err := NewRecipeFromFile(path)
	require.NoError(t, err)
	require.Equal(t, "ftp", r.Name)
	require.Equal(t, "v0.1.0", r.Version)
}

func TestNewRecipeFromFileMissing(t *testing.T) {
	_, err := NewRecipeFromFile(filepath.Join(t.TempDir(), "nope.yaml"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Semver, Subdir and Tag are the monorepo helpers. A recent regression
// (b8097ff "use Semver() not Version otherwise won't catch monorepos")
// touched exactly this logic, so it gets thorough coverage.
func TestRecipeSemverSubdirTag(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		wantSemver string
		wantSubdir string
		wantTag    string
	}{
		{
			name:       "flat repo",
			version:    "v1.2.3",
			wantSemver: "v1.2.3",
			wantSubdir: ".",
			wantTag:    "v1.2.3",
		},
		{
			name:       "monorepo single segment",
			version:    "s3/v1.2.3",
			wantSemver: "v1.2.3",
			wantSubdir: "s3",
			wantTag:    "s3/v1.2.3",
		},
		{
			name:       "monorepo nested segments",
			version:    "backends/s3/v1.2.3",
			wantSemver: "v1.2.3",
			wantSubdir: "backends/s3",
			wantTag:    "backends/s3/v1.2.3",
		},
		{
			name:       "empty version",
			version:    "",
			wantSemver: "",
			wantSubdir: ".",
			wantTag:    "",
		},
		{
			name:       "trailing slash yields empty semver",
			version:    "foo/",
			wantSemver: "",
			wantSubdir: "foo",
			wantTag:    "foo/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Recipe{Name: "x", Version: tt.version}
			require.Equal(t, tt.wantSemver, r.Semver(), "Semver()")
			require.Equal(t, tt.wantSubdir, r.Subdir(), "Subdir()")
			require.Equal(t, tt.wantTag, r.Tag(), "Tag()")
		})
	}
}

func TestRecipePkgName(t *testing.T) {
	// PkgName honors GOOS/GOARCH overrides via the environment.
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "arm64")

	r := &Recipe{Name: "s3", Version: "backends/s3/v1.2.3"}
	require.Equal(t, "s3_v1.2.3_linux_arm64.ptar", r.PkgName())
}

func TestRecipePkgNameDefaultsToRuntime(t *testing.T) {
	t.Setenv("GOOS", "")
	t.Setenv("GOARCH", "")

	r := &Recipe{Name: "fs", Version: "v1.0.0"}
	require.Equal(t, "fs_v1.0.0_"+runtime.GOOS+"_"+runtime.GOARCH+".ptar", r.PkgName())
}
