package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/PlakarKorp/pkg"
)

func main() {
	var klosetPluginsDir string
	var integrationsDir string
	var cacheDir string
	var checkOnly bool

	flag.BoolVar(&checkOnly, "check", false, "Only check the manifest")
	flag.StringVar(&klosetPluginsDir, "kloset-plugins", "", "Path to the kloset-plugins repository")
	flag.StringVar(&cacheDir, "cache", "/tmp/integrations-cache", "Path to the cache directory")
	flag.StringVar(&integrationsDir, "integrations", "", "Path to the integrations root directory")
	flag.Parse()

	fn := manifestFromRemoteRepositoryWithCache(cacheDir)
	if integrationsDir != "" {
		fn = manifestFromLocalRepository(integrationsDir)
	}

	var res pkg.Index
	res.Timestamp = time.Now()
	res.Version = "v1.0.0"

	for _, p := range ScanPlugins(klosetPluginsDir) {
		int, err := p.CompileIntegration(fn)
		if err != nil {
			log.Printf("ERROR: %s", err)
			continue
		}
		res.Integrations = append(res.Integrations, *int)
	}

	if checkOnly {
		return
	}

	WriteJSON("out.json", &res)
}

func manifestFromLocalRepository(dir string) LocateManifestFunc {
	return func(p Plugin) (string, error) {
		name := filepath.Base(p.Repository)
		return filepath.Join(dir, name, "manifest.yaml"), nil
	}
}

func manifestFromRemoteRepositoryWithCache(cachedir string) LocateManifestFunc {
	return func(p Plugin) (string, error) {
		tmpdir, err := GitCloneTag(p.Repository, p.Version, cachedir)
		if err != nil {
			return "", fmt.Errorf("git clone failed: %w", err)
		}
		return filepath.Join(tmpdir, "manifest.yaml"), nil
	}
}
