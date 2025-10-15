package pkg

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

var (
	ErrBadPackageName = errors.New("invalid package name")
)

type Package struct {
	Name            string
	Version         string
	Architecture    string
	OperatingSystem string
}

func (pkg *Package) parseName(name string) error {
	baseName, has := strings.CutSuffix(name, ".ptar")
	if !has {
		return fmt.Errorf("%w %q: does not end with .ptar",
			ErrBadPackageName, name)
	}

	atoms := strings.Split(baseName, "_")
	if len(atoms) != 4 {
		return fmt.Errorf("%w %q: is malformed", ErrBadPackageName, name)
	}

	pkg.Name = atoms[0]
	pkg.Version = atoms[1]
	pkg.OperatingSystem = atoms[2]
	pkg.Architecture = atoms[3]

	return pkg.Validate()
}

func isNameChar(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-'
}

func isOsArchChar(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9'
}

func (pkg *Package) Validate() error {
	if pkg.Name == "" {
		return ErrBadPackageName
	}
	for i := 0; i < len(pkg.Name); i++ {
		if !isNameChar(pkg.Name[i]) {
			return fmt.Errorf("%w %q: contains invalid char '%c",
				ErrBadPackageName, pkg.Name, pkg.Name[i])
		}
	}

	if !semver.IsValid(pkg.Version) {
		return fmt.Errorf("%w: invalid version %q", ErrBadPackageName, pkg.Version)
	}

	for i := 0; i < len(pkg.OperatingSystem); i++ {
		if !isOsArchChar(pkg.OperatingSystem[i]) {
			return fmt.Errorf("%w %q: contains invalid char '%c",
				ErrBadPackageName, pkg.OperatingSystem, pkg.OperatingSystem[i])
		}
	}

	for i := 0; i < len(pkg.Architecture); i++ {
		if !isOsArchChar(pkg.Architecture[i]) {
			return fmt.Errorf("%w %q: contains invalid char '%c",
				ErrBadPackageName, pkg.Architecture, pkg.Architecture[i])
		}
	}

	return nil
}

func (p *Package) Filename() string {
	return fmt.Sprintf("%s_%s_%s_%s.ptar", p.Name, p.Version, p.OperatingSystem, p.OperatingSystem)
}
