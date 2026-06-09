package pkg

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"testing"

	"github.com/PlakarKorp/kloset/kcontext"
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
	if err != nil {
		t.Fatalf("NewFlatBackend: %v", err)
	}
	return be, pkgdir, cachedir
}

func TestNewFlatBackendCreatesDirs(t *testing.T) {
	be, pkgdir, cachedir := newTestFlatBackend(t, nil)
	_ = be
	for _, d := range []string{pkgdir, cachedir} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Errorf("expected directory %q to exist: err=%v", d, err)
		}
	}
}

// touch creates an empty file with the given name inside pkgdir.
func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0644); err != nil {
		t.Fatal(err)
	}
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
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, p.Name+"_"+p.Version)
	}
	sort.Strings(got)
	want := []string{"ftp_v0.1.0", "s3_v1.0.0", "s3_v2.0.0"}
	if !slices.Equal(got, want) {
		t.Errorf("List(\"\") = %v, want %v", got, want)
	}

	// filtered by name
	var s3 []string
	for p, err := range be.List("s3") {
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "s3" {
			t.Errorf("filtered list returned %q", p.Name)
		}
		s3 = append(s3, p.Version)
	}
	sort.Strings(s3)
	if !slices.Equal(s3, []string{"v1.0.0", "v2.0.0"}) {
		t.Errorf("List(s3) = %v", s3)
	}
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
		if err != nil {
			t.Fatal(err)
		}
		count++
		break // consumer stops early; iterator must respect the false yield
	}
	if count != 1 {
		t.Errorf("consumed %d items after break, want 1", count)
	}
}

func TestFlatBackendListMissingDir(t *testing.T) {
	root := t.TempDir()
	kctx := kcontext.NewKContext()
	be, err := NewFlatBackend(kctx, filepath.Join(root, "pkgs"), filepath.Join(root, "cache"), &FlatBackendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Remove the pkgdir out from under the backend.
	if err := os.RemoveAll(be.pkgdir); err != nil {
		t.Fatal(err)
	}

	var gotErr error
	for _, err := range be.List("") {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr == nil {
		t.Fatal("expected error listing a missing pkgdir")
	}
}

// loadmanifest validates the executable path stays within the manifest's
// own directory. A path that escapes via ".." must be rejected.
func TestLoadManifestRejectsPathTraversal(t *testing.T) {
	be, _, cachedir := newTestFlatBackend(t, nil)

	mdir := filepath.Join(cachedir, "evil")
	if err := os.MkdirAll(mdir, 0755); err != nil {
		t.Fatal(err)
	}
	const manifest = `
name: evil
connectors:
  - type: storage
    executable: ../../../etc/passwd
`
	mpath := filepath.Join(mdir, "manifest.yaml")
	if err := os.WriteFile(mpath, []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := be.loadmanifest(mpath); err == nil {
		t.Fatal("loadmanifest accepted an executable path escaping the manifest dir")
	}
}

func TestLoadManifestAcceptsValidExecutable(t *testing.T) {
	be, _, cachedir := newTestFlatBackend(t, nil)

	mdir := filepath.Join(cachedir, "good")
	if err := os.MkdirAll(mdir, 0755); err != nil {
		t.Fatal(err)
	}
	const manifest = `
name: good
connectors:
  - type: storage
    class: object-storage
    subclass: s3
    executable: bin/s3-storage
    location_flags:
      - localfs
`
	mpath := filepath.Join(mdir, "manifest.yaml")
	if err := os.WriteFile(mpath, []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := be.loadmanifest(mpath)
	if err != nil {
		t.Fatalf("loadmanifest: %v", err)
	}
	if m.Name != "good" {
		t.Errorf("Name = %q", m.Name)
	}
}

func TestLoadManifestRejectsBadFlags(t *testing.T) {
	be, _, cachedir := newTestFlatBackend(t, nil)

	mdir := filepath.Join(cachedir, "badflag")
	if err := os.MkdirAll(mdir, 0755); err != nil {
		t.Fatal(err)
	}
	const manifest = `
name: badflag
connectors:
  - type: storage
    executable: tool
    location_flags:
      - notarealflag
`
	mpath := filepath.Join(mdir, "manifest.yaml")
	if err := os.WriteFile(mpath, []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := be.loadmanifest(mpath); err == nil {
		t.Fatal("loadmanifest accepted an unknown location flag")
	}
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
	if err := os.WriteFile(ptarPath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	extracted := filepath.Join(cachedir, "s3_v1.0.0_"+runtime.GOOS+"_"+runtime.GOARCH)
	if err := os.MkdirAll(extracted, 0755); err != nil {
		t.Fatal(err)
	}

	// No unloadhook, so Unload should not need a manifest.
	if err := be.Unload(pkg); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if _, err := os.Stat(ptarPath); !os.IsNotExist(err) {
		t.Errorf("ptar file still exists after Unload: %v", err)
	}
	if _, err := os.Stat(extracted); !os.IsNotExist(err) {
		t.Errorf("extracted dir still exists after Unload: %v", err)
	}
}

func TestFlatBackendUnloadHook(t *testing.T) {
	var called bool
	var gotPkg *Package
	be, pkgdir, cachedir := newTestFlatBackend(t, &FlatBackendOptions{
		UnloadHook: func(m *Manifest, p *Package) {
			called = true
			gotPkg = p
		},
	})

	pkg := &Package{
		Name:            "s3",
		Version:         "v1.0.0",
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}
	if err := os.WriteFile(filepath.Join(pkgdir, pkg.Filename()), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	extracted := filepath.Join(cachedir, "s3_v1.0.0_"+runtime.GOOS+"_"+runtime.GOARCH)
	if err := os.MkdirAll(extracted, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extracted, "manifest.yaml"), []byte("name: s3\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := be.Unload(pkg); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if !called {
		t.Fatal("unload hook was not called")
	}
	if gotPkg == nil || gotPkg.Name != "s3" {
		t.Errorf("unload hook got package %+v", gotPkg)
	}
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
	if err := os.WriteFile(filepath.Join(pkgdir, pkg.Filename()), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	// With an unloadhook but no extracted manifest, Unload should report
	// the missing manifest rather than silently succeed.
	if err := be.Unload(pkg); err == nil {
		t.Fatal("expected error when unloadhook set but manifest missing")
	}
}

// unload tolerates an already-missing extracted directory.
func TestFlatBackendUnloadIdempotent(t *testing.T) {
	be, pkgdir, _ := newTestFlatBackend(t, nil)
	ptarPath := filepath.Join(pkgdir, "s3_v1.0.0_"+runtime.GOOS+"_"+runtime.GOARCH+".ptar")
	if err := os.WriteFile(ptarPath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := be.unload(ptarPath, filepath.Join(t.TempDir(), "does-not-exist")); err != nil {
		t.Errorf("unload with missing extracted dir: %v", err)
	}
}
