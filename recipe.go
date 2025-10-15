package pkg

import (
	"fmt"
	"io"
	"os"
	"runtime"

	"go.yaml.in/yaml/v3"
)

type Recipe struct {
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	Repository string `yaml:"repository"`
	// Checksum   string `yaml:"checksum"`
}

func (recipe *Recipe) Parse(rd io.Reader) error {
	return yaml.NewDecoder(rd).Decode(recipe)
}

// xxx unused
func (recipe *Recipe) PkgName() string {
	GOOS := runtime.GOOS
	GOARCH := runtime.GOARCH
	if goosEnv := os.Getenv("GOOS"); goosEnv != "" {
		GOOS = goosEnv
	}
	if goarchEnv := os.Getenv("GOARCH"); goarchEnv != "" {
		GOARCH = goarchEnv
	}

	return fmt.Sprintf("%s_%s_%s_%s.ptar", recipe.Name, recipe.Version, GOOS, GOARCH)
}
