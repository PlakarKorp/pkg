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

import "time"

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

type Integration struct {
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

	Id            string                  `json:"id"`
	Types         IntegrationTypes        `json:"types"`
	Stage         string                  `json:"stage"`
	Installation  IntegrationInstallation `json:"installation"`
	LatestVersion string                  `json:"latest_version"`
}

type IntegrationIndex struct {
	Version      string        `json:"version"`
	Timestamp    time.Time     `json:"timestamp"`
	Integrations []Integration `json:"integrations"`
}

func (int *Integration) HasConnectorType(ct string) bool {
	for i := range int.Connectors {
		if int.Connectors[i].Type == ct {
			return true
		}
	}
	return false
}
