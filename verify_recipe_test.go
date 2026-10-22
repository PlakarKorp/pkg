package pkg

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// recipeRegistry serves a package directory: a recipe, its signature, and
// whatever else a test wants to publish.
func recipeRegistry(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	for name, body := range files {
		mux.HandleFunc("/community/"+PLUGIN_API_VERSION+"/s3/"+name,
			func(body string) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, body)
				}
			}(body))
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

const testRecipe = "name: s3\nversion: s3/v1.1.5\nrepository: https://github.com/PlakarKorp/integrations\n"

// A recipe decides which version is installed, so it must reach the Verifier
// before it is parsed — otherwise whoever controls the server chooses the
// version regardless of how well the artifact itself is signed.
func TestRecipeIsVerifiedBeforeParse(t *testing.T) {
	srv := recipeRegistry(t, map[string]string{
		"recipe.yaml":         testRecipe,
		"recipe.yaml.sum.sig": "signature-bytes",
	})

	var seen *Artifact

	m, err := New(newFakeBackend(), &Options{
		DistURL: srv.URL,
		Verifier: VerifierFunc(func(a *Artifact, rd io.Reader) error {
			seen = a
			io.Copy(io.Discard, rd)
			return nil
		}),
	})
	require.NoError(t, err)

	r, err := m.FetchRecipe("s3", nil)
	require.NoError(t, err)
	require.NotNil(t, seen, "the recipe was parsed without reaching the Verifier")
	require.Equal(t, "recipe.yaml", seen.Filename)
	require.Equal(t, "signature-bytes", string(seen.Signature))
	require.Equal(t, "v1.1.5", r.Semver())
}

// A rejected recipe must not resolve a version: the whole point is that an
// attacker who can serve a recipe cannot choose what gets installed.
func TestRejectedRecipeYieldsNoVersion(t *testing.T) {
	srv := recipeRegistry(t, map[string]string{
		"recipe.yaml":         testRecipe,
		"recipe.yaml.sum.sig": "signature-bytes",
	})

	m, err := New(newFakeBackend(), &Options{
		DistURL: srv.URL,
		Verifier: VerifierFunc(func(a *Artifact, rd io.Reader) error {
			return fmt.Errorf("nope: %w", ErrUnverified)
		}),
	})
	require.NoError(t, err)

	_, err = m.FetchRecipe("s3", nil)
	require.ErrorIs(t, err, ErrUnverified)
}

// A recipe published without a signature still reaches the Verifier, with a
// nil Signature, so that policy stays in one place.
func TestUnsignedRecipeReachesVerifier(t *testing.T) {
	srv := recipeRegistry(t, map[string]string{
		"recipe.yaml": testRecipe,
	})

	var seen *Artifact

	m, err := New(newFakeBackend(), &Options{
		DistURL: srv.URL,
		Verifier: VerifierFunc(func(a *Artifact, rd io.Reader) error {
			seen = a
			io.Copy(io.Discard, rd)
			return nil
		}),
	})
	require.NoError(t, err)

	_, err = m.FetchRecipe("s3", nil)
	require.NoError(t, err)
	require.NotNil(t, seen, "the recipe never reached the Verifier")
	require.Nil(t, seen.Signature)
}

// Without a Verifier the signature is not even requested, so an existing
// deployment pays nothing for a feature it has not opted into.
func TestNoVerifierSkipsSignatureFetch(t *testing.T) {
	var requested []string

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		if r.URL.Path == "/community/"+PLUGIN_API_VERSION+"/s3/recipe.yaml" {
			fmt.Fprint(w, testRecipe)
			return
		}
		http.NotFound(w, r)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	m, err := New(newFakeBackend(), &Options{DistURL: srv.URL})
	require.NoError(t, err)

	_, err = m.FetchRecipe("s3", nil)
	require.NoError(t, err)

	for _, p := range requested {
		require.False(t, strings.HasSuffix(p, sigSuffix), "fetched %s with no Verifier configured", p)
	}
}

// Only a 404 means the signature is absent. Any other failure must abort,
// otherwise an outage on the signature path reads as "unsigned" and a lax
// policy installs whatever it is served.
func TestSignatureFetchFailureIsNotUnsigned(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{"not found", http.StatusNotFound, false},
		{"server error", http.StatusInternalServerError, true},
		{"forbidden", http.StatusForbidden, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/community/"+PLUGIN_API_VERSION+"/s3/recipe.yaml", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, testRecipe)
			})
			mux.HandleFunc("/community/"+PLUGIN_API_VERSION+"/s3/recipe.yaml"+sigSuffix, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			verified := false
			m, err := New(newFakeBackend(), &Options{
				DistURL: srv.URL,
				Verifier: VerifierFunc(func(a *Artifact, rd io.Reader) error {
					verified = true
					io.Copy(io.Discard, rd)
					return nil
				}),
			})
			require.NoError(t, err)

			_, err = m.FetchRecipe("s3", nil)
			if !tt.wantErr {
				require.NoError(t, err)
				require.True(t, verified)
				return
			}
			require.Error(t, err)
			require.False(t, verified, "an unreachable signature reached the Verifier as unsigned")
		})
	}
}
