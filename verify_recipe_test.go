package pkg

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// recipeRegistry serves a package directory: a recipe, its signature, and
// whatever else a test wants to publish.
func recipeRegistry(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	for name, body := range files {
		mux.HandleFunc("/"+PLUGIN_API_VERSION+"/s3/"+name,
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
		InstallURL: srv.URL,
		Verifier: VerifierFunc(func(a *Artifact, rd io.Reader) error {
			seen = a
			io.Copy(io.Discard, rd)
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.FetchRecipe("s3")
	if err != nil {
		t.Fatalf("FetchRecipe: %v", err)
	}

	if seen == nil {
		t.Fatal("the recipe was parsed without reaching the Verifier")
	}

	if seen.Filename != "recipe.yaml" {
		t.Errorf("filename = %q, want recipe.yaml", seen.Filename)
	}

	if string(seen.Signature) != "signature-bytes" {
		t.Errorf("signature = %q, want the published one", seen.Signature)
	}

	if r.Semver() != "v1.1.5" {
		t.Errorf("resolved version %q, want v1.1.5", r.Semver())
	}
}

// A rejected recipe must not resolve a version: the whole point is that an
// attacker who can serve a recipe cannot choose what gets installed.
func TestRejectedRecipeYieldsNoVersion(t *testing.T) {
	srv := recipeRegistry(t, map[string]string{
		"recipe.yaml":         testRecipe,
		"recipe.yaml.sum.sig": "signature-bytes",
	})

	m, err := New(newFakeBackend(), &Options{
		InstallURL: srv.URL,
		Verifier: VerifierFunc(func(a *Artifact, rd io.Reader) error {
			return fmt.Errorf("nope: %w", ErrUnverified)
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := m.FetchRecipe("s3"); !errors.Is(err, ErrUnverified) {
		t.Errorf("FetchRecipe error = %v, want ErrUnverified", err)
	}
}

// A recipe published without a signature still reaches the Verifier, with a
// nil Signature, so that policy stays in one place.
func TestUnsignedRecipeReachesVerifier(t *testing.T) {
	srv := recipeRegistry(t, map[string]string{
		"recipe.yaml": testRecipe,
	})

	var seen *Artifact

	m, err := New(newFakeBackend(), &Options{
		InstallURL: srv.URL,
		Verifier: VerifierFunc(func(a *Artifact, rd io.Reader) error {
			seen = a
			io.Copy(io.Discard, rd)
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := m.FetchRecipe("s3"); err != nil {
		t.Fatalf("FetchRecipe: %v", err)
	}

	if seen == nil {
		t.Fatal("the recipe never reached the Verifier")
	}

	if seen.Signature != nil {
		t.Errorf("expected a nil signature, got %q", seen.Signature)
	}
}

// Without a Verifier the signature is not even requested, so an existing
// deployment pays nothing for a feature it has not opted into.
func TestNoVerifierSkipsSignatureFetch(t *testing.T) {
	var requested []string

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		if r.URL.Path == "/"+PLUGIN_API_VERSION+"/s3/recipe.yaml" {
			fmt.Fprint(w, testRecipe)
			return
		}
		http.NotFound(w, r)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	m, err := New(newFakeBackend(), &Options{InstallURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := m.FetchRecipe("s3"); err != nil {
		t.Fatalf("FetchRecipe: %v", err)
	}

	for _, p := range requested {
		if len(p) > len(sigSuffix) && p[len(p)-len(sigSuffix):] == sigSuffix {
			t.Errorf("fetched %s with no Verifier configured", p)
		}
	}
}
