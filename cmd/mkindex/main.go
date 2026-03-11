package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"github.com/PlakarKorp/pkg"
)

func main() {
	var edition string
	var api string
	var recipePath string
	var manifestPath string
	var check bool

	flag.BoolVar(&check, "check", false, "Check only")
	flag.StringVar(&edition, "edition", "community", "Edition")
	flag.StringVar(&api, "api", "v1.1.0", "API version")
	flag.StringVar(&recipePath, "r", "", "Path to recipe file")
	flag.StringVar(&manifestPath, "m", "./manifest.yaml", "Path to manifest file")
	flag.Parse()

	var r pkg.Recipe
	r.Name = "@@NAME@@"
	r.Repository = "@@REPOSITORY@@"
	r.Version = "@@VERSION@@"

	if recipePath != "" {
		if err := r.ParseFile(recipePath); err != nil {
			log.Fatalf("ERROR: failed to parse recipe: %v", err)
		}
	}

	info, err := pkg.NewIntegrationFromRecipeAndManifest(manifestPath, &r)
	if err != nil {
		log.Fatalf("ERROR: failed to parse manifest: %v", err)
	}
	info.API = api
	info.Edition = edition

	if !check {
		data, _ := json.MarshalIndent(info, "", "   ")
		fmt.Println(string(data))
	}
}
