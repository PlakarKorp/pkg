package pkg

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRecipeParse(t *testing.T) {
	const doc = `
name: s3
version: v1.2.3
repository: https://github.com/PlakarKorp/integrations
`
	var r Recipe
	if err := r.Parse(strings.NewReader(doc)); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r.Name != "s3" {
		t.Errorf("Name = %q, want s3", r.Name)
	}
	if r.Version != "v1.2.3" {
		t.Errorf("Version = %q, want v1.2.3", r.Version)
	}
	if r.Repository != "https://github.com/PlakarKorp/integrations" {
		t.Errorf("Repository = %q", r.Repository)
	}
}

func TestRecipeParseEmpty(t *testing.T) {
	// Recipe.Parse does not wrap the decoder error (unlike Manifest.Parse),
	// so an empty document surfaces the raw io.EOF from the yaml decoder.
	var r Recipe
	err := r.Parse(strings.NewReader(""))
	if err != io.EOF {
		t.Fatalf("Parse empty = %v, want io.EOF", err)
	}
	if r.Name != "" || r.Version != "" {
		t.Errorf("expected zero recipe, got %+v", r)
	}
}

func TestRecipeParseInvalidYAML(t *testing.T) {
	var r Recipe
	if err := r.Parse(strings.NewReader("name: [unterminated")); err == nil {
		t.Fatal("expected error decoding invalid yaml, got nil")
	}
}

func TestNewRecipeFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recipe.yaml")
	if err := os.WriteFile(path, []byte("name: ftp\nversion: v0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRecipeFromFile(path)
	if err != nil {
		t.Fatalf("NewRecipeFromFile: %v", err)
	}
	if r.Name != "ftp" || r.Version != "v0.1.0" {
		t.Errorf("got %+v", r)
	}
}

func TestNewRecipeFromFileMissing(t *testing.T) {
	if _, err := NewRecipeFromFile(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing file")
	}
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
			if got := r.Semver(); got != tt.wantSemver {
				t.Errorf("Semver() = %q, want %q", got, tt.wantSemver)
			}
			if got := r.Subdir(); got != tt.wantSubdir {
				t.Errorf("Subdir() = %q, want %q", got, tt.wantSubdir)
			}
			if got := r.Tag(); got != tt.wantTag {
				t.Errorf("Tag() = %q, want %q", got, tt.wantTag)
			}
		})
	}
}

func TestRecipePkgName(t *testing.T) {
	// PkgName honors GOOS/GOARCH overrides via the environment.
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "arm64")

	r := &Recipe{Name: "s3", Version: "backends/s3/v1.2.3"}
	got := r.PkgName()
	want := "s3_v1.2.3_linux_arm64.ptar"
	if got != want {
		t.Errorf("PkgName() = %q, want %q", got, want)
	}
}

func TestRecipePkgNameDefaultsToRuntime(t *testing.T) {
	t.Setenv("GOOS", "")
	t.Setenv("GOARCH", "")

	r := &Recipe{Name: "fs", Version: "v1.0.0"}
	got := r.PkgName()
	want := "fs_v1.0.0_" + runtime.GOOS + "_" + runtime.GOARCH + ".ptar"
	if got != want {
		t.Errorf("PkgName() = %q, want %q", got, want)
	}
}
