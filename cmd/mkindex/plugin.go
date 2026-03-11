package main

import (
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"strings"

	"github.com/PlakarKorp/pkg"
)

type LocateManifestFunc func(Plugin) (string, error)

type Plugin struct {
	pkg.Recipe
	Edition string
	API     string
}

// Scan recipe files from .../<edition>/<api>/*/recipe.yaml
func ScanPlugins(rootDir string) []Plugin {
	var plugins []Plugin

	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() || d.Name() != "recipe.yaml" {
			return nil
		}

		rel, _ := strings.CutPrefix(path, rootDir+"/")
		parts := strings.Split(rel, "/")
		if len(parts) < 4 {
			return nil
		}

		var p Plugin
		if err := p.Recipe.ParseFile(path); err != nil {
			log.Printf("ERROR: %s: cannot parse recipe: %v", path, err)
		}

		p.Edition = parts[0]
		p.API = parts[1]
		plugins = append(plugins, p)
		return nil
	})
	if err != nil {
		log.Printf("ERROR: WalkDir failed: %v", err)
	}

	return plugins
}

func (p Plugin) CompileIntegration(fn LocateManifestFunc) (*pkg.IntegrationV2, error) {
	manifestFile, err := fn(p)
	if err != nil {
		return nil, fmt.Errorf("%s@%s: cannot find manifest file: %v", p.Name, p.Version, err)
	}

	int, err := pkg.NewIntegrationFromRecipeAndManifestFile(p.Recipe, manifestFile)
	if err != nil {
		return nil, fmt.Errorf("%s@%s: cannot build integration: %v", p.Name, p.Version, err)
	}

	int.Edition = p.Edition
	int.API = p.API
	return int, nil
}
