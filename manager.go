/*
 * Copyright (c) 2025, 2026 Gilles Chehade <gilles@poolp.org>
 * Copyright (c) 2025, 2026 Eric Faurot <eric.faurot@plakar.io>
 * Copyright (c) 2025, 2026 Omar Polo <op@omarpolo.com>
 *
 * Permission to use, copy, modify, and distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */

package pkg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const PLUGIN_API_VERSION = "v1.1.0"
const PLUGIN_BUNDLE_VERSION = "v1.0.0"

type RequestHook func(*http.Request) error

var (
	ErrInvalidOptions        = errors.New("invalid options")
	ErrAlreadyInstalled      = errors.New("already installed")
	ErrBadOSArch             = errors.New("OS or architecture don't match the current one")
	ErrAuthorizationRequired = errors.New("authorization required")
	ErrNoDistURL             = errors.New("no DistURL provided")
	ErrNoApiURL              = errors.New("no ApiURL provided")
	ErrBadEdition            = errors.New("bad edition")
	ErrBadRegistry           = errors.New("bad registry")
	ErrUnknownRegistry       = errors.New("not a configured registry")

	editionre = regexp.MustCompile(`^[-_a-zA-Z0-9]+$`)
)

type Manager struct {
	store           Backend
	repository      *url.URL
	edition         string
	api             *url.URL
	index           *url.URL
	reqhook         RequestHook
	binaryNeedsAuth bool
	useragent       string
	verifier        Verifier
	registries      []registry

	// registryClient fetches the indexes of the registries.
	registryClient *http.Client
}

type Options struct {
	DistURL         string
	Edition         string // community, devel, enterprise, ...
	ApiURL          string

	// IndexURL is where [Manager.Query] fetches the integrations
	// index from, overriding the default location on the API
	// server.  A mirror of the distribution tree serves it at its
	// root, next to the edition directories.
	IndexURL string
	BinaryNeedsAuth bool
	RequestHook     RequestHook

	// User agent name for network requests on the repository at
	// DistURL.  "(os/architecture)" will be appended implicitly.
	UserAgent string

	// Verifier decides whether an artifact may be installed. When nil,
	// nothing is verified and no signature is fetched.
	Verifier Verifier

	// Registries are additional package registries whose indexes
	// [Manager.QueryAll] merges after the official one, in order.
	// [Manager.Add] fetches a package from them when the official
	// tree doesn't have it, and upgrades it from the registry it was
	// installed from.
	Registries []Registry
}

// WithBearer adds an Authorization header with the Bearer token
// returned by the given callback.  It's meant to be passed as
// [Options.RequestHook].  If it yields an empty token, the header
// will not be added.
func WithBearer(fn func() (string, error)) func(*http.Request) error {
	return func(req *http.Request) error {
		token, err := fn()
		if err != nil {
			return err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return nil
	}
}

// New creates a new package manager.
func New(store Backend, opts *Options) (*Manager, error) {
	if opts == nil {
		opts = &Options{}
	}

	m := &Manager{
		store:           store,
		edition:         "community",
		useragent:       opts.UserAgent,
		binaryNeedsAuth: opts.BinaryNeedsAuth,
		reqhook:         opts.RequestHook,
		verifier:        opts.Verifier,
	}

	if opts.DistURL != "" {
		u, err := url.Parse(opts.DistURL)
		if err != nil {
			return nil, err
		}
		m.repository = u
	}

	if opts.Edition != "" {
		if !editionre.MatchString(opts.Edition) {
			return nil, fmt.Errorf("%w: %q", ErrBadEdition, opts.Edition)
		}
		m.edition = opts.Edition
	}

	if opts.ApiURL != "" {
		u, err := url.Parse(opts.ApiURL)
		if err != nil {
			return nil, err
		}
		m.api = u
	}

	if opts.IndexURL != "" {
		u, err := url.Parse(opts.IndexURL)
		if err != nil {
			return nil, err
		}
		m.index = u
	}

	registries, err := parseRegistries(opts.Registries)
	if err != nil {
		return nil, err
	}
	m.registries = registries
	m.registryClient = &http.Client{Timeout: registryIndexTimeout}

	if m.useragent == "" {
		m.useragent = "pkg/v0.0.1"
	}
	m.useragent += fmt.Sprintf(" (%s/%s)", runtime.GOOS, runtime.GOARCH)
	return m, nil
}

// List lists all the installed packages.
func (p *Manager) List() iter.Seq2[*Package, error] {
	return p.store.List("")
}

// Signature returns the retained signature of an installed package, or nil if
// it was installed without one.
func (p *Manager) Signature(pkg *Package) ([]byte, error) {
	return p.store.Signature(pkg)
}

type AddOptions struct {
	// The edition to consider, if given.  Falls back to
	// [Options.Edition].
	Edition string

	// The version to install, if given.  Otherwise, the latest
	// version available will be used.
	Version string

	// If exists a older version of the plugin, remove it prior
	// to install this version.
	Upgrade bool

	// If exists a newer version of the plugin, remove it prior
	// to install this version.
	Downgrade bool

	// Remove other version of the plugin, even if it's the same,
	// and install this version.
	Replace bool

	// Don't fail if other versions of the same plugin exist.
	AllowMultipleVersions bool

	// If target does not point at a .ptar file, attempt to fetch
	// the pre-packaged plugin from the repository.
	ImplicitFetch bool

	// Install the package even if the OS and Architecture don't
	// match.
	AllowOSArchMismatch bool

	// Install the container flavor when fetching from the repository: the
	// package whose OS atom is OSContainer, carrying an image pin instead
	// of executables. Local .ptar files carry it in the filename instead.
	Container bool
}

func (p *Manager) preadd(name, version string, opts *AddOptions) error {
	for pkg, err := range p.store.List(name) {
		if err != nil {
			return err
		}

		if opts.AllowMultipleVersions {
			if pkg.Version == version {
				return ErrAlreadyInstalled
			}
			continue
		}

		if !opts.Replace && !opts.Upgrade && !opts.Downgrade {
			return ErrAlreadyInstalled
		}

		// Replace removes whatever other version is present,
		// regardless of how it compares to the requested one.
		if !opts.Replace {
			cmp := semver.Compare(version, pkg.Version)
			if cmp == 0 {
				return ErrAlreadyInstalled
			}
			if cmp < 0 && !opts.Downgrade {
				return ErrAlreadyInstalled
			}
			if cmp > 0 && !opts.Upgrade {
				return ErrAlreadyInstalled
			}
		}

		if err := p.store.Unload(pkg); err != nil {
			return err
		}
	}

	return nil
}

// Add installs a package.  By default, it will fail if another
// version of the same plugin is already present.  A package fetched from
// an additional registry is recorded as such when the backend is an
// [OriginStore]; if that fails, the package is not kept installed, and
// when upgrading, the previous version is already removed.
func (p *Manager) Add(target string, opts *AddOptions) error {
	if opts == nil {
		opts = &AddOptions{}
	}

	if opts.Upgrade && opts.Downgrade {
		return ErrInvalidOptions
	}

	if opts.Replace && (opts.Upgrade || opts.Downgrade) {
		return ErrInvalidOptions
	}

	if opts.AllowMultipleVersions && (opts.Upgrade || opts.Downgrade || opts.Replace) {
		return ErrInvalidOptions
	}

	base := filepath.Base(target)

	if opts.ImplicitFetch && !strings.HasSuffix(base, ".ptar") {
		src, r, err := p.resolve(base, opts)
		if err != nil {
			return err
		}

		name, version := base, opts.Version
		if version == "" {
			name, version = r.Name, r.Semver()
		}

		if err := p.preadd(name, version, opts); err != nil {
			return err
		}

		return p.fetchbinary(src, name, version, opts.Edition, opts.Container)
	}

	var pkg Package
	if err := pkg.parseName(base); err != nil {
		return err
	}

	if !opts.AllowOSArchMismatch {
		// Container packages are not tied to the host OS; they run on any
		// linux host with a container runtime.
		goos := pkg.OperatingSystem
		if goos == OSContainer {
			goos = "linux"
		}
		if goos != runtime.GOOS || pkg.Architecture != runtime.GOARCH {
			return ErrBadOSArch
		}
	}

	if err := p.preadd(pkg.Name, pkg.Version, opts); err != nil {
		return err
	}

	fp, err := os.Open(target)
	if err != nil {
		return err
	}
	defer fp.Close()

	// A local artifact carries its signature as a sibling file.
	sig, err := os.ReadFile(target + sigSuffix)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	rd, err := p.verify(pkg.Filename(), &pkg, LocalOrigin, sig, fp)
	if err != nil {
		return err
	}

	return p.store.Load(&pkg, rd, sig)
}

func (p *Manager) official() *source {
	return &source{url: p.repository, needsAuth: p.binaryNeedsAuth}
}

// resolve finds the source to install name from and, unless opts gives
// the version and the source is known without it, its recipe there.  An
// installed package is fetched again from the registry it was installed
// from, or from the official tree when none was recorded.  Otherwise, the
// official tree is preferred and the additional registries are only tried
// in order when it doesn't have the recipe, so a failing official tree or
// registry never lets a later registry provide the name.
func (p *Manager) resolve(name string, opts *AddOptions) (*source, *Recipe, error) {
	src, err := p.installedFrom(name)
	if err != nil {
		return nil, nil, err
	}
	if src == nil && len(p.registries) == 0 {
		src = p.official()
	}
	if src != nil {
		if opts.Version != "" {
			return src, nil, nil
		}
		r, err := p.recipeOf(src, name, opts.Edition)
		return src, r, err
	}

	official := p.official()
	r, err := p.recipeOf(official, name, opts.Edition)
	if !isNotFound(err) {
		return official, r, err
	}

	for i := range p.registries {
		src := p.registries[i].source()
		r, rerr := p.recipeOf(src, name, opts.Edition)
		if rerr == nil {
			return src, r, nil
		}
		if !isNotFound(rerr) {
			return nil, nil, fmt.Errorf("registry %s: %w", p.registries[i].name, rerr)
		}
	}
	return nil, nil, fmt.Errorf("%s: not found in the official repository or any configured registry: %w", name, err)
}

// recipeOf fetches the recipe of name from src, making sure it describes
// that name: a recipe for another one would install, or replace, another
// package.
func (p *Manager) recipeOf(src *source, name, edition string) (*Recipe, error) {
	r, err := p.fetchRecipe(src, name, edition)
	if err != nil {
		return nil, err
	}
	if r.Name != name {
		return nil, fmt.Errorf("%w %q: recipe describes %q", ErrBadPackageName, name, r.Name)
	}
	return r, nil
}

// installedFrom returns the source an installed version of name was
// fetched from: the registry recorded for it, or the official tree when
// none was.  It returns nil if name is not installed.
func (p *Manager) installedFrom(name string) (*source, error) {
	store, _ := p.store.(OriginStore)

	installed := false
	for pkg, err := range p.store.List(name) {
		if err != nil {
			return nil, err
		}
		installed = true
		if store == nil {
			break
		}
		origin, err := store.Origin(pkg)
		if err != nil {
			return nil, err
		}
		if origin == "" {
			continue
		}
		if reg := p.registryOf(origin); reg != nil {
			return reg.source(), nil
		}
		return nil, fmt.Errorf("%s: installed from %s, which is %w", name, origin, ErrUnknownRegistry)
	}

	if installed {
		return p.official(), nil
	}
	return nil, nil
}

// registryOf returns the configured registry recorded as origin, or nil
// if none is.
func (p *Manager) registryOf(origin string) *registry {
	for i := range p.registries {
		if sameURL(p.registries[i].rawurl, origin) {
			return &p.registries[i]
		}
	}
	return nil
}

// sameURL tells whether two registry URLs are the same, regardless of a
// trailing slash.
func sameURL(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}

func (p *Manager) repoFor(src *source, edition string) (*url.URL, error) {
	if src.url == nil {
		return nil, ErrNoDistURL
	}

	u := *src.url
	if edition == "" {
		edition = p.edition
	}
	if !editionre.MatchString(edition) {
		return nil, fmt.Errorf("%w: %q", ErrBadEdition, edition)
	}

	u.Path = path.Join(u.Path, edition)
	return &u, nil
}

func (p *Manager) fetch(url *url.URL, endpoint string, reqauth bool) (*http.Response, error) {
	return p.fetchWith(http.DefaultClient, url, endpoint, reqauth)
}

func (p *Manager) fetchWith(client *http.Client, url *url.URL, endpoint string, reqauth bool) (*http.Response, error) {
	u := *url
	u.Path = path.Join(u.Path, endpoint)

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", p.useragent)
	if reqauth && p.reqhook != nil {
		if err := p.reqhook(req); err != nil {
			return nil, err
		}
	}

	if reqauth && req.Header.Get("Authorization") == "" {
		return nil, ErrAuthorizationRequired
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, &statusError{code: resp.StatusCode, status: resp.Status}
	}
	return resp, nil
}

// A statusError reports an unexpected HTTP response.
type statusError struct {
	code   int
	status string
}

func (e *statusError) Error() string {
	return "fetch failed with " + e.status
}

func isNotFound(err error) bool {
	var serr *statusError
	return errors.As(err, &serr) && serr.code == http.StatusNotFound
}

type FetchOptions struct {
	// The edition to consider, if given.  Falls back to
	// [Options.Edition].
	Edition string
}

// FetchRecipe fetches the recipe of name from the official tree.
func (p *Manager) FetchRecipe(name string, opts *FetchOptions) (*Recipe, error) {
	if opts == nil {
		opts = &FetchOptions{}
	}
	return p.fetchRecipe(p.official(), name, opts.Edition)
}

func (p *Manager) fetchRecipe(src *source, name, edition string) (*Recipe, error) {
	const filename = "recipe.yaml"

	repo, err := p.repoFor(src, edition)
	if err != nil {
		return nil, err
	}

	// A recipe resolves which version gets installed, so it must be
	// verified before it is parsed.
	sig, err := p.fetchsig(repo, path.Join(PLUGIN_API_VERSION, name, filename))
	if err != nil {
		return nil, err
	}

	s := path.Join(PLUGIN_API_VERSION, name, filename)
	resp, err := p.fetch(repo, s, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rd, err := p.verify(filename, nil, repo.String(), sig, resp.Body)
	if err != nil {
		return nil, err
	}

	var recipe Recipe
	if err := recipe.Parse(rd); err != nil {
		return nil, err
	}

	return &recipe, nil
}

// fetchsig retrieves the .sum.sig accompanying the file at endpoint. A missing
// signature yields a nil slice, leaving it to the Verifier to decide what an
// unsigned file means.
func (p *Manager) fetchsig(url *url.URL, endpoint string) ([]byte, error) {
	if p.verifier == nil {
		return nil, nil
	}

	resp, err := p.fetch(url, endpoint+sigSuffix, false)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()

	return io.ReadAll(io.LimitReader(resp.Body, maxSignatureSize))
}

// verify runs the configured Verifier over rd, returning a reader over the
// verified content. rd is returned unchanged when no Verifier is configured.
func (p *Manager) verify(filename string, pkg *Package, origin string, sig []byte, rd io.Reader) (io.Reader, error) {
	if p.verifier == nil {
		return rd, nil
	}

	artifact := &Artifact{
		Filename:  filename,
		Package:   pkg,
		Origin:    origin,
		Signature: sig,
	}

	// Buffered because both the Verifier and Load need to read it, and rd
	// is not seekable.
	content, err := io.ReadAll(rd)
	if err != nil {
		return nil, err
	}

	if err := p.verifier.Verify(artifact, bytes.NewReader(content)); err != nil {
		return nil, err
	}

	return bytes.NewReader(content), nil
}

// fetchbinary installs the package name at version from src, and records
// the registry it comes from.  If that can't be recorded, the package is
// unloaded: when upgrading, the previous version is already gone.
func (p *Manager) fetchbinary(src *source, name, version, edition string, container bool) error {
	goos := runtime.GOOS
	if container {
		goos = OSContainer
	}

	pkg := Package{
		Name:            name,
		Version:         version,
		Architecture:    runtime.GOARCH,
		OperatingSystem: goos,
	}

	repo, err := p.repoFor(src, edition)
	if err != nil {
		return err
	}

	s := path.Join(PLUGIN_API_VERSION, name, pkg.Filename())

	// Fetched first, so a missing signature fails before pulling down tens
	// of megabytes.
	sig, err := p.fetchsig(repo, s)
	if err != nil {
		return err
	}

	resp, err := p.fetch(repo, s, src.needsAuth)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	rd, err := p.verify(pkg.Filename(), &pkg, repo.String(), sig, resp.Body)
	if err != nil {
		return err
	}

	if err := p.store.Load(&pkg, rd, sig); err != nil {
		return err
	}

	// Nothing is recorded for the official tree, the default origin.
	store, ok := p.store.(OriginStore)
	if src.origin == "" || !ok {
		return nil
	}
	if err := store.SetOrigin(&pkg, src.origin); err != nil {
		// A package with a wrong origin would later be upgraded
		// from the wrong place.
		return errors.Join(err, p.store.Unload(&pkg))
	}
	return nil
}

type DelOptions struct {
	// If target is the empty string, delete all the packages
	// installed.
	All bool

	// If version is not the empty string, delete only the given
	// version.  It's incompatible with All.
	Version string
}

// Del uninstalls all matching packages.
func (p *Manager) Del(target string, opts *DelOptions) error {
	if opts == nil {
		opts = &DelOptions{}
	}

	if !opts.All && target == "" {
		return ErrBadPackageName
	}

	if opts.All && opts.Version != "" {
		return ErrInvalidOptions
	}

	for pkg, err := range p.store.List(target) {
		if err != nil {
			return err
		}

		if opts.Version != "" && pkg.Version != opts.Version {
			continue
		}

		if err := p.store.Unload(pkg); err != nil {
			return err
		}
	}

	return nil
}

// stageOf classifies a semver version's prerelease component into the
// release stage shown by the UI: "stable" for a version with none, then
// "devel", "beta" or "testing" for the well-known prerelease prefixes, and
// the raw prerelease string itself (e.g. "-alpha.1") for anything else.
func stageOf(version string) string {
	pr := semver.Prerelease(version)
	switch {
	case pr == "":
		return "stable"
	case strings.HasPrefix(pr, "-devel."):
		return "devel"
	case strings.HasPrefix(pr, "-beta."):
		return "beta"
	case strings.HasPrefix(pr, "-rc."):
		return "testing"
	default:
		return pr
	}
}

type QueryOptions struct {
	Type    string
	Tag     string
	Status  string
	Edition string

	OnlyLocal bool
}

// QueryResult is the outcome of [Manager.QueryAll].
type QueryResult struct {
	Integrations []*Integration

	// Warnings reports the registries that could not be queried, the
	// registry entries shadowed by another source, and the installed
	// integrations whose registry is no longer configured.
	Warnings []string
}

// Query returns the integrations of [Manager.QueryAll], without its
// warnings.
func (p *Manager) Query(opts *QueryOptions) ([]*Integration, error) {
	res, err := p.QueryAll(opts)
	if err != nil {
		return nil, err
	}
	return res.Integrations, nil
}

// QueryAll lists the installed integrations along with the ones available
// from the official index and then from each additional registry, in
// order.  An installed integration only takes the index entry of the
// source it was installed from: the registry recorded for it, or the
// official index when none was.  Otherwise, an integration provided by an
// earlier source is not taken from a later registry.  A registry that
// can't be queried, and an installed integration whose registry is no
// longer configured, are reported in the warnings instead of failing the
// query.
func (p *Manager) QueryAll(opts *QueryOptions) (*QueryResult, error) {
	if opts == nil {
		opts = &QueryOptions{}
	}

	edition := opts.Edition
	if edition == "" {
		edition = p.edition
	}
	if !editionre.MatchString(edition) {
		return nil, fmt.Errorf("%w: %q", ErrBadEdition, edition)
	}

	res := &QueryResult{}
	packages := make(map[string]*Integration)

	// providers maps an integration name to the source it was taken
	// from: the registry name, or "" for the official index.  An
	// installed integration is bound to its own source beforehand.
	providers := make(map[string]string)
	store, _ := p.store.(OriginStore)
	for pkg, err := range p.List() {
		if err != nil {
			return nil, err
		}

		var reg *registry
		from := noOrigin
		if store != nil {
			origin, err := store.Origin(pkg)
			if err != nil {
				return nil, err
			}
			if origin != "" {
				if reg = p.registryOf(origin); reg != nil {
					from = reg.name
				} else {
					// An origin that is no longer configured
					// matches no registry name, so no index
					// entry is merged into it, ever: mergeIndex
					// would otherwise have to repeat this warning.
					res.Warnings = append(res.Warnings, fmt.Sprintf("integration %s: installed from %s, which is %v", pkg.Name, origin, ErrUnknownRegistry))
					from = unconfiguredOrigin
				}
			}
		}
		providers[pkg.Name] = from

		// we don't have all the information locally, so fill
		// what we have and integrate the rest after we've hit
		// the api.
		in := &Integration{
			Id:            pkg.Name,
			Name:          pkg.Name,
			DisplayName:   pkg.Name,
			Tags:          []string{},
			API:           PLUGIN_API_VERSION,
			LatestVersion: pkg.Version,
			Stage:         stageOf(pkg.Version),
			Installation: IntegrationInstallation{
				Status:  "installed",
				Version: pkg.Version,
			},
		}
		if reg != nil {
			in.Registry = reg.name
		}

		// Fall back to the package's own manifest so an installed
		// integration missing from the remote catalog still gets a
		// complete card. A manifest that can't be read or parsed
		// must not fail Query, it only leaves the fallback fields
		// unset.
		if mr, ok := p.store.(ManifestReader); ok {
			if m, dir, err := mr.Manifest(pkg); err == nil {
				integrationFromManifest(in, m, dir)
			}
		}

		packages[pkg.Name] = in
	}

	if !opts.OnlyLocal {
		from, endp := p.index, ""
		if from == nil {
			if p.api == nil {
				return nil, ErrNoApiURL
			}
			from = p.api
			endp = "v1/integrations/integrations-" + PLUGIN_BUNDLE_VERSION + ".json"
		}

		index, err := p.fetchIndex(http.DefaultClient, from, endp)
		if err != nil {
			return nil, err
		}

		mergeIndex(packages, providers, index, edition, "")

		for _, reg := range p.registries {
			endp := "integrations-" + PLUGIN_BUNDLE_VERSION + ".json"
			index, err := p.fetchIndex(p.registryClient, reg.url, endp)
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("registry %s: %v", reg.name, err))
				continue
			}
			w := mergeIndex(packages, providers, index, edition, reg.name)
			res.Warnings = append(res.Warnings, w...)
		}
	}

	for _, plug := range packages {
		if opts.Type == "storage" && !plug.Types.Storage {
			continue
		}
		if opts.Type == "source" && !plug.Types.Source {
			continue
		}
		if opts.Type == "destination" && !plug.Types.Destination {
			continue
		}

		if opts.Tag != "" && !slices.Contains(plug.Tags, opts.Tag) {
			continue
		}

		if opts.Status != "" && opts.Status != plug.Installation.Status {
			continue
		}

		res.Integrations = append(res.Integrations, plug)
	}

	slices.SortFunc(res.Integrations, func(a, b *Integration) int {
		return strings.Compare(a.Name, b.Name)
	})
	return res, nil
}

const (
	// maxIndexSize bounds the size of an integrations index.
	maxIndexSize = 64 << 20 // 64 MiB

	// registryIndexTimeout bounds the fetch of a registry index, so
	// that a stalled registry doesn't hang the query.
	registryIndexTimeout = 30 * time.Second
)

// fetchIndex retrieves an integrations index.  It never authenticates:
// the index is public, and the request may target a third-party registry.
func (p *Manager) fetchIndex(client *http.Client, from *url.URL, endpoint string) (*IntegrationIndex, error) {
	res, err := p.fetchWith(client, from, endpoint, false)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var index IntegrationIndex
	if err := json.NewDecoder(io.LimitReader(res.Body, maxIndexSize)).Decode(&index); err != nil {
		return nil, err
	}
	return &index, nil
}

const (
	// noOrigin marks, in the providers map given to mergeIndex, an
	// installed package with no recorded origin (or whose backend
	// doesn't implement OriginStore).  The official index is its
	// default source and still claims the entry when it lists it, but a
	// registry that lists the name while the official index doesn't
	// must not sound like the official catalog shadowed it: nothing
	// actually did.
	noOrigin = "\x00no-origin"

	// unconfiguredOrigin marks an installed package whose recorded
	// origin no longer matches any configured registry.  The warning is
	// already emitted where the origin is resolved, so mergeIndex stays
	// silent about it for every source, official included.
	unconfiguredOrigin = "\x00unconfigured-origin"
)

// mergeIndex merges the entries of index matching the current plugin API
// and the given edition into packages, keeping the first entry of a name.
// registry is the name of the registry the index comes from, empty for the
// official one.  An entry never replaces one taken from another source, as
// recorded in providers (by registry name, empty for the official index,
// or one of the sentinels above for an installed package); a registry
// entry turned away that way is reported in the returned warnings, worded
// after the actual reason: shadowed by the official index or an earlier
// registry, or ignored because the installed package has no recorded
// origin or was installed from another registry.
func mergeIndex(packages map[string]*Integration, providers map[string]string, index *IntegrationIndex, edition, registry string) (warnings []string) {
	seen := make(map[string]bool)
	for i := range index.Integrations {
		plug := &index.Integrations[i]

		if plug.API != PLUGIN_API_VERSION || plug.Edition != edition || seen[plug.Name] {
			continue
		}
		seen[plug.Name] = true

		plug.normalize(registry)

		by, ok := providers[plug.Id]
		switch {
		case ok && by == unconfiguredOrigin:
			// Already warned about above; stay silent for every
			// source.
			continue
		case ok && by == noOrigin && registry == "":
			// The official index is the default source for a
			// package installed with no recorded origin: let it
			// claim the entry now that it actually lists it.
			providers[plug.Id] = registry
		case ok && by != registry:
			if registry != "" {
				switch {
				case by == noOrigin:
					warnings = append(warnings, fmt.Sprintf("registry %s: integration %s ignored: installed package has no recorded origin (reinstall it to track this registry)", registry, plug.Id))
				case by == "":
					warnings = append(warnings, fmt.Sprintf("registry %s: integration %s shadowed by official", registry, plug.Id))
				case packages[plug.Id] != nil && packages[plug.Id].Installation.Status == "installed":
					warnings = append(warnings, fmt.Sprintf("registry %s: integration %s ignored: installed from registry %s", registry, plug.Id, by))
				default:
					warnings = append(warnings, fmt.Sprintf("registry %s: integration %s shadowed by %s", registry, plug.Id, by))
				}
			}
			continue
		default:
			providers[plug.Id] = registry
		}

		if p, ok := packages[plug.Id]; ok {
			p.merge(plug)
		} else {
			plug.Installation.Status = "not-installed"
			plug.Installation.Available = true
			packages[plug.Id] = plug
		}
	}
	return warnings
}
