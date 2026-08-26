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

	editionre = regexp.MustCompile(`^[-_a-zA-Z0-9]+$`)
)

type Manager struct {
	store           Backend
	repository      *url.URL
	edition         string
	api             *url.URL
	reqhook         RequestHook
	binaryNeedsAuth bool
	useragent       string
	verifier        Verifier
}

type Options struct {
	DistURL         string
	Edition         string // community, devel, enterprise, ...
	ApiURL          string
	BinaryNeedsAuth bool
	RequestHook     RequestHook

	// User agent name for network requests on the repository at
	// DistURL.  "(os/architecture)" will be appended implicitly.
	UserAgent string

	// Verifier decides whether an artifact may be installed. When nil,
	// nothing is verified and no signature is fetched.
	Verifier Verifier
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
// version of the same plugin is already present.
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
		var name, version string

		if opts.Version != "" {
			name, version = base, opts.Version
		} else {
			r, err := p.FetchRecipe(base, &FetchOptions{Edition: opts.Edition})
			if err != nil {
				return err
			}
			name, version = r.Name, r.Semver()
		}

		if err := p.preadd(name, version, opts); err != nil {
			return err
		}

		return p.fetchbinary(name, version, opts.Edition, opts.Container)
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

func (p *Manager) repoFor(edition string) (*url.URL, error) {
	if p.repository == nil {
		return nil, ErrNoDistURL
	}

	u := *p.repository
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("fetch failed with %s", resp.Status)
	}
	return resp, nil
}

type FetchOptions struct {
	// The edition to consider, if given.  Falls back to
	// [Options.Edition].
	Edition string
}

func (p *Manager) FetchRecipe(name string, opts *FetchOptions) (*Recipe, error) {
	if opts == nil {
		opts = &FetchOptions{}
	}

	const filename = "recipe.yaml"

	repo, err := p.repoFor(opts.Edition)
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

func (p *Manager) fetchbinary(name, version, edition string, container bool) error {
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

	repo, err := p.repoFor(edition)
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

	resp, err := p.fetch(repo, s, p.binaryNeedsAuth)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	rd, err := p.verify(pkg.Filename(), &pkg, repo.String(), sig, resp.Body)
	if err != nil {
		return err
	}

	return p.store.Load(&pkg, rd, sig)
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

type QueryOptions struct {
	Type    string
	Tag     string
	Status  string
	Edition string

	OnlyLocal bool
}

func (p *Manager) Query(opts *QueryOptions) (ret []*Integration, err error) {
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

	packages := make(map[string]*Integration)
	for p, err := range p.List() {
		if err != nil {
			return nil, err
		}

		// we don't have all the information locally, so fill
		// what we have and integrate the rest after we've hit
		// the api.
		packages[p.Name] = &Integration{
			Id:          p.Name,
			Name:        p.Name,
			DisplayName: p.Name,
			Tags:        []string{},
			API:         PLUGIN_API_VERSION,
			Installation: IntegrationInstallation{
				Status:  "installed",
				Version: p.Version,
			},
		}
	}

	if !opts.OnlyLocal {
		if p.api == nil {
			return nil, ErrNoApiURL
		}

		endp := "v1/integrations/integrations-" + PLUGIN_BUNDLE_VERSION + ".json"
		res, err := p.fetch(p.api, endp, false)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()

		var index IntegrationIndex
		err = json.NewDecoder(res.Body).Decode(&index)
		if err != nil {
			return nil, err
		}

		for i := range index.Integrations {
			plug := &index.Integrations[i]

			if plug.API != PLUGIN_API_VERSION {
				continue
			}
			if plug.Edition != edition {
				continue
			}

			// Set compatibility fields for the former model
			plug.Id = plug.Name
			plug.LatestVersion = plug.Version
			pr := semver.Prerelease(plug.Version)
			switch {
			case pr == "":
				plug.Stage = "stable"
			case strings.HasPrefix(pr, "-devel."):
				plug.Stage = "devel"
			case strings.HasPrefix(pr, "-beta."):
				plug.Stage = "beta"
			case strings.HasPrefix(pr, "-rc."):
				plug.Stage = "testing"
			default:
				plug.Stage = pr
			}
			plug.Types.Destination = plug.HasConnectorType("exporter")
			plug.Types.Source = plug.HasConnectorType("importer")
			plug.Types.Storage = plug.HasConnectorType("storage")

			if p, ok := packages[plug.Id]; ok {
				p.Id = plug.Id
				p.DisplayName = plug.DisplayName
				p.Description = plug.Description
				p.Homepage = plug.Homepage
				p.Repository = plug.Repository
				p.License = plug.License
				p.Tags = plug.Tags
				p.LatestVersion = plug.LatestVersion
				p.Stage = plug.Stage
				p.Types = plug.Types
				p.Documentation = plug.Documentation
				p.Icon = plug.Icon
				p.Featured = plug.Featured

				p.Installation.Available = true
			} else {
				plug.Installation.Status = "not-installed"
				plug.Installation.Available = true
				packages[plug.Id] = plug
			}
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

		ret = append(ret, plug)
	}

	slices.SortFunc(ret, func(a, b *Integration) int {
		return strings.Compare(a.Name, b.Name)
	})
	return ret, nil
}
