package pkg

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

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
	Version     string   `yaml:"version"`

	Connectors []ManifestConnector `yaml:"connectors"`
}

func (m *Manifest) Parse(rd io.Reader) error {
	if err := yaml.NewDecoder(rd).Decode(m); err != nil {
		return fmt.Errorf("failed to decode the manifest: %w", err)
	}

	// We really want version to start with a 'v'
	if m.Version != "" && m.Version[0] != 'v' {
		m.Version = "v" + m.Version
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
