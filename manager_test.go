package pkg

import (
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// fakeBackend is an in-memory Backend implementation for exercising the
// Manager orchestration logic without touching disk, ptar archives, or
// the kloset storage engine.
type fakeBackend struct {
	pkgs     []*Package
	loaded   []*Package // packages passed to Load, with their bytes
	loadData map[string][]byte
	unloaded []*Package

	listErr   error
	loadErr   error
	unloadErr error
}

func newFakeBackend(pkgs ...*Package) *fakeBackend {
	return &fakeBackend{pkgs: pkgs, loadData: map[string][]byte{}}
}

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

func (f *fakeBackend) Load(p *Package, rd io.Reader) error {
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
	f.pkgs = append(f.pkgs, &cp)
	return nil
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
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	wantUA := fmt.Sprintf("pkg/v0.0.1 (%s/%s)", runtime.GOOS, runtime.GOARCH)
	if m.useragent != wantUA {
		t.Errorf("useragent = %q, want %q", m.useragent, wantUA)
	}
}

func TestNewManagerCustomUserAgent(t *testing.T) {
	m, err := New(newFakeBackend(), &Options{UserAgent: "myapp/1.0"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	wantUA := fmt.Sprintf("myapp/1.0 (%s/%s)", runtime.GOOS, runtime.GOARCH)
	if m.useragent != wantUA {
		t.Errorf("useragent = %q, want %q", m.useragent, wantUA)
	}
}

func TestNewManagerInvalidURLs(t *testing.T) {
	if _, err := New(newFakeBackend(), &Options{InstallURL: "://bad"}); err == nil {
		t.Error("expected error for bad InstallURL")
	}
	if _, err := New(newFakeBackend(), &Options{ApiURL: "://bad"}); err == nil {
		t.Error("expected error for bad ApiURL")
	}
}

func TestManagerList(t *testing.T) {
	be := newFakeBackend(pkgOf(t, "s3"), pkgOf(t, "ftp"))
	m, _ := New(be, nil)

	var names []string
	for p, err := range m.List() {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, p.Name)
	}
	if !slices.Equal(names, []string{"s3", "ftp"}) {
		t.Errorf("List() = %v", names)
	}
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
			if !errors.Is(err, ErrInvalidOptions) {
				t.Errorf("Add err = %v, want ErrInvalidOptions", err)
			}
		})
	}
}

func TestAddRejectsBadName(t *testing.T) {
	m, _ := New(newFakeBackend(), nil)
	err := m.Add("not-a-package", nil)
	if !errors.Is(err, ErrBadPackageName) {
		t.Errorf("Add err = %v, want ErrBadPackageName", err)
	}
}

func TestAddRejectsOSArchMismatch(t *testing.T) {
	be := newFakeBackend()
	m, _ := New(be, nil)

	// A package for an OS that is definitely not the host.
	bad := "s3_v1.0.0_plan9_sparc64.ptar"
	err := m.Add(bad, nil)
	if !errors.Is(err, ErrBadOSArch) {
		t.Errorf("Add err = %v, want ErrBadOSArch", err)
	}
	if len(be.loaded) != 0 {
		t.Errorf("backend Load called %d times, want 0", len(be.loaded))
	}
}

func TestAddAllowOSArchMismatchStillFailsOnMissingFile(t *testing.T) {
	be := newFakeBackend()
	m, _ := New(be, nil)

	// With the mismatch allowed, the OS/arch gate is skipped and we
	// proceed to open the (nonexistent) file, which should error.
	err := m.Add("s3_v1.0.0_plan9_sparc64.ptar", &AddOptions{AllowOSArchMismatch: true})
	if err == nil {
		t.Fatal("expected error opening missing file")
	}
	if errors.Is(err, ErrBadOSArch) {
		t.Error("got ErrBadOSArch, but mismatch was allowed")
	}
}

func TestPreaddNoExistingVersion(t *testing.T) {
	be := newFakeBackend() // empty
	m, _ := New(be, nil)
	if err := m.preadd("s3", "v1.0.0", &AddOptions{}); err != nil {
		t.Errorf("preadd with no existing version: %v", err)
	}
	if len(be.unloaded) != 0 {
		t.Errorf("unloaded %d, want 0", len(be.unloaded))
	}
}

func TestPreaddAlreadyInstalledDefault(t *testing.T) {
	be := newFakeBackend(pkgVer("s3", "v1.0.0"))
	m, _ := New(be, nil)
	err := m.preadd("s3", "v1.0.0", &AddOptions{})
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Errorf("preadd err = %v, want ErrAlreadyInstalled", err)
	}
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
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("preadd err = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Errorf("preadd err = %v, want nil", err)
			}
			gotUnload := len(be.unloaded) > 0
			if gotUnload != tt.wantUnload {
				t.Errorf("unloaded = %v, want %v", gotUnload, tt.wantUnload)
			}
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
			if err := m.preadd("s3", "v1.5.0", &AddOptions{Replace: true}); err != nil {
				t.Errorf("preadd with Replace (installed %s -> v1.5.0) = %v, want nil", installed, err)
			}
			if len(be.unloaded) != 1 {
				t.Errorf("Replace unloaded %d existing packages, want 1", len(be.unloaded))
			}
		})
	}
}

func TestPreaddAllowMultipleVersions(t *testing.T) {
	be := newFakeBackend(pkgVer("s3", "v1.0.0"))
	m, _ := New(be, nil)

	// A different version is fine and does not unload the existing one.
	if err := m.preadd("s3", "v2.0.0", &AddOptions{AllowMultipleVersions: true}); err != nil {
		t.Errorf("preadd v2: %v", err)
	}
	if len(be.unloaded) != 0 {
		t.Errorf("unloaded %d, want 0 with AllowMultipleVersions", len(be.unloaded))
	}

	// The same version, however, is still rejected.
	if err := m.preadd("s3", "v1.0.0", &AddOptions{AllowMultipleVersions: true}); !errors.Is(err, ErrAlreadyInstalled) {
		t.Errorf("preadd same version err = %v, want ErrAlreadyInstalled", err)
	}
}

func TestPreaddPropagatesListError(t *testing.T) {
	be := newFakeBackend()
	be.listErr = errors.New("boom")
	m, _ := New(be, nil)
	if err := m.preadd("s3", "v1.0.0", &AddOptions{}); err == nil {
		t.Fatal("expected list error to propagate")
	}
}

func TestDelRequiresTargetOrAll(t *testing.T) {
	m, _ := New(newFakeBackend(pkgOf(t, "s3")), nil)
	if err := m.Del("", nil); !errors.Is(err, ErrBadPackageName) {
		t.Errorf("Del empty target err = %v, want ErrBadPackageName", err)
	}
}

func TestDelByName(t *testing.T) {
	be := newFakeBackend(pkgOf(t, "s3"), pkgOf(t, "ftp"))
	m, _ := New(be, nil)
	if err := m.Del("s3", nil); err != nil {
		t.Fatalf("Del: %v", err)
	}
	if len(be.unloaded) != 1 || be.unloaded[0].Name != "s3" {
		t.Errorf("unloaded = %v, want [s3]", be.unloaded)
	}
}

func TestDelAll(t *testing.T) {
	be := newFakeBackend(pkgOf(t, "s3"), pkgOf(t, "ftp"))
	m, _ := New(be, nil)
	if err := m.Del("", &DelOptions{All: true}); err != nil {
		t.Fatalf("Del all: %v", err)
	}
	if len(be.unloaded) != 2 {
		t.Errorf("unloaded %d, want 2", len(be.unloaded))
	}
}

func TestFetchRecipe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/" + PLUGIN_API_VERSION + "/s3/recipe.yaml"
		if r.URL.Path != wantPath {
			t.Errorf("request path = %q, want %q", r.URL.Path, wantPath)
		}
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "pkg/") {
			t.Errorf("missing/unexpected User-Agent: %q", ua)
		}
		io.WriteString(w, "name: s3\nversion: v1.2.3\n")
	}))
	defer srv.Close()

	m, _ := New(newFakeBackend(), &Options{InstallURL: srv.URL})
	r, err := m.FetchRecipe("s3")
	if err != nil {
		t.Fatalf("FetchRecipe: %v", err)
	}
	if r.Name != "s3" || r.Version != "v1.2.3" {
		t.Errorf("recipe = %+v", r)
	}
}

func TestFetchRecipeHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	m, _ := New(newFakeBackend(), &Options{InstallURL: srv.URL})
	if _, err := m.FetchRecipe("s3"); err == nil {
		t.Fatal("expected error for 404 recipe")
	}
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
	m, _ := New(be, &Options{InstallURL: srv.URL})

	// ImplicitFetch with a bare name (no .ptar) drives the recipe ->
	// binary fetch path.
	if err := m.Add("s3", &AddOptions{ImplicitFetch: true}); err != nil {
		t.Fatalf("Add (implicit fetch): %v", err)
	}
	if len(be.loaded) != 1 {
		t.Fatalf("backend Load called %d times, want 1", len(be.loaded))
	}
	got := be.loaded[0]
	if got.Name != "s3" || got.Version != "v1.2.3" {
		t.Errorf("loaded package = %+v", got)
	}
	if string(be.loadData[wantFile]) != "PTARDATA" {
		t.Errorf("loaded data = %q, want PTARDATA", be.loadData[wantFile])
	}
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
	m, _ := New(be, &Options{InstallURL: srv.URL})

	if err := m.Add("s3", &AddOptions{ImplicitFetch: true, Version: "v3.0.0"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if recipeHit {
		t.Error("recipe endpoint was hit despite explicit Version")
	}
	if len(be.loaded) != 1 || be.loaded[0].Version != "v3.0.0" {
		t.Errorf("loaded = %+v", be.loaded)
	}
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
	m, _ := New(be, &Options{InstallURL: srv.URL, BinaryNeedsAuth: true})
	err := m.Add("s3", &AddOptions{ImplicitFetch: true, Version: "v1.0.0"})
	if !errors.Is(err, ErrAuthorizationRequired) {
		t.Errorf("Add err = %v, want ErrAuthorizationRequired", err)
	}
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
		InstallURL:      srv.URL,
		BinaryNeedsAuth: true,
		RequestHook:     WithBearer(func() (string, error) { return "secrettoken", nil }),
	})
	if err := m.Add("s3", &AddOptions{ImplicitFetch: true, Version: "v1.0.0"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if seen != "Bearer secrettoken" {
		t.Errorf("Authorization = %q, want Bearer secrettoken", seen)
	}
}

func TestWithBearerEmptyTokenOmitsHeader(t *testing.T) {
	hook := WithBearer(func() (string, error) { return "", nil })
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	if err := hook(req); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want empty", got)
	}
}

func TestWithBearerError(t *testing.T) {
	want := errors.New("token failure")
	hook := WithBearer(func() (string, error) { return "", want })
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	if err := hook(req); !errors.Is(err, want) {
		t.Errorf("hook err = %v, want %v", err, want)
	}
}

func TestQueryOnlyLocal(t *testing.T) {
	be := newFakeBackend(pkgVer("s3", "v1.2.3"), pkgVer("ftp", "v0.1.0"))
	m, _ := New(be, nil)

	got, err := m.Query(&QueryOptions{OnlyLocal: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Query returned %d, want 2", len(got))
	}
	// results are sorted by name
	if got[0].Name != "ftp" || got[1].Name != "s3" {
		t.Errorf("order = %q, %q; want ftp, s3", got[0].Name, got[1].Name)
	}
	for _, in := range got {
		if in.Installation.Status != "installed" {
			t.Errorf("%s status = %q, want installed", in.Name, in.Installation.Status)
		}
	}
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

	// s3 is installed locally; ftp is only remote.
	be := newFakeBackend(pkgVer("s3", "v1.2.3"))
	m, _ := New(be, &Options{ApiURL: srv.URL})

	got, err := m.Query(nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	byName := map[string]*Integration{}
	for _, in := range got {
		byName[in.Name] = in
	}

	// wrong api version and non-community edition are filtered out.
	if _, ok := byName["wrongapi"]; ok {
		t.Error("wrongapi should be filtered by API version")
	}
	if _, ok := byName["enterprise-only"]; ok {
		t.Error("enterprise-only should be filtered by edition")
	}

	s3, ok := byName["s3"]
	if !ok {
		t.Fatal("s3 missing from results")
	}
	if s3.Installation.Status != "installed" {
		t.Errorf("s3 status = %q, want installed", s3.Installation.Status)
	}
	if s3.Installation.Version != "v1.2.3" {
		t.Errorf("s3 installed version = %q, want v1.2.3", s3.Installation.Version)
	}
	if s3.LatestVersion != "v2.0.0" {
		t.Errorf("s3 latest = %q, want v2.0.0", s3.LatestVersion)
	}
	if s3.DisplayName != "Amazon S3" {
		t.Errorf("s3 display name = %q, want merged from index", s3.DisplayName)
	}
	if !s3.Installation.Available {
		t.Error("s3 should be marked available")
	}
	if !s3.Types.Storage {
		t.Error("s3 should have Types.Storage from its storage connector")
	}
	if s3.Stage != "stable" {
		t.Errorf("s3 stage = %q, want stable (no prerelease)", s3.Stage)
	}

	ftp, ok := byName["ftp"]
	if !ok {
		t.Fatal("ftp missing from results")
	}
	if ftp.Installation.Status != "not-installed" {
		t.Errorf("ftp status = %q, want not-installed", ftp.Installation.Status)
	}
	if ftp.Stage != "beta" {
		t.Errorf("ftp stage = %q, want beta", ftp.Stage)
	}
	if !ftp.Types.Source || !ftp.Types.Destination {
		t.Errorf("ftp types = %+v, want source+destination", ftp.Types)
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
		if err != nil {
			t.Fatalf("Query(%s): %v", version, err)
		}
		if len(got) != 1 {
			t.Fatalf("version %s: got %d results, want 1", version, len(got))
		}
		if got[0].Stage != wantStage {
			t.Errorf("version %s: stage = %q, want %q", version, got[0].Stage, wantStage)
		}
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
		if err != nil {
			t.Fatalf("Query type=%s: %v", tt.typ, err)
		}
		if len(got) != 1 || got[0].Name != tt.want {
			names := make([]string, len(got))
			for i, g := range got {
				names[i] = g.Name
			}
			t.Errorf("Query type=%s = %v, want [%s]", tt.typ, names, tt.want)
		}
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
	if err != nil {
		t.Fatalf("Query tag: %v", err)
	}
	if len(got) != 1 || got[0].Name != "a" {
		t.Errorf("Query tag=cloud returned wrong set: %+v", got)
	}

	// nothing is installed, so status=installed should match nothing.
	got, err = m.Query(&QueryOptions{Status: "installed"})
	if err != nil {
		t.Fatalf("Query status: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Query status=installed returned %d, want 0", len(got))
	}
}

func TestQueryAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	m, _ := New(newFakeBackend(), &Options{ApiURL: srv.URL})
	if _, err := m.Query(nil); err == nil {
		t.Fatal("expected error when API returns 500")
	}
}
