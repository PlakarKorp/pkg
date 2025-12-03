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
	Type          string   `yaml:"type"`
	Protocols     []string `yaml:"protocols"`
	LocationFlags []string `yaml:"location_flags"`
	Executable    string   `yaml:"executable"`
	Args          []string `yaml:"args"`
	ExtraFiles    []string `yaml:"extra_files"`
}

type Manifest struct {
	Name        string   `yaml:"name"`
	DisplayName string   `yaml:"display_name"`
	Description string   `yaml:"description"`
	Homepage    string   `yaml:"homepage"`
	License     string   `yaml:"license"`
	Tags        []string `yaml:"tags"`
	APIVersion  string   `yaml:"api_version"`

	Connectors []ManifestConnector `yaml:"connectors"`
}

func (m *Manifest) Parse(rd io.Reader) error {
	if err := yaml.NewDecoder(rd).Decode(m); err != nil {
		return fmt.Errorf("failed to decode the manifest: %w", err)
	}

	// Windows really wants executables to end with .exe
	if os.Getenv("GOOS") == "windows" || runtime.GOOS == "windows" {
		for i := range m.Connectors {
			if !strings.HasSuffix(m.Connectors[i].Executable, ".exe") {
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
