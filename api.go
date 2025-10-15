package pkg

type IntegrationInstallation struct {
	Status    string `json:"status"`
	Version   string `json:"version,omitempty"`
	Available bool   `json:"available"`
}

type IntegrationTypes struct {
	Storage     bool `json:"storage"`
	Source      bool `json:"source"`
	Destination bool `json:"destination"`
	Provider    bool `json:"provider"`
}

type Integration struct {
	Id            string           `json:"id"`
	Name          string           `json:"name"`
	DisplayName   string           `json:"display_name"`
	Description   string           `json:"description"`
	Homepage      string           `json:"homepage"`
	Repository    string           `json:"repository"`
	License       string           `json:"license"`
	Tags          []string         `json:"tags"`
	APIVersion    string           `json:"api_version"`
	LatestVersion string           `json:"latest_version"`
	Stage         string           `json:"stage"`
	Types         IntegrationTypes `json:"types"`

	Documentation string `json:"documentation"` // README.md
	Icon          string `json:"icon"`          // assets/icon.{png,svg}
	Featured      string `json:"featured"`      // assets/featured.{png,svg}

	Installation IntegrationInstallation `json:"installation"`
}

type IntegrationIndex struct {
	Plugins []Integration `json:"integrations"`
}
