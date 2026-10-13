package validator

import (
	"fmt"
	"io/fs"
	"path"

	_ "github.com/PlakarKorp/integrations/ptar/storage"
	"github.com/PlakarKorp/kloset/connectors/storage"
	"github.com/PlakarKorp/kloset/kcontext"
	"github.com/PlakarKorp/kloset/locate"
	"github.com/PlakarKorp/kloset/repository"
	"github.com/PlakarKorp/kloset/snapshot"
)

// ValidatePtar validates an integration's content (manifest, schema.json...) from a built ptar.
// Kcontext with cache+logger is needed because we use kloset storage to read the ptar
func (v *Validator) ValidatePtar(kctx *kcontext.KContext, ptar string) error {
	store, config, err := storage.Open(kctx, map[string]string{
		"location": "ptar://" + ptar,
	})
	if err != nil {
		return fmt.Errorf("open ptar: %w", err)
	}
	defer func() { _ = store.Close(kctx) }()

	// need to build the state for listing
	repo, err := repository.NewRepository(kctx, nil, store, config, &repository.RepositoryOpts{
		DoRebuild:    true,
		RWStateCache: true,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	snapids, err := locate.LocateSnapshotIDs(repo, locate.NewDefaultLocateOptions())
	if err != nil {
		return fmt.Errorf("list snapshots: %w", err)
	}
	if len(snapids) != 1 {
		return fmt.Errorf("ptar holds %d snapshots, want 1", len(snapids))
	}

	snap, err := snapshot.Load(repo, snapids[0])
	if err != nil {
		return fmt.Errorf("load snapshot: %w", err)
	}
	defer func() { _ = snap.Close() }()

	vfs, err := snap.Filesystem()
	if err != nil {
		return fmt.Errorf("open snapshot filesystem: %w", err)
	}

	return v.runValidate(rootedFS{vfs, snap.Header.GetSource(0).Importer.Directory})
}

type rootedFS struct {
	fsys fs.FS
	root string
}

// We want rootedFS and fsys to match fs.FS interface, enforcing it at compile time
var _ fs.FS = rootedFS{}

func (r rootedFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return r.fsys.Open(path.Join(r.root, name))
}
