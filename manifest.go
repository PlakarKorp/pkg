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
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/PlakarKorp/kloset/location"
	"go.yaml.in/yaml/v3"
)

type ManifestConnector struct {
	Type          ConnectorType    `yaml:"type"`
	Class         ResourceClass    `yaml:"class,omitempty"`
	SubClass      ResourceSubClass `yaml:"subclass,omitempty"`
	Validator     string           `yaml:"validator,omitempty"`
	Protocols     []string         `yaml:"protocols"`
	LocationFlags []string         `yaml:"location_flags,omitempty"`
	Executable    string           `yaml:"executable,omitempty"`
	Args          []string         `yaml:"args,omitempty"`
	ExtraFiles    []string         `yaml:"extra_files,omitempty"`

	// Containerized connectors carry an image pin instead of an executable.
	// ImageID is the sha256 of the image, filled at package creation time
	// following a docker build.
	// Image is registry reference to pull from when install pre-built packages.
	ImageID string `yaml:"image_id,omitempty"`
	Image   string `yaml:"image,omitempty"`
}

type Manifest struct {
	Name        string   `yaml:"name"`
	DisplayName string   `yaml:"display_name"`
	Description string   `yaml:"description"`
	Tier        string   `yaml:"tier"`
	Contact     string   `yaml:"contact"`
	Homepage    string   `yaml:"homepage"`
	License     string   `yaml:"license"`
	Tags        []string `yaml:"tags"`
	APIVersion  string   `yaml:"api_version"`

	Connectors []ManifestConnector `yaml:"connectors"`
}

func NewManifestFromFile(path string) (*Manifest, error) {
	var m Manifest
	if err := m.ParseFile(path); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) ParseFile(path string) error {
	fp, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fp.Close()

	return m.Parse(fp)
}

func (m *Manifest) Parse(rd io.Reader) error {
	if err := yaml.NewDecoder(rd).Decode(m); err != nil {
		return fmt.Errorf("failed to decode the manifest: %w", err)
	}

	if len(m.Connectors) == 0 {
		return fmt.Errorf("no connectors defined?")
	}

	for i := range m.Connectors {
		if err := m.Connectors[i].Validate(); err != nil {
			return fmt.Errorf("connector #%b: %w", i, err)
		}
	}

	// Windows really wants executables to end with .exe
	if os.Getenv("GOOS") == "windows" || runtime.GOOS == "windows" {
		for i := range m.Connectors {
			if m.Connectors[i].Executable != "" && !strings.HasSuffix(m.Connectors[i].Executable, ".exe") {
				m.Connectors[i].Executable += ".exe"
			}
		}
	}

	return nil
}

func (conn *ManifestConnector) Flags() (flags location.Flags, err error) {
	for _, flag := range conn.LocationFlags {
		f, err := location.ParseFlag(flag)
		if err != nil {
			return 0, fmt.Errorf("%w: %q", err, flag)
		}
		flags |= f
	}
	return
}

func (conn *ManifestConnector) Validate() error {
	flags, err := conn.Flags()
	if err != nil {
		return err
	}

	var (
		class    = ResourceClass(conn.Class)
		subclass = ResourceSubClass(conn.SubClass)
		ct       = ConnectorType(conn.Type)
	)

	if !class.IsValid() || !subclass.IsValid() {
		return fmt.Errorf("bad class or subclass")
	}

	if subclass != ResourceSubClassUndefined && !subclass.IsSubClassOf(class) {
		return fmt.Errorf("subclass %s is not under class %s", subclass, class)
	}

	if !ct.IsValid() {
		return fmt.Errorf("type %s is invalid", ct)
	}

	if (conn.Executable == "") == (conn.ImageID == "") {
		return fmt.Errorf("connector must set exactly one of executable and image_id")
	}

	if conn.Executable != "" {
		if conn.Image != "" {
			return fmt.Errorf("cannot use image with executable")
		}

		if conn.Executable == "" {
			return fmt.Errorf("executable not set")
		}
	} else {
		if flags&location.FLAG_LOCALFS != 0 {
			return fmt.Errorf("localfs connectors cannot run as containers")
		}
	}

	return nil
}
