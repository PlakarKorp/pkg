package pkg

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
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
	require.NoError(t, os.WriteFile(target, content, 0644))
	if sig != nil {
		require.NoError(t, os.WriteFile(target+sigSuffix, sig, 0644))
	}

	return target
}

func TestVerifierNotCalledWhenAbsent(t *testing.T) {
	backend := newFakeBackend()

	m, err := New(backend, &Options{})
	require.NoError(t, err)

	dir := t.TempDir()
	target := writeLocalPkg(t, dir, "s3", []byte("artifact"), nil)

	require.NoError(t, m.Add(target, nil))
	require.Len(t, backend.loaded, 1)
}

func TestVerifierReceivesArtifactAndSignature(t *testing.T) {
	backend := newFakeBackend()
	verifier := &recordingVerifier{}

	m, err := New(backend, &Options{Verifier: verifier})
	require.NoError(t, err)

	dir := t.TempDir()
	content := []byte("the artifact bytes")
	sig := []byte("untrusted comment: verify with plakar.pub\nsignature\nSHA256 (x) = abc\n")
	target := writeLocalPkg(t, dir, "s3", content, sig)

	require.NoError(t, m.Add(target, nil))
	require.Equal(t, 1, verifier.calls)
	require.Equal(t, content, verifier.content)
	require.Equal(t, sig, verifier.artifact.Signature)
	require.Equal(t, LocalOrigin, verifier.artifact.Origin)
	require.Equal(t, "s3", verifier.artifact.Package.Name)
}

// A rejected artifact must not reach the backend at all: the parser is never
// handed bytes the Verifier refused.
func TestVerifierRejectionPreventsInstall(t *testing.T) {
	backend := newFakeBackend()
	verifier := &recordingVerifier{err: fmt.Errorf("bad signature: %w", ErrUnverified)}

	m, err := New(backend, &Options{Verifier: verifier})
	require.NoError(t, err)

	dir := t.TempDir()
	target := writeLocalPkg(t, dir, "s3", []byte("artifact"), []byte("sig"))

	require.ErrorIs(t, m.Add(target, nil), ErrUnverified)
	require.Empty(t, backend.loaded, "unverified bytes reached the backend")
}

// An artifact with no signature alongside it still reaches the Verifier, with
// a nil Signature, so that policy lives entirely in the Verifier.
func TestMissingSignatureReachesVerifier(t *testing.T) {
	backend := newFakeBackend()
	verifier := &recordingVerifier{}

	m, err := New(backend, &Options{Verifier: verifier})
	require.NoError(t, err)

	dir := t.TempDir()
	target := writeLocalPkg(t, dir, "s3", []byte("artifact"), nil)

	require.NoError(t, m.Add(target, nil))
	require.Equal(t, 1, verifier.calls)
	require.Nil(t, verifier.artifact.Signature)
}

// The bytes handed to the backend must be the same ones the Verifier approved.
func TestVerifiedContentReachesBackendIntact(t *testing.T) {
	backend := newFakeBackend()
	verifier := &recordingVerifier{}

	m, err := New(backend, &Options{Verifier: verifier})
	require.NoError(t, err)

	dir := t.TempDir()
	content := []byte("exactly these bytes and no others")
	target := writeLocalPkg(t, dir, "s3", content, []byte("sig"))

	require.NoError(t, m.Add(target, nil))

	pkg := &Package{
		Name:            "s3",
		Version:         "v1.0.0",
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}
	require.Equal(t, content, backend.loadData[pkg.Filename()])
}
