package pkg

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"

	fsexporter "github.com/PlakarKorp/integration-fs/exporter"
	_ "github.com/PlakarKorp/integration-ptar/storage"
	"github.com/PlakarKorp/kloset/kcontext"
	"github.com/PlakarKorp/kloset/locate"
	"github.com/PlakarKorp/kloset/repository"
	"github.com/PlakarKorp/kloset/snapshot"
	"github.com/PlakarKorp/kloset/snapshot/exporter"
	"github.com/PlakarKorp/kloset/storage"
)

// A backend that stores integrations in a single, flat, directory.
type FlatBackend struct {
	kcontext *kcontext.KContext
	pkgdir   string
	cachedir string

	preloadhook func(*Manifest) error
	loadhook    func(*Manifest, *Package, string)
	unloadhook  func(*Manifest, *Package)
}

type FlatBackendOptions struct {
	PreLoadHook func(*Manifest) error
	LoadHook    func(*Manifest, *Package, string)
	UnloadHook  func(*Manifest, *Package)
}

func NewFlatBackend(kctx *kcontext.KContext, pkgdir, cachedir string, opts *FlatBackendOptions) (*FlatBackend, error) {
	if err := os.MkdirAll(pkgdir, 0755); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(cachedir, 0755); err != nil {
		return nil, err
	}

	return &FlatBackend{
		kcontext:    kctx,
		pkgdir:      pkgdir,
		cachedir:    cachedir,
		preloadhook: opts.PreLoadHook,
		loadhook:    opts.LoadHook,
		unloadhook:  opts.UnloadHook,
	}, nil
}

func (f *FlatBackend) List(name string) iter.Seq2[*Package, error] {
	return func(yield func(*Package, error) bool) {
		dir, err := os.Open(f.pkgdir)
		if err != nil {
			yield(nil, err)
			return
		}
		defer dir.Close()

		for {
			dirents, err := dir.ReadDir(16)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					yield(nil, err)
				}
				return
			}

			for i := range dirents {
				// skip hidden files
				if strings.HasPrefix(dirents[i].Name(), ".") {
					continue
				}

				var pkg Package
				if err := pkg.parseName(dirents[i].Name()); err != nil {
					if !yield(nil, err) {
						return
					}
				}

				if name != "" && pkg.Name != name {
					continue
				}

				if !yield(&pkg, nil) {
					return
				}
			}
		}
	}
}

func (f *FlatBackend) extract(destDir, ptar string) error {
	opts := map[string]string{
		"location": "ptar://" + ptar,
	}

	store, serializedConfig, err := storage.Open(f.kcontext, opts)
	if err != nil {
		return err
	}

	repo, err := repository.New(f.kcontext, nil, store, serializedConfig)
	if err != nil {
		return err
	}

	locopts := locate.NewDefaultLocateOptions()
	snapids, err := locate.LocateSnapshotIDs(repo, locopts)
	if len(snapids) != 1 {
		return fmt.Errorf("too many snapshot in ptar plugin: %d",
			len(snapids))
	}

	snapid := snapids[0]
	snap, err := snapshot.Load(repo, snapid)
	if err != nil {
		return err
	}

	fsexp, err := fsexporter.NewFSExporter(f.kcontext, &exporter.Options{
		MaxConcurrency: 1,
	}, "fs", opts)
	if err != nil {
		return err
	}

	tmpdir, err := os.MkdirTemp(filepath.Dir(destDir), ".extract-*")
	if err != nil {
		return err
	}

	base := snap.Header.GetSource(0).Importer.Directory
	err = snap.Restore(fsexp, tmpdir, base, &snapshot.RestoreOptions{
		MaxConcurrency: 1,
		Strip:          base,
	})
	if err != nil {
		os.RemoveAll(tmpdir)
		return err
	}

	if err := os.Rename(tmpdir, destDir); err != nil {
		os.RemoveAll(tmpdir)
		return fmt.Errorf("failed to rename: %w", err)
	}

	return nil
}

func (f *FlatBackend) loadmanifest(mpath string) (*Manifest, error) {
	fp, err := os.Open(mpath)
	if err != nil {
		return nil, err
	}
	defer fp.Close()

	var m Manifest
	if err := m.Parse(fp); err != nil {
		return nil, err
	}

	dir := filepath.Dir(mpath)
	for _, conn := range m.Connectors {
		exe := filepath.Join(dir, conn.Executable)
		if !strings.HasPrefix(exe, dir) {
			return nil, fmt.Errorf("bad executable path %q", conn.Executable)
		}

		if _, err := conn.Flags(); err != nil {
			return nil, err
		}
	}

	return &m, nil
}

func (f *FlatBackend) Load(pkg *Package, rd io.Reader) error {
	fp, err := os.CreateTemp(f.pkgdir, "."+pkg.Name+"-*")
	if err != nil {
		return err
	}

	_, err = io.Copy(fp, rd)
	fp.Close()
	if err != nil {
		return err
	}

	// extract and validate its manifest before enabling it.

	extracted := filepath.Join(f.cachedir, strings.TrimSuffix(pkg.Filename(), ".ptar"))
	if err := f.extract(extracted, fp.Name()); err != nil {
		f.unload(fp.Name(), extracted)
		return err
	}

	m, err := f.loadmanifest(filepath.Join(extracted, "manifest.yaml"))
	if err != nil {
		f.unload(fp.Name(), extracted)
		return err
	}

	if f.preloadhook != nil {
		if err := f.preloadhook(m); err != nil {
			f.unload(fp.Name(), extracted)
			return err
		}
	}

	pkgdir := filepath.Join(f.pkgdir, pkg.Filename())
	if err := os.Link(fp.Name(), pkgdir); err != nil {
		f.unload(fp.Name(), extracted)
		return err
	}

	if f.loadhook != nil {
		f.loadhook(m, pkg, extracted)
	}

	return nil
}

func (f *FlatBackend) reload(pkg *Package) error {
	// extract if needed
	ptar := filepath.Join(f.pkgdir, pkg.Filename())
	extracted := filepath.Join(f.cachedir, strings.TrimSuffix(pkg.Filename(), ".ptar"))
	if _, err := os.Stat(extracted); err != nil {
		if err := f.extract(extracted, ptar); err != nil {
			f.unload(ptar, extracted)
			return err
		}
	}

	m, err := f.loadmanifest(filepath.Join(extracted, "manifest.yaml"))
	if err != nil {
		f.unload(ptar, extracted)
		return err
	}

	if f.loadhook != nil {
		f.loadhook(m, pkg, extracted)
	}

	return nil
}

func (f *FlatBackend) LoadAll() error {
	for pkg, err := range f.List("") {
		if err != nil {
			return err
		}
		if err := f.reload(pkg); err != nil {
			return err
		}
	}
	return nil
}

func (f *FlatBackend) unload(pkgfile, extracted string) error {
	err := os.Remove(pkgfile)
	if extracted != "" {
		if e := os.RemoveAll(extracted); err == nil && !errors.Is(e, fs.ErrNotExist) {
			err = e
		}
	}
	return err
}

func (f *FlatBackend) Unload(pkg *Package) error {
	var (
		pkgfile   = filepath.Join(f.pkgdir, pkg.Filename())
		extf      = strings.TrimSuffix(pkg.Filename(), ".ptar")
		extracted = filepath.Join(f.cachedir, extf)
	)
	return f.unload(pkgfile, extracted)
}
