package pkg

import (
	"io"
	"iter"
)

type Backend interface {
	// List returns an iterator of plugin names,
	// e.g. s3_v1.0.0_openbsd_amd64.ptar, optionally filtered by
	// the given name, or an error.
	List(name string) iter.Seq2[*Package, error]

	// Load a plugin' ptar from the given reader.
	Load(*Package, io.Reader) error

	// Unload a plugin
	Unload(*Package) error
}
