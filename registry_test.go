package pkg

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// indexServer serves an integrations index holding the given entries at
// the location registries are expected to serve it from, and 404 elsewhere.
func indexServer(t *testing.T, entries ...Integration) *httptest.Server {
	t.Helper()
	body, err := json.Marshal(IntegrationIndex{Version: "v1.0.0", Integrations: entries})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/integrations-"+PLUGIN_BUNDLE_VERSION+".json") {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// entry is a community index entry for the current plugin API.
func entry(name, description string) Integration {
	return Integration{
		Name:        name,
		DisplayName: strings.ToUpper(name),
		Description: description,
		Edition:     "community",
		API:         PLUGIN_API_VERSION,
		Version:     "v1.0.0",
	}
}

func byName(ins []*Integration) map[string]*Integration {
	ret := map[string]*Integration{}
	for _, in := range ins {
		ret[in.Name] = in
	}
	return ret
}

func TestQueryAllMergesRegistries(t *testing.T) {
	official := indexServer(t, entry("s3", "official"))
	lab := indexServer(t, entry("talos", "lab"))
	other := indexServer(t, entry("gitlab", "other"))

	m, err := New(newFakeBackend(), &Options{
		ApiURL: official.URL,
		Registries: []Registry{
			{Name: "lab", URL: lab.URL},
			{Name: "other", URL: other.URL + "/some/path/"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := m.QueryAll(nil)
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %q, want none", res.Warnings)
	}

	got := byName(res.Integrations)
	for name, reg := range map[string]string{"s3": "", "talos": "lab", "gitlab": "other"} {
		in, ok := got[name]
		if !ok {
			t.Errorf("%s missing from results", name)
			continue
		}
		if in.Registry != reg {
			t.Errorf("%s registry = %q, want %q", name, in.Registry, reg)
		}
		if in.Installation.Status != "not-installed" || !in.Installation.Available {
			t.Errorf("%s installation = %+v, want available and not-installed", name, in.Installation)
		}
		if in.Id != name || in.Stage != "stable" || in.LatestVersion != "v1.0.0" {
			t.Errorf("%s not normalised: id %q stage %q latest %q", name, in.Id, in.Stage, in.LatestVersion)
		}
	}
	if len(res.Integrations) != 3 {
		t.Errorf("got %d integrations, want 3", len(res.Integrations))
	}

	// Query is QueryAll without the warnings.
	q, err := m.Query(nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(q) != 3 {
		t.Errorf("Query returned %d integrations, want 3", len(q))
	}
}

func TestQueryAllOfficialWinsOnNameClash(t *testing.T) {
	official := indexServer(t, entry("s3", "official"))
	lab := indexServer(t, entry("s3", "lab"), entry("talos", "lab"))
	other := indexServer(t, entry("s3", "other"), entry("talos", "other"))

	m, err := New(newFakeBackend(), &Options{
		ApiURL: official.URL,
		Registries: []Registry{
			{Name: "lab", URL: lab.URL},
			{Name: "other", URL: other.URL},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := m.QueryAll(nil)
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	got := byName(res.Integrations)

	s3 := got["s3"]
	if s3 == nil || s3.Description != "official" || s3.Registry != "" {
		t.Fatalf("s3 = %+v, want the official entry", s3)
	}

	// Between registries, the first declared wins.
	talos := got["talos"]
	if talos == nil || talos.Description != "lab" || talos.Registry != "lab" {
		t.Fatalf("talos = %+v, want lab's entry", talos)
	}

	want := []string{
		"registry lab: integration s3 shadowed by official",
		"registry other: integration s3 shadowed by official",
		"registry other: integration talos shadowed by lab",
	}
	if !slices.Equal(res.Warnings, want) {
		t.Errorf("warnings = %q, want %q", res.Warnings, want)
	}
}

func TestQueryAllInstalledShadowsRegistry(t *testing.T) {
	official := indexServer(t, entry("s3", "official"))
	lab := indexServer(t, entry("s3", "lab"))

	m, err := New(newFakeBackend(pkgVer("s3", "v0.9.0")), &Options{
		ApiURL:     official.URL,
		Registries: []Registry{{Name: "lab", URL: lab.URL}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := m.QueryAll(nil)
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	s3 := byName(res.Integrations)["s3"]
	if s3 == nil {
		t.Fatal("s3 missing from results")
	}
	if s3.Installation.Status != "installed" || s3.Description != "official" || s3.Registry != "" {
		t.Errorf("s3 = status %q description %q registry %q, want installed, official, empty",
			s3.Installation.Status, s3.Description, s3.Registry)
	}
	want := []string{"registry lab: integration s3 shadowed by official"}
	if !slices.Equal(res.Warnings, want) {
		t.Errorf("warnings = %q, want %q", res.Warnings, want)
	}
}

func TestQueryAllRegistryDuplicateEntry(t *testing.T) {
	official := indexServer(t)
	lab := indexServer(t, entry("talos", "first"), entry("talos", "second"))

	m, err := New(newFakeBackend(), &Options{
		ApiURL:     official.URL,
		Registries: []Registry{{Name: "lab", URL: lab.URL}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := m.QueryAll(nil)
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if len(res.Integrations) != 1 {
		t.Fatalf("got %d integrations, want 1", len(res.Integrations))
	}
	talos := res.Integrations[0]
	if talos.Description != "first" {
		t.Errorf("talos description %q, want the first entry", talos.Description)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %q, want none", res.Warnings)
	}
}

func TestQueryAllRegistryUnreachable(t *testing.T) {
	official := indexServer(t, entry("s3", "official"))
	lab := indexServer(t, entry("talos", "lab"))
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	m, err := New(newFakeBackend(), &Options{
		ApiURL: official.URL,
		Registries: []Registry{
			{Name: "gone", URL: closed.URL},
			{Name: "lab", URL: lab.URL},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := m.QueryAll(nil)
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	got := byName(res.Integrations)
	if got["s3"] == nil || got["talos"] == nil {
		t.Errorf("integrations = %v, want s3 and talos", got)
	}
	if len(res.Warnings) != 1 || !strings.HasPrefix(res.Warnings[0], "registry gone: ") {
		t.Errorf("warnings = %q, want one about gone", res.Warnings)
	}
}

// A stalled registry is given up on, without failing the query.
func TestQueryAllRegistryTimeout(t *testing.T) {
	official := indexServer(t, entry("s3", "official"))
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer stalled.Close()

	m, err := New(newFakeBackend(), &Options{
		ApiURL:     official.URL,
		Registries: []Registry{{Name: "stalled", URL: stalled.URL}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.registryClient.Timeout != registryIndexTimeout {
		t.Errorf("registry timeout = %v, want %v", m.registryClient.Timeout, registryIndexTimeout)
	}
	m.registryClient.Timeout = 50 * time.Millisecond

	start := time.Now()
	res, err := m.QueryAll(nil)
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("QueryAll took %v, want the registry given up on", elapsed)
	}
	if byName(res.Integrations)["s3"] == nil {
		t.Error("s3 missing from results")
	}
	if len(res.Warnings) != 1 || !strings.HasPrefix(res.Warnings[0], "registry stalled: ") {
		t.Errorf("warnings = %q, want one about stalled", res.Warnings)
	}
}

func TestQueryAllRegistryBadResponses(t *testing.T) {
	official := indexServer(t, entry("s3", "official"))
	notFound := httptest.NewServer(http.NotFoundHandler())
	defer notFound.Close()
	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer garbage.Close()

	m, err := New(newFakeBackend(), &Options{
		ApiURL: official.URL,
		Registries: []Registry{
			{Name: "missing", URL: notFound.URL},
			{Name: "garbage", URL: garbage.URL},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := m.QueryAll(nil)
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if len(res.Integrations) != 1 {
		t.Errorf("got %d integrations, want only the official one", len(res.Integrations))
	}
	if len(res.Warnings) != 2 ||
		!strings.HasPrefix(res.Warnings[0], "registry missing: ") ||
		!strings.HasPrefix(res.Warnings[1], "registry garbage: ") {
		t.Errorf("warnings = %q, want one per failing registry, in order", res.Warnings)
	}
}

func TestRegistryRequestsCarryNoToken(t *testing.T) {
	official := indexServer(t, entry("s3", "official"))

	var (
		mu      sync.Mutex
		headers []string
	)
	lab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers = append(headers, r.Header.Get("Authorization"))
		mu.Unlock()
		body, _ := json.Marshal(IntegrationIndex{Integrations: []Integration{entry("talos", "lab")}})
		w.Write(body)
	}))
	defer lab.Close()

	hookCalls := 0
	m, err := New(newFakeBackend(), &Options{
		ApiURL: official.URL,
		RequestHook: WithBearer(func() (string, error) {
			hookCalls++
			return "secret", nil
		}),
		Registries: []Registry{{Name: "lab", URL: lab.URL}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := m.QueryAll(nil)
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if byName(res.Integrations)["talos"] == nil {
		t.Fatal("talos missing: the registry was not queried")
	}
	if hookCalls != 0 {
		t.Errorf("request hook called %d times, want 0", hookCalls)
	}
	if len(headers) == 0 {
		t.Fatal("the registry received no request")
	}
	for i, h := range headers {
		if h != "" {
			t.Errorf("registry request %d carried Authorization %q", i, h)
		}
	}
}

func TestQueryAllInstalledFromRegistry(t *testing.T) {
	official := indexServer(t, entry("s3", "official"))

	talos := entry("talos", "lab")
	talos.DisplayName = "Talos Linux"
	talos.Version = "v1.4.0-beta.1"
	talos.Icon = "data:image/svg+xml;base64,PHN2Zy8+"
	talos.Connectors = []Connector{{Type: "importer"}}
	lab := indexServer(t, talos)

	m, err := New(newFakeBackend(pkgVer("talos", "v1.3.0")), &Options{
		ApiURL:     official.URL,
		Registries: []Registry{{Name: "lab", URL: lab.URL}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := m.QueryAll(nil)
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	in := byName(res.Integrations)["talos"]
	if in == nil {
		t.Fatal("talos missing from results")
	}
	if in.DisplayName != "Talos Linux" || in.Description != "lab" {
		t.Errorf("talos display name %q description %q, want merged from lab", in.DisplayName, in.Description)
	}
	if in.LatestVersion != "v1.4.0-beta.1" || in.Stage != "beta" {
		t.Errorf("talos latest %q stage %q, want v1.4.0-beta.1 beta", in.LatestVersion, in.Stage)
	}
	if in.Icon != talos.Icon || !in.Types.Source {
		t.Errorf("talos icon %q types %+v, want merged from lab", in.Icon, in.Types)
	}
	if in.Installation.Status != "installed" || in.Installation.Version != "v1.3.0" || !in.Installation.Available {
		t.Errorf("talos installation = %+v, want installed v1.3.0 and available", in.Installation)
	}
	if in.Registry != "lab" {
		t.Errorf("talos registry = %q, want lab", in.Registry)
	}
}

func TestQueryAllOnlyLocalSkipsRegistries(t *testing.T) {
	lab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("registry queried with OnlyLocal: %s", r.URL)
	}))
	defer lab.Close()

	m, err := New(newFakeBackend(pkgVer("talos", "v1.3.0")), &Options{
		Registries: []Registry{{Name: "lab", URL: lab.URL}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := m.QueryAll(&QueryOptions{OnlyLocal: true})
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if len(res.Integrations) != 1 || res.Integrations[0].Registry != "" || len(res.Warnings) != 0 {
		t.Errorf("result = %+v, want only the local talos", res)
	}
}

func TestQueryAllRegistryFiltersAPIAndEdition(t *testing.T) {
	official := indexServer(t)

	wrongAPI := entry("oldapi", "lab")
	wrongAPI.API = "v0.0.1"
	enterprise := entry("paid", "lab")
	enterprise.Edition = "enterprise"
	lab := indexServer(t, entry("talos", "lab"), wrongAPI, enterprise)

	m, err := New(newFakeBackend(), &Options{
		ApiURL:     official.URL,
		Registries: []Registry{{Name: "lab", URL: lab.URL}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := m.QueryAll(nil)
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if len(res.Integrations) != 1 || res.Integrations[0].Name != "talos" {
		t.Errorf("integrations = %+v, want only talos", res.Integrations)
	}

	res, err = m.QueryAll(&QueryOptions{Edition: "enterprise"})
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if len(res.Integrations) != 1 || res.Integrations[0].Name != "paid" {
		t.Errorf("enterprise integrations = %+v, want only paid", res.Integrations)
	}
}

func TestIntegrationRegistryOmittedWhenEmpty(t *testing.T) {
	b, err := json.Marshal(&Integration{Name: "s3"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"registry"`) {
		t.Errorf("empty registry field marshalled: %s", b)
	}

	b, err = json.Marshal(&Integration{Name: "s3", Registry: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"registry":"lab"`) {
		t.Errorf("registry field not marshalled: %s", b)
	}
}

func TestNewRejectsBadRegistries(t *testing.T) {
	tests := []struct {
		name       string
		registries []Registry
	}{
		{"empty name", []Registry{{Name: "", URL: "https://example.com"}}},
		{"bad name", []Registry{{Name: "a/b", URL: "https://example.com"}}},
		{"duplicate name", []Registry{
			{Name: "lab", URL: "https://a.example.com"},
			{Name: "lab", URL: "https://b.example.com"},
		}},
		{"bad scheme", []Registry{{Name: "lab", URL: "ftp://example.com"}}},
		{"no scheme", []Registry{{Name: "lab", URL: "example.com/dist"}}},
		{"no host", []Registry{{Name: "lab", URL: "https:///dist"}}},
		{"unparsable", []Registry{{Name: "lab", URL: "https://example.com/%zz"}}},
		{"reserved name", []Registry{{Name: "official", URL: "https://example.com"}}},
		{"userinfo", []Registry{{Name: "lab", URL: "https://user:pass@example.com"}}},
		{"query", []Registry{{Name: "lab", URL: "https://example.com/dist?token=x"}}},
		{"empty query", []Registry{{Name: "lab", URL: "https://example.com/dist?"}}},
		{"fragment", []Registry{{Name: "lab", URL: "https://example.com/dist#x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(newFakeBackend(), &Options{Registries: tt.registries})
			if !errors.Is(err, ErrBadRegistry) {
				t.Errorf("New err = %v, want ErrBadRegistry", err)
			}
		})
	}

	_, err := New(newFakeBackend(), &Options{Registries: []Registry{
		{Name: "lab", URL: "https://example.com/dist"},
		{Name: "test_2", URL: "http://127.0.0.1:8080"},
	}})
	if err != nil {
		t.Errorf("New with valid registries: %v", err)
	}
}

func TestCheckRegistries(t *testing.T) {
	if err := CheckRegistries([]Registry{{Name: "lab", URL: "https://lab.example.org/"}}); err != nil {
		t.Errorf("CheckRegistries: %v", err)
	}
	err := CheckRegistries([]Registry{{Name: "official", URL: "https://lab.example.org/"}})
	if !errors.Is(err, ErrBadRegistry) {
		t.Errorf("CheckRegistries(official) = %v, want ErrBadRegistry", err)
	}
}
