package pkg

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Protocol struct {
	Scheme    string `json:"scheme"`    // sftp, s3, http, ...
	Validator any    `json:"validator"` // json schema, override connector schema if defined
}

type Connector struct {
	Type      string     `json:"type"` // importer, exporter, storage
	Protocols []Protocol `json:"protocols"`
	Class     string     `json:"class"`
	SubClass  string     `json:"subclass"`
	Validator any        `json:"validator"` // json schema
}

type IntegrationV2 struct {
	// from path in kloset-plugins <edition>/<api_version>/<name>/recipe.yaml
	Edition string `json:"edition"`
	API     string `json:"api"`

	Tier          string      `json:"tier"`
	Contact       string      `json:"contact"`
	Name          string      `json:"name"`
	DisplayName   string      `json:"display_name"`
	Description   string      `json:"description"`
	Homepage      string      `json:"homepage"`
	Repository    string      `json:"repository"`
	License       string      `json:"license"`
	Tags          []string    `json:"tags"`
	Version       string      `json:"version"` // integration version
	Connectors    []Connector `json:"connectors"`
	Documentation string      `json:"documentation"` // README.md
	Icon          string      `json:"icon"`          // assets/icon.{png,svg}
	Featured      string      `json:"featured"`      // assets/featured.{png,svg}
}

type Index struct {
	Version      string          `json:"version"`
	Timestamp    time.Time       `json:"timestamp"`
	Integrations []IntegrationV2 `json:"integrations"`
}

func recipe_Asset(r *Recipe, filename string) string {
	base := strings.Replace(r.Repository, "/github.com/", "/raw.githubusercontent.com/", 1)
	return fmt.Sprintf("%s/refs/tags/%s/assets/%s", base, r.Version, filename)
}

func NewIntegrationFromRecipeAndManifest(manifestFile string, recipe *Recipe) (*IntegrationV2, error) {
	manifest, err := NewManifestFromFile(manifestFile)
	if err != nil {
		return nil, err
	}

	info := IntegrationV2{
		Name:        manifest.Name,
		DisplayName: manifest.DisplayName,
		Description: manifest.Description,
		Tier:        manifest.Tier,
		Contact:     manifest.Contact,
		Homepage:    manifest.Homepage,
		Repository:  recipe.Repository,
		License:     manifest.License,
		Tags:        make([]string, 0),
		Version:     recipe.Version,
		Connectors:  make([]Connector, 0),
	}
	for _, tag := range manifest.Tags {
		info.Tags = append(info.Tags, tag)
	}

	srcdir := filepath.Dir(manifestFile)

	for _, conn := range manifest.Connectors {
		var c Connector
		c.Type = conn.Type
		c.Class = conn.Class
		c.SubClass = conn.SubClass

		if conn.Validator != "" {
			data, err := os.ReadFile(filepath.Join(srcdir, conn.Validator))
			if err != nil {
				log.Printf("WARNING: cannot read schema %s", conn.Validator)
				continue
			}
			var res map[string]any
			if err := json.Unmarshal(data, &res); err != nil {
				log.Printf("WARNING: failed to decode schema %s", conn.Validator)
				continue
			}
			c.Validator = res
		}

		for _, p := range conn.Protocols {
			var prot Protocol
			prot.Scheme = p
			prot.Validator = nil
			c.Protocols = append(c.Protocols, prot)
		}

		if len(c.Protocols) == 0 {
			log.Printf("WARNING: no protocol defined for connector of type %s", conn.Type)
			continue
		}

		info.Connectors = append(info.Connectors, c)
	}

	if len(info.Connectors) == 0 {
		return nil, fmt.Errorf("no connector defined")
	}

	var data []byte
	path := filepath.Join(srcdir, "README.md")
	data, err = os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("can't open %s: %w", path, err)
		}
	}
	info.Documentation = string(data)

	path = filepath.Join(srcdir, "assets")
	entries, err := os.ReadDir(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("can't read directory %s: %w", path, err)
		}
	}

	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}

	for _, sfx := range []string{".svg", ".png", ".jpg"} {
		if slices.Contains(names, "icon"+sfx) {
			info.Icon = recipe_Asset(recipe, "icon"+sfx)
		}
	}

	for _, sfx := range []string{".svg", ".png", ".jpg"} {
		if slices.Contains(names, "featured"+sfx) {
			info.Featured = recipe_Asset(recipe, "featured"+sfx)
		}
	}

	if info.DisplayName == "" {
		log.Printf("WARNING: %s: missing display name", info.Name)
		info.DisplayName = info.Name
	}
	if info.Description == "" {
		log.Printf("WARNING: %s: missing description", info.Name)
	}
	if info.Homepage == "" {
		log.Printf("WARNING: %s: missing homepage", info.Name)
	}
	if info.License == "" {
		log.Printf("WARNING: %s: missing license", info.Name)
	}
	if info.Icon == "" {
		log.Printf("WARNING: %s: missing icon file", info.Name)
	}
	if info.Featured == "" {
		log.Printf("WARNING: %s: missing featured file", info.Name)
	}
	if info.Documentation == "" {
		log.Printf("WARNING: %s: missing documentation", info.Name)
		info.Documentation = "This integration is not documented yet."
	}

	return &info, nil
}
