package pkg

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeBackend is an in-memory Backend implementation for exercising the
// Manager orchestration logic without touching disk, ptar archives, or
// the kloset storage engine.
type fakeBackend struct {
	pkgs     []*Package
	loaded   []*Package // packages passed to Load, with their bytes
	loadData map[string][]byte
	loadSigs map[string][]byte
	unloaded []*Package

	listErr   error
	loadErr   error
	unloadErr error

	// manifests and manifestDirs back fakeBackend's ManifestReader
	// implementation, keyed by package name. manifestErr, if set,
	// makes Manifest fail for every package.
	manifests    map[string]*Manifest
	manifestDirs map[string]string
	manifestErr  error
}

func newFakeBackend(pkgs ...*Package) *fakeBackend {
	return &fakeBackend{
		pkgs:         pkgs,
		loadData:     map[string][]byte{},
		loadSigs:     map[string][]byte{},
		manifests:    map[string]*Manifest{},
		manifestDirs: map[string]string{},
	}
}

// Manifest implements ManifestReader.
func (f *fakeBackend) Manifest(pkg *Package) (*Manifest, string, error) {
	if f.manifestErr != nil {
		return nil, "", f.manifestErr
	}
	m, ok := f.manifests[pkg.Name]
	if !ok {
		return nil, "", fmt.Errorf("no manifest for %q", pkg.Name)
	}
	return m, f.manifestDirs[pkg.Name], nil
}

var _ ManifestReader = (*fakeBackend)(nil)

func (f *fakeBackend) List(name string) iter.Seq2[*Package, error] {
	return func(yield func(*Package, error) bool) {
		if f.listErr != nil {
			yield(nil, f.listErr)
			return
		}
		// snapshot so a consumer that mutates f.pkgs (e.g. via Unload)
		// during iteration does not race the range below.
		snapshot := slices.Clone(f.pkgs)
		for _, p := range snapshot {
			if name != "" && p.Name != name {
				continue
			}
			if !yield(p, nil) {
				return
			}
		}
	}
}

func (f *fakeBackend) Load(p *Package, rd io.Reader, sig []byte) error {
	if f.loadErr != nil {
		return f.loadErr
	}
	b, err := io.ReadAll(rd)
	if err != nil {
		return err
	}
	cp := *p
	f.loaded = append(f.loaded, &cp)
	f.loadData[cp.Filename()] = b
	if sig != nil {
		f.loadSigs[cp.Filename()] = sig
	}
	f.pkgs = append(f.pkgs, &cp)
	return nil
}

func (f *fakeBackend) Signature(p *Package) ([]byte, error) {
	return f.loadSigs[p.Filename()], nil
}

func (f *fakeBackend) Unload(p *Package) error {
	if f.unloadErr != nil {
		return f.unloadErr
	}
	cp := *p
	f.unloaded = append(f.unloaded, &cp)
	// drop it from the installed set
	f.pkgs = slices.DeleteFunc(f.pkgs, func(q *Package) bool {
		return q.Filename() == p.Filename()
	})
	return nil
}

func pkgOf(t *testing.T, name string) *Package {
	t.Helper()
	return &Package{
		Name:            name,
		Version:         "v1.0.0",
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}
}

func pkgVer(name, version string) *Package {
	return &Package{
		Name:            name,
		Version:         version,
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}
}

func TestNewManagerDefaults(t *testing.T) {
	m, err := New(newFakeBackend(), nil)
	require.NoError(t, err)
	wantUA := fmt.Sprintf("pkg/v0.0.1 (%s/%s)", runtime.GOOS, runtime.GOARCH)
	require.Equal(t, wantUA, m.useragent)
}

func TestNewManagerCustomUserAgent(t *testing.T) {
	m, err := New(newFakeBackend(), &Options{UserAgent: "myapp/1.0"})
	require.NoError(t, err)
	wantUA := fmt.Sprintf("myapp/1.0 (%s/%s)", runtime.GOOS, runtime.GOARCH)
	require.Equal(t, wantUA, m.useragent)
}

func TestNewManagerURLs(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://plakar.io/dist", false},
		{"http://127.0.0.1:8080", false},
		{"://bad", true},
		{"plakar.io/dist", true},
		{"ftp://plakar.io/dist", true},
		{"file:///srv/dist", true},
		{"https:///dist", true},
	}
	options := map[string]func(string) *Options{
		"DistURL":  func(u string) *Options { return &Options{DistURL: u} },
		"ApiURL":   func(u string) *Options { return &Options{ApiURL: u} },
		"IndexURL": func(u string) *Options { return &Options{IndexURL: u} },
	}
	for option, opts := range options {
		for _, tt := range tests {
			t.Run(option+"/"+tt.url, func(t *testing.T) {
				_, err := New(newFakeBackend(), opts(tt.url))
				if !tt.wantErr {
					require.NoError(t, err)
					return
				}
				require.ErrorIs(t, err, ErrInvalidOptions)
			})
		}
	}
}

func TestManagerList(t *testing.T) {
	be := newFakeBackend(pkgOf(t, "s3"), pkgOf(t, "ftp"))
	m, _ := New(be, nil)

	var names []string
	for p, err := range m.List() {
		require.NoError(t, err)
		names = append(names, p.Name)
	}
	require.Equal(t, []string{"s3", "ftp"}, names)
}

func TestAddInvalidOptionCombinations(t *testing.T) {
	tests := []struct {
		name string
		opts *AddOptions
	}{
		{"upgrade+downgrade", &AddOptions{Upgrade: true, Downgrade: true}},
		{"replace+upgrade", &AddOptions{Replace: true, Upgrade: true}},
		{"replace+downgrade", &AddOptions{Replace: true, Downgrade: true}},
		{"multi+upgrade", &AddOptions{AllowMultipleVersions: true, Upgrade: true}},
		{"multi+downgrade", &AddOptions{AllowMultipleVersions: true, Downgrade: true}},
		{"multi+replace", &AddOptions{AllowMultipleVersions: true, Replace: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := New(newFakeBackend(), nil)
			err := m.Add("s3_v1.0.0_"+runtime.GOOS+"_"+runtime.GOARCH+".ptar", tt.opts)
			require.ErrorIs(t, err, ErrInvalidOptions)
		})
	}
}

func TestAddRejectsBadName(t *testing.T) {
	m, _ := New(newFakeBackend(), nil)
	err := m.Add("not-a-package", nil)
	require.ErrorIs(t, err, ErrBadPackageName)
}

func TestAddRejectsOSArchMismatch(t *testing.T) {
	be := newFakeBackend()
	m, _ := New(be, nil)

	// A package for an OS that is definitely not the host.
	bad := "s3_v1.0.0_plan9_sparc64.ptar"
	err := m.Add(bad, nil)
	require.ErrorIs(t, err, ErrBadOSArch)
	require.Empty(t, be.loaded)
}

func TestAddAllowOSArchMismatchStillFailsOnMissingFile(t *testing.T) {
	be := newFakeBackend()
	m, _ := New(be, nil)

	// With the mismatch allowed, the OS/arch gate is skipped and we
	// proceed to open the (nonexistent) file, which should error.
	err := m.Add("s3_v1.0.0_plan9_sparc64.ptar", &AddOptions{AllowOSArchMismatch: true})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrBadOSArch)
}

func TestPreaddNoExistingVersion(t *testing.T) {
	be := newFakeBackend() // empty
	m, _ := New(be, nil)
	require.NoError(t, m.preadd("s3", "v1.0.0", &AddOptions{}))
	require.Empty(t, be.unloaded)
}

func TestPreaddAlreadyInstalledDefault(t *testing.T) {
	be := newFakeBackend(pkgVer("s3", "v1.0.0"))
	m, _ := New(be, nil)
	err := m.preadd("s3", "v1.0.0", &AddOptions{})
	require.ErrorIs(t, err, ErrAlreadyInstalled)
}

func TestPreaddUpgrade(t *testing.T) {
	tests := []struct {
		name       string
		installed  string
		requested  string
		opts       *AddOptions
		wantErr    error
		wantUnload bool
	}{
		{"upgrade newer ok", "v1.0.0", "v2.0.0", &AddOptions{Upgrade: true}, nil, true},
		{"upgrade older rejected", "v2.0.0", "v1.0.0", &AddOptions{Upgrade: true}, ErrAlreadyInstalled, false},
		{"upgrade same rejected", "v1.0.0", "v1.0.0", &AddOptions{Upgrade: true}, ErrAlreadyInstalled, false},
		{"downgrade older ok", "v2.0.0", "v1.0.0", &AddOptions{Downgrade: true}, nil, true},
		{"downgrade newer rejected", "v1.0.0", "v2.0.0", &AddOptions{Downgrade: true}, ErrAlreadyInstalled, false},
		{"replace same ok", "v1.0.0", "v1.0.0", &AddOptions{Replace: true}, nil, true},
		{"replace newer ok", "v1.0.0", "v2.0.0", &AddOptions{Replace: true}, nil, true},
		{"replace older ok", "v2.0.0", "v1.0.0", &AddOptions{Replace: true}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newFakeBackend(pkgVer("s3", tt.installed))
			m, _ := New(be, nil)
			err := m.preadd("s3", tt.requested, tt.opts)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantUnload, len(be.unloaded) > 0, "unloaded")
		})
	}
}

// TestPreaddReplaceDifferentVersion guards against a regression in which
// AddOptions.Replace stopped working when the installed version differed
// from the requested one.
//
// Replace is documented as "Remove other version of the plugin, even if
// it's the same, and install this version", so it must replace any
// existing version regardless of how the two compare. Commit dfb4172
// once broke this by routing Replace through the cmp-based Upgrade/
// Downgrade guards; this test ensures it stays fixed.
func TestPreaddReplaceDifferentVersion(t *testing.T) {
	for _, installed := range []string{"v1.0.0", "v2.0.0"} {
		t.Run("installed_"+installed, func(t *testing.T) {
			be := newFakeBackend(pkgVer("s3", installed))
			m, _ := New(be, nil)
			require.NoError(t, m.preadd("s3", "v1.5.0", &AddOptions{Replace: true}))
			require.Len(t, be.unloaded, 1)
		})
	}
}

func TestPreaddAllowMultipleVersions(t *testing.T) {
	be := newFakeBackend(pkgVer("s3", "v1.0.0"))
	m, _ := New(be, nil)

	// A different version is fine and does not unload the existing one.
	require.NoError(t, m.preadd("s3", "v2.0.0", &AddOptions{AllowMultipleVersions: true}))
	require.Empty(t, be.unloaded)

	// The same version, however, is still rejected.
	require.ErrorIs(t, m.preadd("s3", "v1.0.0", &AddOptions{AllowMultipleVersions: true}), ErrAlreadyInstalled)
}

func TestPreaddPropagatesListError(t *testing.T) {
	be := newFakeBackend()
	be.listErr = errors.New("boom")
	m, _ := New(be, nil)
	require.Error(t, m.preadd("s3", "v1.0.0", &AddOptions{}))
}

func TestDelRequiresTargetOrAll(t *testing.T) {
	m, _ := New(newFakeBackend(pkgOf(t, "s3")), nil)
	require.ErrorIs(t, m.Del("", nil), ErrBadPackageName)
}

func TestDelByName(t *testing.T) {
	be := newFakeBackend(pkgOf(t, "s3"), pkgOf(t, "ftp"))
	m, _ := New(be, nil)
	require.NoError(t, m.Del("s3", nil))
	require.Len(t, be.unloaded, 1)
	require.Equal(t, "s3", be.unloaded[0].Name)
}

func TestDelAll(t *testing.T) {
	be := newFakeBackend(pkgOf(t, "s3"), pkgOf(t, "ftp"))
	m, _ := New(be, nil)
	require.NoError(t, m.Del("", &DelOptions{All: true}))
	require.Len(t, be.unloaded, 2)
}

func TestFetchRecipe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/community/" + PLUGIN_API_VERSION + "/s3/recipe.yaml"
		if r.URL.Path != wantPath {
			t.Errorf("request path = %q, want %q", r.URL.Path, wantPath)
		}
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "pkg/") {
			t.Errorf("missing/unexpected User-Agent: %q", ua)
		}
		io.WriteString(w, "name: s3\nversion: v1.2.3\n")
	}))
	defer srv.Close()

	m, _ := New(newFakeBackend(), &Options{DistURL: srv.URL})
	r, err := m.FetchRecipe("s3", nil)
	require.NoError(t, err)
	require.Equal(t, "s3", r.Name)
	require.Equal(t, "v1.2.3", r.Version)
}

func TestFetchRecipeRejectsBadName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request for %q", r.URL.Path)
		http.Error(w, "unexpected", http.StatusNotFound)
	}))
	defer srv.Close()

	m, _ := New(newFakeBackend(), &Options{DistURL: srv.URL})
	for _, name := range []string{"", "..", "../s3", "s3/../../x", "-rf", "s3_extra"} {
		t.Run(name, func(t *testing.T) {
			_, err := m.FetchRecipe(name, nil)
			require.ErrorIs(t, err, ErrBadPackageName)
		})
	}

	// Add derives the name from its target on the implicit fetch path.
	require.ErrorIs(t, m.Add("..", &AddOptions{ImplicitFetch: true}), ErrBadPackageName)
}

func TestFetchRecipeWithoutDistURL(t *testing.T) {
	m, _ := New(newFakeBackend(), nil)
	_, err := m.FetchRecipe("s3", nil)
	require.ErrorIs(t, err, ErrNoDistURL)
}

func TestFetchRecipeHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	m, _ := New(newFakeBackend(), &Options{DistURL: srv.URL})
	_, err := m.FetchRecipe("s3", nil)
	require.Error(t, err)
}

func TestFetchBinaryThroughAdd(t *testing.T) {
	wantFile := (&Package{
		Name:            "s3",
		Version:         "v1.2.3",
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}).Filename()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "recipe.yaml"):
			io.WriteString(w, "name: s3\nversion: v1.2.3\n")
		case strings.HasSuffix(r.URL.Path, wantFile):
			io.WriteString(w, "PTARDATA")
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	be := newFakeBackend()
	m, _ := New(be, &Options{DistURL: srv.URL})

	// ImplicitFetch with a bare name (no .ptar) drives the recipe ->
	// binary fetch path.
	require.NoError(t, m.Add("s3", &AddOptions{ImplicitFetch: true}))
	require.Len(t, be.loaded, 1)
	require.Equal(t, "s3", be.loaded[0].Name)
	require.Equal(t, "v1.2.3", be.loaded[0].Version)
	require.Equal(t, "PTARDATA", string(be.loadData[wantFile]))
}

func TestFetchBinaryRejectsBadName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request for %q", r.URL.Path)
		http.Error(w, "unexpected", http.StatusNotFound)
	}))
	defer srv.Close()

	for _, name := range []string{"..", "-rf", "s3_extra"} {
		t.Run(name, func(t *testing.T) {
			be := newFakeBackend()
			m, _ := New(be, &Options{DistURL: srv.URL})

			err := m.Add(name, &AddOptions{ImplicitFetch: true, Version: "v1.2.3"})
			require.ErrorIs(t, err, ErrBadPackageName)
			require.Empty(t, be.loaded)
		})
	}
}

func TestAddRejectsShorthandVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request for %q", r.URL.Path)
		http.Error(w, "unexpected", http.StatusNotFound)
	}))
	defer srv.Close()

	for _, version := range []string{"v1", "v1.0", "v1.0.0+build.1"} {
		t.Run(version, func(t *testing.T) {
			be := newFakeBackend(pkgVer("s3", "v1.0.0"))
			m, _ := New(be, &Options{DistURL: srv.URL})

			err := m.Add("s3", &AddOptions{ImplicitFetch: true, Version: version, Replace: true})
			require.ErrorIs(t, err, ErrBadPackageName)
			require.Empty(t, be.unloaded)
		})
	}
}

func TestFetchRecipeRejectsMismatch(t *testing.T) {
	for _, name := range []string{"gcs", "../../evil", "-rf", ""} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "recipe.yaml") {
					t.Errorf("unexpected request for %q", r.URL.Path)
					http.Error(w, "unexpected", http.StatusNotFound)
					return
				}
				fmt.Fprintf(w, "name: %q\nversion: v1.2.3\n", name)
			}))
			defer srv.Close()

			be := newFakeBackend()
			m, _ := New(be, &Options{DistURL: srv.URL})

			_, err := m.FetchRecipe("s3", nil)
			require.ErrorIs(t, err, ErrRecipeMismatch)
			require.ErrorIs(t, m.Add("s3", &AddOptions{ImplicitFetch: true}), ErrRecipeMismatch)
			require.Empty(t, be.loaded)
		})
	}
}

func TestAddFromNonDefaultEdition(t *testing.T) {
	wantFile := (&Package{
		Name:            "s3",
		Version:         "v1.2.3",
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}).Filename()

	const root = "/dist/"
	dir := root + "devel/" + PLUGIN_API_VERSION + "/s3/"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case dir + "recipe.yaml":
			io.WriteString(w, "name: s3\nversion: v1.2.3\n")
		case dir + wantFile:
			io.WriteString(w, "PTARDATA")
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	origins := map[string]string{}
	be := newFakeBackend()
	m, _ := New(be, &Options{
		DistURL: srv.URL + root,
		Verifier: VerifierFunc(func(a *Artifact, rd io.Reader) error {
			origins[a.Filename] = a.Origin
			io.Copy(io.Discard, rd)
			return nil
		}),
	})

	require.NoError(t, m.Add("s3", &AddOptions{ImplicitFetch: true, Edition: "devel"}))

	wantOrigin := srv.URL + root + "devel"
	for _, name := range []string{"recipe.yaml", wantFile} {
		require.Equal(t, wantOrigin, origins[name], "%s origin", name)
	}
	require.Equal(t, "PTARDATA", string(be.loadData[wantFile]))
}

func TestFetchBinaryWithExplicitVersionSkipsRecipe(t *testing.T) {
	var recipeHit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "recipe.yaml") {
			recipeHit = true
			http.Error(w, "should not be called", http.StatusInternalServerError)
			return
		}
		io.WriteString(w, "PTARDATA")
	}))
	defer srv.Close()

	be := newFakeBackend()
	m, _ := New(be, &Options{DistURL: srv.URL})

	require.NoError(t, m.Add("s3", &AddOptions{ImplicitFetch: true, Version: "v3.0.0"}))
	require.False(t, recipeHit, "recipe endpoint was hit despite explicit Version")
	require.Len(t, be.loaded, 1)
	require.Equal(t, "v3.0.0", be.loaded[0].Version)
}

func TestFetchRequiresAuthWhenConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "PTARDATA")
	}))
	defer srv.Close()

	be := newFakeBackend()
	// BinaryNeedsAuth, but no RequestHook supplies a token -> the
	// Authorization header stays empty and the fetch is refused before
	// any request is made.
	m, _ := New(be, &Options{DistURL: srv.URL, BinaryNeedsAuth: true})
	err := m.Add("s3", &AddOptions{ImplicitFetch: true, Version: "v1.0.0"})
	require.ErrorIs(t, err, ErrAuthorizationRequired)
}

func TestWithBearer(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		io.WriteString(w, "PTARDATA")
	}))
	defer srv.Close()

	be := newFakeBackend()
	m, _ := New(be, &Options{
		DistURL:         srv.URL,
		BinaryNeedsAuth: true,
		RequestHook:     WithBearer(func() (string, error) { return "secrettoken", nil }),
	})
	require.NoError(t, m.Add("s3", &AddOptions{ImplicitFetch: true, Version: "v1.0.0"}))
	require.Equal(t, "Bearer secrettoken", seen)
}

func TestWithBearerEmptyTokenOmitsHeader(t *testing.T) {
	hook := WithBearer(func() (string, error) { return "", nil })
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	require.NoError(t, hook(req))
	require.Empty(t, req.Header.Get("Authorization"))
}

func TestWithBearerError(t *testing.T) {
	want := errors.New("token failure")
	hook := WithBearer(func() (string, error) { return "", want })
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	require.ErrorIs(t, hook(req), want)
}

func TestQueryOnlyLocal(t *testing.T) {
	be := newFakeBackend(pkgVer("s3", "v1.2.3"), pkgVer("ftp", "v0.1.0-beta.2"))
	m, _ := New(be, nil)

	got, err := m.Query(&QueryOptions{OnlyLocal: true})
	require.NoError(t, err)
	require.Len(t, got, 2)
	// results are sorted by name
	require.Equal(t, "ftp", got[0].Name)
	require.Equal(t, "s3", got[1].Name)
	for _, in := range got {
		require.Equal(t, "installed", in.Installation.Status, in.Name)
		// Missing from the catalog (no remote merge happens with
		// OnlyLocal), so the UI card still needs a version and stage:
		// they default to the installed package's own.
		require.Equal(t, in.Installation.Version, in.LatestVersion, in.Name)
	}
	require.Equal(t, "beta", got[0].Stage, "ftp: derived from its installed v0.1.0-beta.2")
	require.Equal(t, "stable", got[1].Stage, "s3: derived from its installed v1.2.3")
}

func TestQueryWithoutApiURL(t *testing.T) {
	m, _ := New(newFakeBackend(), nil)

	_, err := m.Query(nil)
	require.ErrorIs(t, err, ErrNoApiURL)
}

func TestQueryMergesRemoteIndex(t *testing.T) {
	const index = `{
		"version": "v1.0.0",
		"integrations": [
			{
				"name": "s3",
				"display_name": "Amazon S3",
				"edition": "community",
				"api": "v1.1.0",
				"version": "v2.0.0",
				"tags": ["cloud"],
				"connectors": [{"type": "storage"}]
			},
			{
				"name": "ftp",
				"display_name": "FTP",
				"edition": "community",
				"api": "v1.1.0",
				"version": "v0.5.0-beta.1",
				"connectors": [{"type": "importer"}, {"type": "exporter"}]
			},
			{
				"name": "wrongapi",
				"edition": "community",
				"api": "v0.0.1",
				"version": "v1.0.0"
			},
			{
				"name": "enterprise-only",
				"edition": "enterprise",
				"api": "v1.1.0",
				"version": "v1.0.0"
			}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, index)
	}))
	defer srv.Close()

	// s3 is installed locally, at a prerelease version, so its local
	// default stage ("devel") differs from the catalog's ("stable");
	// ftp is only remote.
	be := newFakeBackend(pkgVer("s3", "v1.2.3-devel.1"))
	m, _ := New(be, &Options{ApiURL: srv.URL})

	got, err := m.Query(nil)
	require.NoError(t, err)

	byName := map[string]*Integration{}
	for _, in := range got {
		byName[in.Name] = in
	}

	// wrong api version and non-community edition are filtered out.
	require.NotContains(t, byName, "wrongapi", "should be filtered by API version")
	require.NotContains(t, byName, "enterprise-only", "should be filtered by edition")

	require.Contains(t, byName, "s3")
	s3 := byName["s3"]
	require.Equal(t, "installed", s3.Installation.Status)
	require.Equal(t, "v1.2.3-devel.1", s3.Installation.Version)
	require.Equal(t, "v2.0.0", s3.LatestVersion, "the catalog's, overwriting the local default")
	require.Equal(t, "Amazon S3", s3.DisplayName, "merged from index")
	require.True(t, s3.Installation.Available)
	require.True(t, s3.Types.Storage, "from its storage connector")
	require.Equal(t, "stable", s3.Stage, "the catalog's, overwriting the local default of devel")

	require.Contains(t, byName, "ftp")
	ftp := byName["ftp"]
	require.Equal(t, "not-installed", ftp.Installation.Status)
	require.Equal(t, "beta", ftp.Stage)
	require.True(t, ftp.Types.Source)
	require.True(t, ftp.Types.Destination)
}

func TestQueryFetchesIndexFromIndexURL(t *testing.T) {
	const index = `{
		"version": "v1.0.0",
		"integrations": [
			{
				"name": "s3",
				"edition": "community",
				"api": "v1.1.0",
				"version": "v2.0.0"
			}
		]
	}`

	// A mirror of the distribution tree serves the index at its root.
	var path string
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		io.WriteString(w, index)
	}))
	defer mirror.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the api must not be consulted", http.StatusTeapot)
	}))
	defer api.Close()

	m, _ := New(newFakeBackend(), &Options{
		ApiURL:   api.URL,
		IndexURL: mirror.URL + "/integrations-" + PLUGIN_BUNDLE_VERSION + ".json",
	})

	got, err := m.Query(nil)
	require.NoError(t, err)
	require.Equal(t, "/integrations-"+PLUGIN_BUNDLE_VERSION+".json", path, "index fetched from the mirror root")
	require.Len(t, got, 1)
	require.Equal(t, "s3", got[0].Name)
}

func TestStageOf(t *testing.T) {
	cases := map[string]string{
		"v1.0.0":         "stable",
		"v1.0.0-devel.1": "devel",
		"v1.0.0-beta.2":  "beta",
		"v1.0.0-rc.1":    "testing",
		"v1.0.0-alpha.1": "-alpha.1",
	}
	for version, want := range cases {
		if got := stageOf(version); got != want {
			t.Errorf("stageOf(%q) = %q, want %q", version, got, want)
		}
	}
}

func TestQueryStageDerivation(t *testing.T) {
	cases := map[string]string{
		"v1.0.0":         "stable",
		"v1.0.0-devel.3": "devel",
		"v1.0.0-beta.1":  "beta",
		"v1.0.0-rc.2":    "testing",
		"v1.0.0-foo":     "-foo",
	}
	for version, wantStage := range cases {
		index := fmt.Sprintf(`{"version":"v1","integrations":[{"name":"x","edition":"community","api":%q,"version":%q}]}`,
			PLUGIN_API_VERSION, version)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, index)
		}))
		m, _ := New(newFakeBackend(), &Options{ApiURL: srv.URL})
		got, err := m.Query(nil)
		srv.Close()
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, wantStage, got[0].Stage, "version %s", version)
	}
}

func TestQueryTypeFilter(t *testing.T) {
	const index = `{
		"version":"v1",
		"integrations":[
			{"name":"s3","edition":"community","api":"v1.1.0","version":"v1.0.0","connectors":[{"type":"storage"}]},
			{"name":"src","edition":"community","api":"v1.1.0","version":"v1.0.0","connectors":[{"type":"importer"}]},
			{"name":"dst","edition":"community","api":"v1.1.0","version":"v1.0.0","connectors":[{"type":"exporter"}]}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, index)
	}))
	defer srv.Close()

	m, _ := New(newFakeBackend(), &Options{ApiURL: srv.URL})

	for _, tt := range []struct {
		typ  string
		want string
	}{
		{"storage", "s3"},
		{"source", "src"},
		{"destination", "dst"},
	} {
		got, err := m.Query(&QueryOptions{Type: tt.typ})
		require.NoError(t, err)
		require.Len(t, got, 1, "type=%s", tt.typ)
		require.Equal(t, tt.want, got[0].Name, "type=%s", tt.typ)
	}
}

func TestQueryTagAndStatusFilter(t *testing.T) {
	const index = `{
		"version":"v1",
		"integrations":[
			{"name":"a","edition":"community","api":"v1.1.0","version":"v1.0.0","tags":["cloud"]},
			{"name":"b","edition":"community","api":"v1.1.0","version":"v1.0.0","tags":["local"]}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, index)
	}))
	defer srv.Close()

	m, _ := New(newFakeBackend(), &Options{ApiURL: srv.URL})

	got, err := m.Query(&QueryOptions{Tag: "cloud"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "a", got[0].Name)

	// nothing is installed, so status=installed should match nothing.
	got, err = m.Query(&QueryOptions{Status: "installed"})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestQueryAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	m, _ := New(newFakeBackend(), &Options{ApiURL: srv.URL})
	_, err := m.Query(nil)
	require.Error(t, err)
}

// testManifest builds the manifest used by the local-manifest-fallback
// tests below: an importer with a JSON schema validator, an exporter
// without one, and a README.md, all written under a temporary directory
// that stands in for where the package was extracted.
func testManifest(t *testing.T) (*Manifest, string) {
	t.Helper()

	dir := t.TempDir()

	schemaDir := filepath.Join(dir, "plugin", "importer")
	if err := os.MkdirAll(schemaDir, 0755); err != nil {
		t.Fatal(err)
	}
	const schema = `{"type":"object","properties":{"endpoint":{"type":"string"}}}`
	if err := os.WriteFile(filepath.Join(schemaDir, "schema.json"), []byte(schema), 0644); err != nil {
		t.Fatal(err)
	}

	const readme = "# Talos Linux\n\nA Talos integration.\n"
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0644); err != nil {
		t.Fatal(err)
	}

	m := &Manifest{
		Name:        "talos",
		DisplayName: "Talos Linux",
		Description: "Talos Linux cluster integration",
		Homepage:    "https://talos.dev",
		License:     "MPL-2.0",
		Tags:        []string{"kubernetes", "linux"},
		Connectors: []ManifestConnector{
			{
				Type:      ConnectorTypeImporter,
				Protocols: []string{"talos"},
				Validator: "./plugin/importer/schema.json",
			},
			{
				Type:      ConnectorTypeExporter,
				Protocols: []string{"talos"},
			},
		},
	}

	return m, dir
}

func TestQueryLocalManifestFillsMetadata(t *testing.T) {
	m, dir := testManifest(t)

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
	in := got[0]

	if in.DisplayName != "Talos Linux" {
		t.Errorf("DisplayName = %q, want Talos Linux", in.DisplayName)
	}
	if in.Description != "Talos Linux cluster integration" {
		t.Errorf("Description = %q", in.Description)
	}
	if in.Homepage != "https://talos.dev" {
		t.Errorf("Homepage = %q", in.Homepage)
	}
	if in.License != "MPL-2.0" {
		t.Errorf("License = %q", in.License)
	}
	if !slices.Equal(in.Tags, []string{"kubernetes", "linux"}) {
		t.Errorf("Tags = %v", in.Tags)
	}
	if !in.Types.Source {
		t.Error("Types.Source = false, want true (importer present)")
	}
	if !in.Types.Destination {
		t.Error("Types.Destination = false, want true (exporter present)")
	}
	if len(in.Connectors) != 2 {
		t.Fatalf("len(Connectors) = %d, want 2", len(in.Connectors))
	}
	if len(in.Connectors[0].Protocols) != 1 || in.Connectors[0].Protocols[0].Scheme != "talos" {
		t.Errorf("Connectors[0].Protocols = %+v, want [{talos}]", in.Connectors[0].Protocols)
	}
	if in.Connectors[0].Validator == nil {
		t.Error("Connectors[0].Validator = nil, want the decoded schema")
	} else if v, ok := in.Connectors[0].Validator.(map[string]any); !ok || v["type"] != "object" {
		t.Errorf("Connectors[0].Validator = %+v, want decoded JSON schema", in.Connectors[0].Validator)
	}
	if want := "# Talos Linux\n\nA Talos integration.\n"; in.Documentation != want {
		t.Errorf("Documentation = %q, want %q", in.Documentation, want)
	}
}

func TestQueryRemoteOverridesLocalManifest(t *testing.T) {
	m, dir := testManifest(t)

	// A local icon must not leak through either: the remote catalog's
	// Icon (an https URL) always wins over the data URI built from it.
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "icon.svg"), []byte("<svg></svg>"), 0644); err != nil {
		t.Fatal(err)
	}

	be := newFakeBackend(pkgVer("talos", "v1.0.0"))
	be.manifests["talos"] = m
	be.manifestDirs["talos"] = dir

	const index = `{
		"version": "v1.0.0",
		"integrations": [
			{
				"name": "talos",
				"display_name": "Talos (official)",
				"edition": "community",
				"api": "v1.1.0",
				"version": "v1.0.0",
				"connectors": [{"type": "storage"}],
				"icon": "https://cdn.example.com/talos/icon.svg"
			}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, index)
	}))
	defer srv.Close()

	mgr, _ := New(be, &Options{ApiURL: srv.URL})
	got, err := mgr.Query(nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("Query returned %d results, want 1", len(got))
	}
	in := got[0]
	if in.DisplayName != "Talos (official)" {
		t.Errorf("DisplayName = %q, want the remote value to win", in.DisplayName)
	}
	if len(in.Connectors) != 1 || in.Connectors[0].Type != "storage" {
		t.Errorf("Connectors = %+v, want the remote connectors to win", in.Connectors)
	}
	// The remote fixture sets neither description nor documentation, and it
	// must win entirely: the local manifest's values must not leak through.
	if in.Description != "" {
		t.Errorf("Description = %q, want empty (remote has none, must not leak the local value)", in.Description)
	}
	if in.Documentation != "" {
		t.Errorf("Documentation = %q, want empty (remote has none, must not leak the local README)", in.Documentation)
	}
	if want := "https://cdn.example.com/talos/icon.svg"; in.Icon != want {
		t.Errorf("Icon = %q, want %q (the remote value must win over the local data URI)", in.Icon, want)
	}
}

func TestQueryLocalManifestErrorIgnored(t *testing.T) {
	be := newFakeBackend(pkgVer("talos", "v1.0.0"))
	be.manifestErr = errors.New("manifest unreadable")

	mgr, _ := New(be, nil)
	got, err := mgr.Query(&QueryOptions{OnlyLocal: true})
	if err != nil {
		t.Fatalf("Query: %v, want it to succeed despite the manifest error", err)
	}
	if len(got) != 1 {
		t.Fatalf("Query returned %d results, want 1", len(got))
	}
	if got[0].DisplayName != got[0].Name {
		t.Errorf("DisplayName = %q, want %q (fallback to Name)", got[0].DisplayName, got[0].Name)
	}
}

func TestQueryLocalManifestMissingSchema(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Name:        "talos",
		DisplayName: "Talos Linux",
		Connectors: []ManifestConnector{
			{
				Type:      ConnectorTypeImporter,
				Protocols: []string{"talos"},
				Validator: "./plugin/importer/missing-schema.json",
			},
		},
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
	if got[0].Connectors[0].Validator != nil {
		t.Errorf("Validator = %+v, want nil for a missing schema file", got[0].Connectors[0].Validator)
	}
	if got[0].DisplayName != "Talos Linux" {
		t.Errorf("DisplayName = %q, want Talos Linux (rest of the manifest still applies)", got[0].DisplayName)
	}
}

// TestQueryLocalManifestDefaultsWhenUnset guards against a manifest with
// no display_name or tags clobbering the defaults Query already filled in:
// DisplayName must fall back to the package name (not become ""), and Tags
// must stay a non-nil slice (not become a JSON null).
func TestQueryLocalManifestDefaultsWhenUnset(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Name: "talos",
		Connectors: []ManifestConnector{
			{Type: ConnectorTypeImporter, Protocols: []string{"talos"}},
		},
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
	in := got[0]
	if in.DisplayName != in.Name {
		t.Errorf("DisplayName = %q, want %q (fallback to Name when the manifest has none)", in.DisplayName, in.Name)
	}
	if in.Tags == nil {
		t.Error("Tags = nil, want a non-nil slice when the manifest has none")
	}
}

// TestQueryLocalManifestSymlinkEscapeIgnored guards against a malicious or
// broken package using a symlink to make Query read a file outside its
// extracted directory (e.g. README.md -> ~/.ssh/id_ed25519). Both the
// README and the connector's validator point through such a symlink here;
// Query must ignore them rather than serve their target's content.
func TestQueryLocalManifestSymlinkEscapeIgnored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs elevated privileges on windows")
	}

	// Valid JSON so that, were the escape not blocked, it would also
	// leak through the connector's validator (json.Unmarshal would
	// otherwise reject it and Validator would end up nil either way,
	// hiding a broken containment check).
	const secretContent = `{"leaked":"SECRET"}`

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte(secretContent), 0600); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.Symlink(secret, filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}

	schemaDir := filepath.Join(dir, "plugin", "importer")
	if err := os.MkdirAll(schemaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(schemaDir, "schema.json")); err != nil {
		t.Fatal(err)
	}

	m := &Manifest{
		Name:        "talos",
		DisplayName: "Talos Linux",
		Connectors: []ManifestConnector{
			{
				Type:      ConnectorTypeImporter,
				Protocols: []string{"talos"},
				Validator: "./plugin/importer/schema.json",
			},
		},
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
	in := got[0]
	if in.Documentation != "" {
		t.Errorf("Documentation = %q, want empty (README.md escapes the package directory)", in.Documentation)
	}
	if len(in.Connectors) != 1 {
		t.Fatalf("len(Connectors) = %d, want 1", len(in.Connectors))
	}
	if in.Connectors[0].Validator != nil {
		t.Errorf("Connectors[0].Validator = %+v, want nil (schema escapes the package directory)", in.Connectors[0].Validator)
	}
}

func TestQueryLocalManifestIconSVG(t *testing.T) {
	m, dir := testManifest(t)

	const svg = `<svg xmlns="http://www.w3.org/2000/svg"></svg>`
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "icon.svg"), []byte(svg), 0644); err != nil {
		t.Fatal(err)
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

	want := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
	if got[0].Icon != want {
		t.Errorf("Icon = %q, want %q", got[0].Icon, want)
	}
}

func TestQueryLocalManifestIconPNGFallback(t *testing.T) {
	m, dir := testManifest(t)

	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "icon.png"), png, 0644); err != nil {
		t.Fatal(err)
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

	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	if got[0].Icon != want {
		t.Errorf("Icon = %q, want %q (fall back to icon.png when there is no icon.svg)", got[0].Icon, want)
	}
}

func TestQueryLocalManifestIconSVGPreferredOverPNG(t *testing.T) {
	m, dir := testManifest(t)

	const svg = `<svg></svg>`
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "icon.svg"), []byte(svg), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "icon.png"), []byte{0x89, 'P', 'N', 'G'}, 0644); err != nil {
		t.Fatal(err)
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

	want := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
	if got[0].Icon != want {
		t.Errorf("Icon = %q, want the svg to win over the png", got[0].Icon)
	}
}

// TestQueryLocalManifestIconOversizedSkipped guards against serving a
// truncated, broken image: an icon over maxLocalIconSize must be skipped
// entirely (Icon left empty), never cut short like readDocumentation or
// readValidator would.
func TestQueryLocalManifestIconOversizedSkipped(t *testing.T) {
	m, dir := testManifest(t)

	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	big := bytes.Repeat([]byte("a"), maxLocalIconSize+1)
	if err := os.WriteFile(filepath.Join(dir, "assets", "icon.svg"), big, 0644); err != nil {
		t.Fatal(err)
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

	if got[0].Icon != "" {
		t.Errorf("Icon = %q, want empty (an oversized icon must be skipped, not truncated)", got[0].Icon)
	}
}

// TestQueryLocalManifestIconSymlinkEscapeIgnored guards against a symlink
// at assets/icon.svg making Query serve up a file outside the package's
// extracted directory, the same way TestQueryLocalManifestSymlinkEscapeIgnored
// does for README.md and a connector's validator.
func TestQueryLocalManifestIconSymlinkEscapeIgnored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs elevated privileges on windows")
	}

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.svg")
	if err := os.WriteFile(secret, []byte(`<svg>SECRET</svg>`), 0600); err != nil {
		t.Fatal(err)
	}

	m, dir := testManifest(t)
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "assets", "icon.svg")); err != nil {
		t.Fatal(err)
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

	if got[0].Icon != "" {
		t.Errorf("Icon = %q, want empty (assets/icon.svg escapes the package directory)", got[0].Icon)
	}
}
