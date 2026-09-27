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

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
)

var registryNameRe = regexp.MustCompile(`^[-_a-zA-Z0-9]+$`)

// A Registry is an additional package registry, laid out like the official
// distribution tree: the integrations index at its root and one directory
// per edition below it.
type Registry struct {
	Name string
	URL  string
}

type registry struct {
	name   string
	url    *url.URL
	rawurl string // as configured
}

// CheckRegistries reports whether [New] would accept regs as
// [Options.Registries].  The error wraps [ErrBadRegistry].
func CheckRegistries(regs []Registry) error {
	_, err := parseRegistries(regs)
	return err
}

func parseRegistries(regs []Registry) ([]registry, error) {
	var ret []registry
	for _, r := range regs {
		if !registryNameRe.MatchString(r.Name) {
			return nil, fmt.Errorf("%w %q: bad name", ErrBadRegistry, r.Name)
		}
		// "official" names the official index in the warnings of
		// [Manager.QueryAll].
		if r.Name == "official" {
			return nil, fmt.Errorf("%w %q: reserved name", ErrBadRegistry, r.Name)
		}
		if slices.ContainsFunc(ret, func(reg registry) bool { return reg.name == r.Name }) {
			return nil, fmt.Errorf("%w %q: duplicate name", ErrBadRegistry, r.Name)
		}

		u, err := url.Parse(r.URL)
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", ErrBadRegistry, r.Name, err)
		}
		if u.Scheme != "https" && u.Scheme != "http" {
			return nil, fmt.Errorf("%w %q: unsupported scheme %q", ErrBadRegistry, r.Name, u.Scheme)
		}
		if u.Host == "" {
			return nil, fmt.Errorf("%w %q: no host", ErrBadRegistry, r.Name)
		}
		// Registry requests never authenticate, and endpoints are
		// joined to the URL path only.
		if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
			return nil, fmt.Errorf("%w %q: URL with credentials, query or fragment", ErrBadRegistry, r.Name)
		}

		ret = append(ret, registry{name: r.Name, url: u, rawurl: r.URL})
	}
	return ret, nil
}

// A source is a distribution tree packages are fetched from: the official
// one or an additional registry.
type source struct {
	url *url.URL // above the edition directories

	// needsAuth tells whether packages require authorization.
	needsAuth bool

	// origin is the URL of the registry as configured, recorded on
	// install, or "" for the official tree.
	origin string
}

func (r *registry) source() *source {
	return &source{url: r.url, origin: r.rawurl}
}
