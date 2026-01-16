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
