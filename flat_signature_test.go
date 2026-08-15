package pkg

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func testPkg(name string) *Package {
	return &Package{
		Name:            name,
		Version:         "v1.0.0",
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}
}

// A retained signature is what lets an installed package report its
// provenance and be re-checked later, so it must survive Load.
func TestSignatureRoundTrip(t *testing.T) {
	be, pkgdir, _ := newTestFlatBackend(t, nil)

	pkg := testPkg("s3")
	sig := []byte("untrusted comment: verify with plakar-20260815.pub\nsig\nSHA256 (x) = ab\n")

	// Load extracts the ptar, which a fake artifact cannot satisfy, so the
	// sidecar is written directly to exercise the retrieval path.
	if err := os.WriteFile(filepath.Join(pkgdir, pkg.Filename()), []byte("artifact"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(be.sigpath(pkg), sig, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := be.Signature(pkg)
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}

	if string(got) != string(sig) {
		t.Errorf("Signature returned %q, want %q", got, sig)
	}
}

// A package installed without a signature must report nil rather than an
// error, so callers can tell "unsigned" from "broken".
func TestSignatureAbsentIsNotAnError(t *testing.T) {
	be, _, _ := newTestFlatBackend(t, nil)

	got, err := be.Signature(testPkg("s3"))
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}

	if got != nil {
		t.Errorf("expected nil for an unsigned package, got %q", got)
	}
}

// Uninstalling must take the signature with it: a later install of the same
// version would otherwise inherit the previous one's provenance.
func TestUnloadRemovesSignature(t *testing.T) {
	be, pkgdir, _ := newTestFlatBackend(t, nil)

	pkg := testPkg("s3")
	pkgfile := filepath.Join(pkgdir, pkg.Filename())

	if err := os.WriteFile(pkgfile, []byte("artifact"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(be.sigpath(pkg), []byte("sig"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := be.unload(pkgfile, ""); err != nil {
		t.Fatalf("unload: %v", err)
	}

	if _, err := os.Stat(be.sigpath(pkg)); !os.IsNotExist(err) {
		t.Errorf("signature survived unload: %v", err)
	}
}

// The sidecar sits in the plugin directory, so List must not mistake it for
// an installed package.
func TestListIgnoresSignatures(t *testing.T) {
	be, pkgdir, _ := newTestFlatBackend(t, nil)

	pkg := testPkg("s3")
	touch(t, pkgdir, pkg.Filename())
	touch(t, pkgdir, pkg.Filename()+sigSuffix)

	var got []string
	for p, err := range be.List("") {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, p.Filename())
	}

	if len(got) != 1 || got[0] != pkg.Filename() {
		t.Errorf("List returned %v, want just %s", got, pkg.Filename())
	}
}
