package pkg

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// recordingVerifier captures what it was asked to verify and answers with a
// canned result.
type recordingVerifier struct {
	err error

	calls    int
	artifact *Artifact
	content  []byte
}

func (v *recordingVerifier) Verify(a *Artifact, rd io.Reader) error {
	v.calls++

	cp := *a
	v.artifact = &cp

	b, err := io.ReadAll(rd)
	if err != nil {
		return err
	}
	v.content = b

	return v.err
}

// writeLocalPkg writes an artifact, and optionally its signature, into dir and
// returns the artifact's path.
func writeLocalPkg(t *testing.T, dir, name string, content, sig []byte) string {
	t.Helper()

	pkg := &Package{
		Name:            name,
		Version:         "v1.0.0",
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}

	target := filepath.Join(dir, pkg.Filename())

	if err := os.WriteFile(target, content, 0644); err != nil {
		t.Fatalf("writing artifact: %v", err)
	}

	if sig != nil {
		if err := os.WriteFile(target+sigSuffix, sig, 0644); err != nil {
			t.Fatalf("writing signature: %v", err)
		}
	}

	return target
}

func TestVerifierNotCalledWhenAbsent(t *testing.T) {
	backend := newFakeBackend()

	m, err := New(backend, &Options{})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	target := writeLocalPkg(t, dir, "s3", []byte("artifact"), nil)

	if err := m.Add(target, nil); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if len(backend.loaded) != 1 {
		t.Fatalf("expected the package to be installed, got %d loads", len(backend.loaded))
	}
}

func TestVerifierReceivesArtifactAndSignature(t *testing.T) {
	backend := newFakeBackend()
	verifier := &recordingVerifier{}

	m, err := New(backend, &Options{Verifier: verifier})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	content := []byte("the artifact bytes")
	sig := []byte("untrusted comment: verify with plakar.pub\nsignature\nSHA256 (x) = abc\n")
	target := writeLocalPkg(t, dir, "s3", content, sig)

	if err := m.Add(target, nil); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if verifier.calls != 1 {
		t.Fatalf("expected exactly 1 verification, got %d", verifier.calls)
	}

	if string(verifier.content) != string(content) {
		t.Errorf("verifier saw %q, want %q", verifier.content, content)
	}

	if string(verifier.artifact.Signature) != string(sig) {
		t.Errorf("verifier got signature %q, want %q", verifier.artifact.Signature, sig)
	}

	if verifier.artifact.Origin != LocalOrigin {
		t.Errorf("origin = %q, want %q", verifier.artifact.Origin, LocalOrigin)
	}

	if verifier.artifact.Package.Name != "s3" {
		t.Errorf("package name = %q, want s3", verifier.artifact.Package.Name)
	}
}

// A rejected artifact must not reach the backend at all: the parser is never
// handed bytes the Verifier refused.
func TestVerifierRejectionPreventsInstall(t *testing.T) {
	backend := newFakeBackend()
	verifier := &recordingVerifier{err: fmt.Errorf("bad signature: %w", ErrUnverified)}

	m, err := New(backend, &Options{Verifier: verifier})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	target := writeLocalPkg(t, dir, "s3", []byte("artifact"), []byte("sig"))

	err = m.Add(target, nil)
	if err == nil {
		t.Fatal("expected Add to fail when verification fails")
	}

	if !errors.Is(err, ErrUnverified) {
		t.Errorf("error %v does not wrap ErrUnverified", err)
	}

	if len(backend.loaded) != 0 {
		t.Errorf("backend saw %d loads, want 0 — unverified bytes reached the backend",
			len(backend.loaded))
	}
}

// An artifact with no signature alongside it still reaches the Verifier, with
// a nil Signature, so that policy lives entirely in the Verifier.
func TestMissingSignatureReachesVerifier(t *testing.T) {
	backend := newFakeBackend()
	verifier := &recordingVerifier{}

	m, err := New(backend, &Options{Verifier: verifier})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	target := writeLocalPkg(t, dir, "s3", []byte("artifact"), nil)

	if err := m.Add(target, nil); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if verifier.calls != 1 {
		t.Fatalf("expected the verifier to be consulted, got %d calls", verifier.calls)
	}

	if verifier.artifact.Signature != nil {
		t.Errorf("expected a nil signature, got %q", verifier.artifact.Signature)
	}
}

// The bytes handed to the backend must be the same ones the Verifier approved.
func TestVerifiedContentReachesBackendIntact(t *testing.T) {
	backend := newFakeBackend()
	verifier := &recordingVerifier{}

	m, err := New(backend, &Options{Verifier: verifier})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	content := []byte("exactly these bytes and no others")
	target := writeLocalPkg(t, dir, "s3", content, []byte("sig"))

	if err := m.Add(target, nil); err != nil {
		t.Fatalf("Add: %v", err)
	}

	pkg := &Package{
		Name:            "s3",
		Version:         "v1.0.0",
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}

	got := backend.loadData[pkg.Filename()]
	if string(got) != string(content) {
		t.Errorf("backend received %q, want %q", got, content)
	}
}
