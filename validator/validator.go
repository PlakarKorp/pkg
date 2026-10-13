package validator

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"path"

	"github.com/PlakarKorp/pkg"
	"github.com/PlakarKorp/pkg/validator/jsonschema"
	"github.com/PlakarKorp/pkg/validator/manifest"
)

type Validator struct {
	Logger *log.Logger
}

func (v *Validator) logf(format string, args ...any) {
	if v.Logger != nil {
		v.Logger.Printf(format, args...)
	}
}

func (v *Validator) runValidate(fsys fs.FS) error {
	m, err := readAndValidateManifest(fsys)
	if err != nil {
		return fmt.Errorf("manifest.yaml: %w", err)
	}

	var errs []error
	v.logf("validating README.md")
	if err := validateReadme(fsys); err != nil {
		errs = append(errs, fmt.Errorf("README.md: %w", err))
	}
	for _, p := range manifest.Schemas(m) {
		v.logf("validating schema %s", p)
		if err := validateSchema(fsys, p); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
		}
	}
	return errors.Join(errs...)
}

func readAndValidateManifest(fsys fs.FS) (*pkg.Manifest, error) {
	f, err := fsys.Open("manifest.yaml")
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return manifest.ReadAndValidate(f)
}

func validateReadme(fsys fs.FS) error {
	fi, err := fs.Stat(fsys, "README.md")
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	return nil
}

func validateSchema(fsys fs.FS, p string) error {
	// need path.Clean otherwise the read errors. ./importer/schema.json => importer/schema.json
	f, err := fsys.Open(path.Clean(p))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return jsonschema.ReadAndValidate(f)
}
