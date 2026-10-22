package pkg

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// registry serves the s3 package directory of the community edition. Files
// absent from the map answer 404; a body of the form "status:NNN" answers
// that status instead.
func registry(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()

	dir := "/community/" + PLUGIN_API_VERSION + "/s3/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[strings.TrimPrefix(r.URL.Path, dir)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		var code int
		if _, err := fmt.Sscanf(body, "status:%d", &code); err == nil {
			w.WriteHeader(code)
			return
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func acceptAll(a *Artifact, rd io.Reader) error {
	_, err := io.Copy(io.Discard, rd)
	return err
}

func TestFetchRecipeInstallError(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		artifact ArtifactKind
		file     string
		status   int
		cause    error
	}{
		{
			name:     "recipe not found",
			files:    map[string]string{},
			artifact: ArtifactRecipe,
			file:     "recipe.yaml",
			status:   http.StatusNotFound,
			cause:    ErrNotFound,
		},
		{
			name:     "recipe unauthorized",
			files:    map[string]string{"recipe.yaml": "status:401"},
			artifact: ArtifactRecipe,
			file:     "recipe.yaml",
			status:   http.StatusUnauthorized,
			cause:    ErrAuthorizationRequired,
		},
		{
			name:     "recipe forbidden",
			files:    map[string]string{"recipe.yaml": "status:403"},
			artifact: ArtifactRecipe,
			file:     "recipe.yaml",
			status:   http.StatusForbidden,
			cause:    ErrForbidden,
		},
		{
			name:     "recipe server error",
			files:    map[string]string{"recipe.yaml": "status:502"},
			artifact: ArtifactRecipe,
			file:     "recipe.yaml",
			status:   http.StatusBadGateway,
			cause:    ErrUnexpectedStatus,
		},
		{
			name: "recipe mismatch",
			files: map[string]string{
				"recipe.yaml": "name: gcs\nversion: v1.2.3\n",
			},
			artifact: ArtifactRecipe,
			file:     "recipe.yaml",
			cause:    ErrRecipeMismatch,
		},
		{
			name: "signature unreachable",
			files: map[string]string{
				"recipe.yaml":             testRecipe,
				"recipe.yaml" + sigSuffix: "status:500",
			},
			artifact: ArtifactSignature,
			file:     "recipe.yaml" + sigSuffix,
			status:   http.StatusInternalServerError,
			cause:    ErrUnexpectedStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := registry(t, tt.files)
			m, err := New(newFakeBackend(), &Options{
				DistURL:  srv.URL,
				Verifier: VerifierFunc(acceptAll),
			})
			require.NoError(t, err)

			_, err = m.FetchRecipe("s3", nil)
			require.ErrorIs(t, err, tt.cause)

			var ie *InstallError
			require.ErrorAs(t, err, &ie)
			require.Equal(t, "s3", ie.Name)
			require.Nil(t, ie.Package)
			require.Equal(t, "community", ie.Edition)
			require.Equal(t, tt.artifact, ie.Artifact)
			require.Equal(t, srv.URL+"/community/"+PLUGIN_API_VERSION+"/s3/"+tt.file, ie.URL)
			require.Equal(t, tt.status, ie.StatusCode)
			require.Contains(t, err.Error(), ie.URL)
		})
	}
}

func TestFetchRecipeMalformedIsInstallError(t *testing.T) {
	srv := registry(t, map[string]string{"recipe.yaml": "name: [s3\n"})
	m, err := New(newFakeBackend(), &Options{DistURL: srv.URL})
	require.NoError(t, err)

	_, err = m.FetchRecipe("s3", nil)

	var ie *InstallError
	require.ErrorAs(t, err, &ie)
	require.Equal(t, ArtifactRecipe, ie.Artifact)
	require.Zero(t, ie.StatusCode)
}

func TestFetchRecipeTransportError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()

	m, err := New(newFakeBackend(), &Options{DistURL: srv.URL})
	require.NoError(t, err)

	_, err = m.FetchRecipe("s3", nil)

	var ie *InstallError
	require.ErrorAs(t, err, &ie)
	require.Equal(t, ArtifactRecipe, ie.Artifact)
	require.Zero(t, ie.StatusCode)
	require.False(t, errors.Is(err, ErrNotFound))
}

// A package missing from the repository while its recipe exists means the
// version is not built for this platform: the error must say which package,
// version included.
func TestAddPackageNotFound(t *testing.T) {
	srv := registry(t, map[string]string{
		"recipe.yaml": "name: s3\nversion: v1.2.3\n",
	})
	be := newFakeBackend()
	m, err := New(be, &Options{DistURL: srv.URL, Edition: "community"})
	require.NoError(t, err)

	err = m.Add("s3", &AddOptions{ImplicitFetch: true})
	require.ErrorIs(t, err, ErrNotFound)

	var ie *InstallError
	require.ErrorAs(t, err, &ie)
	require.Equal(t, ArtifactPackage, ie.Artifact)
	require.Equal(t, http.StatusNotFound, ie.StatusCode)
	require.Equal(t, &Package{
		Name:            "s3",
		Version:         "v1.2.3",
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}, ie.Package)
	require.True(t, strings.HasSuffix(ie.URL, ie.Package.Filename()))
	require.Contains(t, err.Error(), "s3 v1.2.3")
	require.Empty(t, be.loaded)
}

// Refusing to send an unauthenticated request is reported like a 401, with
// no status since nothing was sent.
func TestAddPackageMissingCredentials(t *testing.T) {
	srv := registry(t, map[string]string{})
	m, err := New(newFakeBackend(), &Options{DistURL: srv.URL, BinaryNeedsAuth: true})
	require.NoError(t, err)

	err = m.Add("s3", &AddOptions{ImplicitFetch: true, Version: "v1.0.0"})
	require.ErrorIs(t, err, ErrAuthorizationRequired)

	var ie *InstallError
	require.ErrorAs(t, err, &ie)
	require.Equal(t, ArtifactPackage, ie.Artifact)
	require.Zero(t, ie.StatusCode)
}

var errRejected = errors.New("rejected")

func reject(a *Artifact, rd io.Reader) error {
	return fmt.Errorf("%w: %w", ErrUnverified, errRejected)
}

// The Verifier owns the reason; the InstallError adds what was being
// verified and where it came from.
func TestVerifierRejectionIsInstallError(t *testing.T) {
	srv := registry(t, map[string]string{
		"recipe.yaml": "name: s3\nversion: v1.2.3\n",
		(&Package{Name: "s3", Version: "v1.2.3", OperatingSystem: runtime.GOOS, Architecture: runtime.GOARCH}).Filename(): "PTARDATA",
	})

	t.Run("recipe", func(t *testing.T) {
		m, err := New(newFakeBackend(), &Options{DistURL: srv.URL, Verifier: VerifierFunc(reject)})
		require.NoError(t, err)

		_, err = m.FetchRecipe("s3", nil)
		require.ErrorIs(t, err, errRejected)

		var ie *InstallError
		require.ErrorAs(t, err, &ie)
		require.Equal(t, ArtifactRecipe, ie.Artifact)
		require.Equal(t, srv.URL+"/community/"+PLUGIN_API_VERSION+"/s3/recipe.yaml", ie.URL)
	})

	t.Run("package", func(t *testing.T) {
		m, err := New(newFakeBackend(), &Options{DistURL: srv.URL, Verifier: VerifierFunc(reject)})
		require.NoError(t, err)

		err = m.Add("s3", &AddOptions{ImplicitFetch: true, Version: "v1.2.3"})
		require.ErrorIs(t, err, errRejected)

		var ie *InstallError
		require.ErrorAs(t, err, &ie)
		require.Equal(t, ArtifactPackage, ie.Artifact)
		require.Equal(t, "v1.2.3", ie.Package.Version)
		require.Equal(t, "community", ie.Edition)
		require.True(t, strings.HasSuffix(ie.URL, ie.Package.Filename()))
	})

	t.Run("local", func(t *testing.T) {
		m, err := New(newFakeBackend(), &Options{Verifier: VerifierFunc(reject)})
		require.NoError(t, err)

		target := writeLocalPkg(t, t.TempDir(), "s3", []byte("artifact"), nil)
		err = m.Add(target, nil)
		require.ErrorIs(t, err, errRejected)

		var ie *InstallError
		require.ErrorAs(t, err, &ie)
		require.Equal(t, ArtifactPackage, ie.Artifact)
		require.Equal(t, "s3", ie.Name)
		require.Equal(t, target, ie.URL)
		require.Empty(t, ie.Edition)
		require.Zero(t, ie.StatusCode)
	})
}
