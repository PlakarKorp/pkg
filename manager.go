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
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
)

const PLUGIN_API_VERSION = "v1.0.0"

type RequestHook func(*http.Request) error

var (
	ErrInvalidOptions        = errors.New("invalid options")
	ErrAlreadyInstalled      = errors.New("already installed")
	ErrAuthorizationRequired = errors.New("authorization required")
)

type Manager struct {
	store           Backend
	repository      *url.URL
	api             *url.URL
	reqhook         RequestHook
	binaryNeedsAuth bool
	useragent       string
}

type Options struct {
	InstallURL      string
	ApiURL          string
	BinaryNeedsAuth bool
	RequestHook     RequestHook

	// User agent name for network requests on the repository at
	// InstallURL.  "(os/architecture)" will be appended
	// implicitly.
	UserAgent string
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
		useragent:       opts.UserAgent,
		binaryNeedsAuth: opts.BinaryNeedsAuth,
		reqhook:         opts.RequestHook,
	}

	if opts.InstallURL != "" {
		u, err := url.Parse(opts.InstallURL)
		if err != nil {
			return nil, err
		}
		m.repository = u
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

type AddOptions struct {
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

		if opts.Replace {
			if err := p.store.Unload(pkg); err != nil {
				return err
			}
			continue
		}

		cmp := semver.Compare(version, pkg.Version)
		if cmp >= 0 && !opts.Downgrade {
			return ErrAlreadyInstalled
		}
		if cmp <= 0 && !opts.Upgrade {
			return ErrAlreadyInstalled
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
			r, err := p.FetchRecipe(base)
			if err != nil {
				return err
			}
			name, version = r.Name, r.Version
		}

		if err := p.preadd(name, version, opts); err != nil {
			return err
		}

		return p.fetchbinary(name, version)
	}

	var pkg Package
	if err := pkg.parseName(base); err != nil {
		return err
	}

	if err := p.preadd(pkg.Name, pkg.Version, opts); err != nil {
		return err
	}

	fp, err := os.Open(target)
	if err != nil {
		return err
	}
	defer fp.Close()

	return p.store.Load(&pkg, fp)
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
		return nil, fmt.Errorf("non-OK status code while fetching: %d %s",
			resp.StatusCode, resp.Status)
	}
	return resp, nil
}

func (p *Manager) FetchRecipe(name string) (*Recipe, error) {
	s := path.Join("kloset/recipe", PLUGIN_API_VERSION, name) + ".yaml"

	resp, err := p.fetch(p.repository, s, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var recipe Recipe
	if err := recipe.Parse(resp.Body); err != nil {
		return nil, err
	}

	return &recipe, nil
}

func (p *Manager) fetchbinary(name, version string) error {
	pkg := Package{
		Name:            name,
		Version:         version,
		Architecture:    runtime.GOARCH,
		OperatingSystem: runtime.GOOS,
	}

	s := path.Join("kloset/pkg", PLUGIN_API_VERSION, pkg.Filename())
	resp, err := p.fetch(p.repository, s, p.binaryNeedsAuth)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return p.store.Load(&pkg, resp.Body)
}

type DelOptions struct {
	// If target is the empty string, delete all the packages
	// installed.
	All bool
}

// Del uninstalls all matching packages.
func (p *Manager) Del(target string, opts *DelOptions) error {
	if opts == nil {
		opts = &DelOptions{}
	}

	if !opts.All && target == "" {
		return ErrBadPackageName
	}

	for pkg, err := range p.store.List(target) {
		if err != nil {
			return err
		}

		if err := p.store.Unload(pkg); err != nil {
			return err
		}
	}

	return nil
}

type QueryOptions struct {
	Type   string
	Tag    string
	Status string

	OnlyLocal bool
}

func (p *Manager) Query(opts *QueryOptions) (ret []*Integration, err error) {
	if opts == nil {
		opts = &QueryOptions{}
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
			APIVersion:  PLUGIN_API_VERSION,
			Installation: IntegrationInstallation{
				Status:  "installed",
				Version: p.Version,
			},
		}
	}

	if !opts.OnlyLocal {
		endp := "v1/integrations/" + PLUGIN_API_VERSION + ".json"
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

		for i := range index.Plugins {
			plug := &index.Plugins[i]

			if p, ok := packages[plug.Id]; ok {
				p.Id = plug.Id
				p.DisplayName = plug.DisplayName
				p.Description = plug.Description
				p.Homepage = plug.Homepage
				p.Repository = plug.Repository
				p.License = plug.License
				p.Tags = p.Tags
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
