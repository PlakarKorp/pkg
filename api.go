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

type Protocol struct {
	Scheme string `json:"scheme"`
	// TODO add validator
}

type Connector struct {
	Type      string     `json:"type"`
	Protocols []Protocol `json:"protocols"`
	Class     string     `json:"class"`
	Subclass  string     `json:"subclass"`
	// TODO add validator
}

type Integration struct {
	Edition       string      `json:"edition"`
	API           string      `json:"api"`
	Tier          string      `json:"tier"`
	Contact       string      `json:"contact"`
	Name          string      `json:"name"`
	DisplayName   string      `json:"display_name"`
	Description   string      `json:"description"`
	Homepage      string      `json:"homepage"`
	Repository    string      `json:"repository"`
	License       string      `json:"license"`
	Tags          []string    `json:"tags"`
	Version       string      `json:"version"`
	Connectors    []Connector `json:"connectors"`
	Documentation string      `json:"documentation"`

	/*
	   Stage   string           `json:"stage"`
	   Types   IntegrationTypes `json:"types"`
	   Version string           `json:"version"`

	   Icon          string `json:"icon"`          // assets/icon.{png,svg}
	   Featured      string `json:"featured"`      // assets/featured.{png,svg}

	   Installation IntegrationInstallation `json:"installation"`
	*/
}

type IntegrationIndex struct {
	Integrations []Integration `json:"integrations"`
}
