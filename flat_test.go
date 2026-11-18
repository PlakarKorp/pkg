package pkg

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/PlakarKorp/integration-sdk"
	"github.com/PlakarKorp/kloset/kcontext"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func newTestFlatBackend(t *testing.T, opts *FlatBackendOptions) (*FlatBackend, string, string) {
	t.Helper()
	if opts == nil {
		opts = &FlatBackendOptions{}
	}
	root := t.TempDir()
	pkgdir := filepath.Join(root, "pkgs")
	cachedir := filepath.Join(root, "cache")
	kctx := kcontext.NewKContext()

	be, err := NewFlatBackend(kctx, pkgdir, cachedir, opts)
	require.NoError(t, err)
	return be, pkgdir, cachedir
}

func TestNewFlatBackendCreatesDirs(t *testing.T) {
	_, pkgdir, cachedir := newTestFlatBackend(t, nil)
	require.DirExists(t, pkgdir)
	require.DirExists(t, cachedir)
}

// touch creates an empty file with the given name inside pkgdir.
func touch(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0644))
}

func TestFlatBackendList(t *testing.T) {
	be, pkgdir, _ := newTestFlatBackend(t, nil)

	os := runtime.GOOS
	arch := runtime.GOARCH
	touch(t, pkgdir, "s3_v1.0.0_"+os+"_"+arch+".ptar")
	touch(t, pkgdir, "s3_v2.0.0_"+os+"_"+arch+".ptar")
	touch(t, pkgdir, "ftp_v0.1.0_"+os+"_"+arch+".ptar")
	touch(t, pkgdir, ".hidden_v1.0.0_"+os+"_"+arch+".ptar") // skipped: hidden
	touch(t, pkgdir, "garbage.txt")                         // skipped: not parseable

	// listing everything
	var got []string
	for p, err := range be.List("") {
		require.NoError(t, err)
		got = append(got, p.Name+"_"+p.Version)
	}
	require.ElementsMatch(t, []string{"ftp_v0.1.0", "s3_v1.0.0", "s3_v2.0.0"}, got)

	// filtered by name
	var s3 []string
	for p, err := range be.List("s3") {
		require.NoError(t, err)
		require.Equal(t, "s3", p.Name)
		s3 = append(s3, p.Version)
	}
	require.ElementsMatch(t, []string{"v1.0.0", "v2.0.0"}, s3)
}

func TestFlatBackendListEarlyStop(t *testing.T) {
	be, pkgdir, _ := newTestFlatBackend(t, nil)
	os := runtime.GOOS
	arch := runtime.GOARCH
	for _, n := range []string{"a", "b", "c", "d"} {
		touch(t, pkgdir, n+"_v1.0.0_"+os+"_"+arch+".ptar")
	}

	count := 0
	for _, err := range be.List("") {
		require.NoError(t, err)
		count++
		break // consumer stops early; iterator must respect the false yield
	}
	require.Equal(t, 1, count)
}

func TestFlatBackendListMissingDir(t *testing.T) {
	root := t.TempDir()
	kctx := kcontext.NewKContext()
	be, err := NewFlatBackend(kctx, filepath.Join(root, "pkgs"), filepath.Join(root, "cache"), &FlatBackendOptions{})
	require.NoError(t, err)
	// Remove the pkgdir out from under the backend.
	require.NoError(t, os.RemoveAll(be.pkgdir))

	var gotErr error
	for _, err := range be.List("") {
		if err != nil {
			gotErr = err
			break
		}
	}
	require.Error(t, gotErr)
}

// loadmanifest validates the executable path stays within the manifest's
// own directory. A path that escapes via ".." must be rejected.
func TestLoadManifestRejectsPathTraversal(t *testing.T) {
	be, _, cachedir := newTestFlatBackend(t, nil)

	mdir := filepath.Join(cachedir, "evil")
	require.NoError(t, os.MkdirAll(mdir, 0755))
	const manifest = `
name: evil
connectors:
  - type: storage
    executable: ../../../etc/passwd
    protocols: [evil]
`
	mpath := filepath.Join(mdir, "manifest.yaml")
	require.NoError(t, os.WriteFile(mpath, []byte(manifest), 0644))

	_, err := be.loadmanifest(mpath, pkgOf(t, "evil"))
	require.ErrorContains(t, err, "escapes the integration directory")
}

func TestLoadManifestAcceptsValidExecutable(t *testing.T) {
	be, _, cachedir := newTestFlatBackend(t, nil)

	mdir := filepath.Join(cachedir, "good")
	require.NoError(t, os.MkdirAll(mdir, 0755))
	const manifest = `
name: good
connectors:
  - type: storage
    class: object-storage
    subclass: s3
    executable: bin/s3-storage
    protocols: [s3]
    location_flags:
      - localfs
`
	mpath := filepath.Join(mdir, "manifest.yaml")
	require.NoError(t, os.WriteFile(mpath, []byte(manifest), 0644))

	m, err := be.loadmanifest(mpath, pkgOf(t, "good"))
	require.NoError(t, err)
	require.Equal(t, "good", m.Name)
}

func TestLoadManifestRejectsBadFlags(t *testing.T) {
	be, _, cachedir := newTestFlatBackend(t, nil)

	mdir := filepath.Join(cachedir, "badflag")
	require.NoError(t, os.MkdirAll(mdir, 0755))
	const manifest = `
name: badflag
connectors:
  - type: storage
    executable: tool
    location_flags:
      - notarealflag
`
	mpath := filepath.Join(mdir, "manifest.yaml")
	require.NoError(t, os.WriteFile(mpath, []byte(manifest), 0644))

	_, err := be.loadmanifest(mpath, pkgOf(t, "badflag"))
	require.ErrorContains(t, err, "notarealflag")
}

func TestFlatBackendUnloadRemovesFiles(t *testing.T) {
	be, pkgdir, cachedir := newTestFlatBackend(t, nil)

	pkg := &Package{
		Name:            "s3",
		Version:         "v1.0.0",
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}

	// Stage the ptar file and an extracted directory as Load would.
	ptarPath := filepath.Join(pkgdir, pkg.Filename())
	require.NoError(t, os.WriteFile(ptarPath, []byte("data"), 0644))
	extracted := filepath.Join(cachedir, "s3_v1.0.0_"+runtime.GOOS+"_"+runtime.GOARCH)
	require.NoError(t, os.MkdirAll(extracted, 0755))

	// No unloadhook, so Unload should not need a manifest.
	require.NoError(t, be.Unload(pkg))
	require.NoFileExists(t, ptarPath)
	require.NoDirExists(t, extracted)
}

func TestFlatBackendUnloadHook(t *testing.T) {
	var gotPkg *Package
	be, pkgdir, cachedir := newTestFlatBackend(t, &FlatBackendOptions{
		UnloadHook: func(m *Manifest, p *Package) {
			gotPkg = p
		},
	})

	pkg := &Package{
		Name:            "s3",
		Version:         "v1.0.0",
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}
	require.NoError(t, os.WriteFile(filepath.Join(pkgdir, pkg.Filename()), []byte("data"), 0644))
	extracted := filepath.Join(cachedir, "s3_v1.0.0_"+runtime.GOOS+"_"+runtime.GOARCH)
	require.NoError(t, os.MkdirAll(extracted, 0755))

	manifest, err := yaml.Marshal(&Manifest{
		Name: "s3",
		Connectors: []ManifestConnector{{
			Protocols:  []string{"s3"},
			Type:       integration.ConnectorTypeImporter,
			Executable: "true",
			Class:      integration.ResourceClassObjectStorage,
			SubClass:   integration.ResourceSubClassS3,
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(extracted, "manifest.yaml"), manifest, 0644))

	require.NoError(t, be.Unload(pkg))
	require.NotNil(t, gotPkg, "unload hook was not called")
	require.Equal(t, "s3", gotPkg.Name)
}

func TestFlatBackendUnloadHookManifestMissing(t *testing.T) {
	be, pkgdir, _ := newTestFlatBackend(t, &FlatBackendOptions{
		UnloadHook: func(m *Manifest, p *Package) {},
	})
	pkg := &Package{
		Name:            "s3",
		Version:         "v1.0.0",
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}
	require.NoError(t, os.WriteFile(filepath.Join(pkgdir, pkg.Filename()), []byte("data"), 0644))

	// With an unloadhook but no extracted manifest, Unload should report
	// the missing manifest rather than silently succeed.
	require.ErrorIs(t, be.Unload(pkg), os.ErrNotExist)
}

// unload tolerates an already-missing extracted directory.
func TestFlatBackendUnloadIdempotent(t *testing.T) {
	be, pkgdir, _ := newTestFlatBackend(t, nil)
	ptarPath := filepath.Join(pkgdir, "s3_v1.0.0_"+runtime.GOOS+"_"+runtime.GOARCH+".ptar")
	require.NoError(t, os.WriteFile(ptarPath, []byte("data"), 0644))
	require.NoError(t, be.unload(ptarPath, filepath.Join(t.TempDir(), "does-not-exist")))
}

// stageExtracted installs pkg with an already extracted manifest, which
// makes LoadAll skip the ptar.
func stageExtracted(t *testing.T, pkgdir, cachedir string, pkg *Package, manifest string) {
	t.Helper()
	touch(t, pkgdir, pkg.Filename())
	extracted := filepath.Join(cachedir, strings.TrimSuffix(pkg.Filename(), ".ptar"))
	require.NoError(t, os.MkdirAll(extracted, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(extracted, "manifest.yaml"), []byte(manifest), 0644))
}

func TestFlatBackendLoadAllManifestName(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		wantErr  error
	}{
		{"matching", "s3", nil},
		{"other package", "gcs", ErrManifestMismatch},
		{"different case", "S3", ErrManifestMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var loaded []string
			be, pkgdir, cachedir := newTestFlatBackend(t, &FlatBackendOptions{
				LoadHook: func(m *Manifest, p *Package, dir string) {
					loaded = append(loaded, p.Name)
				},
			})
			stageExtracted(t, pkgdir, cachedir, pkgOf(t, "s3"),
				"name: "+tt.manifest+"\nconnectors: [{type: importer, executable: tool, protocols: [s3]}]\n")

			err := be.LoadAll()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Empty(t, loaded, "load hook called")
				return
			}
			require.NoError(t, err)
			require.Equal(t, []string{"s3"}, loaded)
		})
	}
}

func TestFlatBackendLoadAllConnectorKind(t *testing.T) {
	const (
		native    = "{type: importer, executable: tool, protocols: [s3]}"
		container = "{type: exporter, image_id: " + testImageID + ", protocols: [s3]}"
	)
	tests := []struct {
		name       string
		os         string
		connectors string
		wantErr    error
	}{
		{"native package, executable", runtime.GOOS, "[" + native + "]", nil},
		{"container package, image", OSContainer, "[" + container + "]", nil},
		{"container package, executable", OSContainer, "[" + native + "]", ErrManifestMismatch},
		{"native package, image", runtime.GOOS, "[" + container + "]", ErrManifestMismatch},
		{"container package, mixed", OSContainer, "[" + container + ", " + native + "]", ErrManifestMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be, pkgdir, cachedir := newTestFlatBackend(t, nil)
			pkg := pkgOf(t, "s3")
			pkg.OperatingSystem = tt.os
			stageExtracted(t, pkgdir, cachedir, pkg, "name: s3\nconnectors: "+tt.connectors+"\n")

			err := be.LoadAll()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// List removes leftover fetch-plugin-* files from pkgdir, and only from there.
func TestFlatBackendListRemovesStaleFetches(t *testing.T) {
	be, pkgdir, _ := newTestFlatBackend(t, nil)
	touch(t, pkgdir, "fetch-plugin-1234")
	touch(t, pkgdir, pkgOf(t, "s3").Filename())

	cwd := t.TempDir()
	t.Chdir(cwd)
	touch(t, cwd, "fetch-plugin-1234")

	var names []string
	for pkg, err := range be.List("") {
		require.NoError(t, err)
		names = append(names, pkg.Name)
	}
	require.Equal(t, []string{"s3"}, names)

	require.NoFileExists(t, filepath.Join(pkgdir, "fetch-plugin-1234"), "stale fetch in pkgdir not removed")
	require.FileExists(t, filepath.Join(cwd, "fetch-plugin-1234"), "file in the working directory touched")
}

// Manifest must return the manifest of an already-extracted package along
// with the directory it lives in, without re-extracting the ptar.
func TestFlatBackendManifest(t *testing.T) {
	be, pkgdir, cachedir := newTestFlatBackend(t, nil)

	pkg := testPkg("s3")
	if err := os.WriteFile(filepath.Join(pkgdir, pkg.Filename()), []byte("artifact"), 0644); err != nil {
		t.Fatal(err)
	}

	extracted := filepath.Join(cachedir, strings.TrimSuffix(pkg.Filename(), ".ptar"))
	if err := os.MkdirAll(extracted, 0755); err != nil {
		t.Fatal(err)
	}

	manifest, err := yaml.Marshal(&Manifest{
		Name:        "s3",
		DisplayName: "S3",
		Description: "S3 storage connector",
		Connectors: []ManifestConnector{{
			Type:       integration.ConnectorTypeImporter,
			Executable: "true",
			Protocols:  []string{"s3"},
			Class:      integration.ResourceClassObjectStorage,
			SubClass:   integration.ResourceSubClassS3,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extracted, "manifest.yaml"), manifest, 0644); err != nil {
		t.Fatal(err)
	}

	m, dir, err := be.Manifest(pkg)
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if dir != extracted {
		t.Errorf("dir = %q, want %q", dir, extracted)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.yaml")); err != nil {
		t.Errorf("manifest.yaml missing from the returned directory: %v", err)
	}
	if m.Name != "s3" || m.DisplayName != "S3" || m.Description != "S3 storage connector" {
		t.Errorf("Manifest = %+v", m)
	}
	if len(m.Connectors) != 1 || m.Connectors[0].SubClass != integration.ResourceSubClassS3 {
		t.Errorf("Connectors = %+v", m.Connectors)
	}
}

// An unknown package has neither a ptar to extract nor a cached manifest, so
// Manifest must report an error instead of panicking or returning zero values.
func TestFlatBackendManifestUnknownPackage(t *testing.T) {
	be, _, _ := newTestFlatBackend(t, nil)

	if _, _, err := be.Manifest(testPkg("unknown")); err == nil {
		t.Fatal("expected an error for an unknown package")
	}
}
