//go:build !windows

package pkg

import (
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// TestQueryLocalManifestReadmeFifoIgnored guards against a malicious package
// shipping a FIFO as README.md: opening it for read would block until a
// writer shows up, which would otherwise stall Query forever. readLocalFile
// must reject it as soon as it stats as non-regular, without ever opening it.
func TestQueryLocalManifestReadmeFifoIgnored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("named pipes are not FIFOs on windows")
	}

	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "README.md"), 0644); err != nil {
		t.Fatal(err)
	}

	m := &Manifest{
		Name:        "talos",
		DisplayName: "Talos Linux",
	}

	be := newFakeBackend(pkgVer("talos", "v1.0.0"))
	be.manifests["talos"] = m
	be.manifestDirs["talos"] = dir

	mgr, _ := New(be, nil)
	got, err := mgr.Query(&QueryOptions{OnlyLocal: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Query returned %d results, want 1", len(got))
	}
	if got[0].Documentation != "" {
		t.Errorf("Documentation = %q, want empty (README.md is a FIFO, not a regular file)", got[0].Documentation)
	}
}
