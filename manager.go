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
	"strings"

	"golang.org/x/mod/semver"
)

const PLUGIN_API_VERSION = "v1.0.0"

var (
	ErrInvalidOptions   = errors.New("invalid options")
	ErrAlreadyInstalled = errors.New("already installed")
	ErrMissingToken     = errors.New("token required")
)

type Manager struct {
	store            Backend
	repository       *url.URL
	recipes          *url.URL
	token            string
	binaryNeedsToken bool
	useragent        string
}

type Options struct {
	InstallURL       string
	RecipesURL       string
	Token            string
	BinaryNeedsToken bool

	// User agent name for network requests on the repository at
	// InstallURL.  "(os/architecture)" will be appended
	// implicitly.
	UserAgent string
}

func New(store Backend, opts *Options) (*Manager, error) {
	if opts == nil {
		opts = &Options{}
	}

	m := &Manager{
		store:     store,
		useragent: opts.UserAgent,
	}
	if opts.InstallURL != "" {
		u, err := url.Parse(opts.InstallURL)
		if err != nil {
			return nil, err
		}
		m.repository = u
	}

	if opts.RecipesURL != "" {
		u, err := url.Parse(opts.RecipesURL)
		if err != nil {
			return nil, err
		}
		m.recipes = u
	}

	if m.useragent == "" {
		m.useragent = "pkg/v0.0.1"
	}
	m.useragent += fmt.Sprintf(" (%s/%s)", runtime.GOOS, runtime.GOARCH)
	return m, nil
}

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
			r, err := p.fetchrecipe(base)
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

func (p *Manager) fetch(url *url.URL, endpoint string) (*http.Response, error) {
	u := *url
	u.Path = path.Join(u.Path, endpoint)

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", p.useragent)
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
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

func (p *Manager) fetchrecipe(name string) (*Recipe, error) {
	s := path.Join("kloset/recipe", PLUGIN_API_VERSION, name) + ".yaml"

	resp, err := p.fetch(p.recipes, s)
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
	if p.binaryNeedsToken && p.token == "" {
		return ErrMissingToken
	}

	pkg := Package{
		Name:            name,
		Version:         version,
		Architecture:    runtime.GOARCH,
		OperatingSystem: runtime.GOOS,
	}

	s := path.Join("kloset/pkg", PLUGIN_API_VERSION, pkg.Filename())
	resp, err := p.fetch(p.repository, s)
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

func (p *Manager) Query() iter.Seq2[*Integration, error] {
	return func(yield func(*Integration, error) bool) {
		endp := "v1/integrations/" + PLUGIN_API_VERSION + ".json"
		res, err := p.fetch(p.recipes, endp)
		if err != nil {
			yield(nil, err)
			return
		}
		defer res.Body.Close()

		var index IntegrationIndex
		err = json.NewDecoder(res.Body).Decode(&index)
		if err != nil {
			yield(nil, err)
			return
		}

		for i := range index.Plugins {
			if !yield(&index.Plugins[i], nil) {
				return
			}
		}
	}
}
