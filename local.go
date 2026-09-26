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
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
)

// maxLocalFileSize bounds how much of a package's README.md or a
// connector's JSON schema is read off disk.
const maxLocalFileSize = 1 << 20 // 1 MiB

// maxLocalIconSize bounds a package's assets/icon.{svg,png}. It is far
// smaller than maxLocalFileSize because, unlike a README or a JSON schema,
// an icon can't be usefully truncated: past this size it is skipped
// entirely rather than cut short into a broken image.
const maxLocalIconSize = 256 << 10 // 256 KiB

// integrationFromManifest fills in from the local manifest m of an installed
// package. It is meant as a fallback for integrations missing from the
// remote catalog, so that an installed, unofficial integration still gets a
// complete card: display name, description, connectors with their JSON
// schemas, README, and so on.
//
// dir is the directory the package was extracted to, used to resolve the
// manifest's relative paths (connector validators, README.md). Reads are
// confined to dir with [os.Root], which also rejects a symlink escaping
// it, so a malicious package cannot use one to make Query serve up
// arbitrary files from the host.
//
// Only the fields actually set in the manifest override in: an empty
// DisplayName or a nil Tags would otherwise clobber the defaults Query
// already filled in (the package name, and a non-nil empty slice).
//
// If the package ships assets/icon.svg or assets/icon.png, in.Icon is set
// to a data URI carrying it, so the UI still has something to show; the
// remote catalog's Icon, an https URL, takes precedence over it once
// merged in Query.
func integrationFromManifest(in *Integration, m *Manifest, dir string) {
	if m.DisplayName != "" {
		in.DisplayName = m.DisplayName
	}
	in.Description = m.Description
	in.Homepage = m.Homepage
	in.License = m.License
	if m.Tags != nil {
		in.Tags = m.Tags
	}

	// A root that can't be opened leaves connectors without a
	// validator and no documentation; the rest of the manifest still
	// applies below.
	root, err := os.OpenRoot(dir)
	if err != nil {
		root = nil
	} else {
		defer root.Close()
	}

	in.Connectors = make([]Connector, len(m.Connectors))
	for i := range m.Connectors {
		mc := &m.Connectors[i]

		conn := Connector{
			Type:      string(mc.Type),
			Class:     string(mc.Class),
			SubClass:  string(mc.SubClass),
			Protocols: make([]Protocol, 0, len(mc.Protocols)),
		}
		for _, proto := range mc.Protocols {
			conn.Protocols = append(conn.Protocols, Protocol{Scheme: proto})
		}
		if root != nil {
			conn.Validator = readValidator(root, mc.Validator)
		}

		in.Connectors[i] = conn
	}

	in.Types.Source = in.HasConnectorType(string(ConnectorTypeImporter))
	in.Types.Destination = in.HasConnectorType(string(ConnectorTypeExporter))
	in.Types.Storage = in.HasConnectorType(string(ConnectorTypeStorage))

	if root != nil {
		in.Documentation = readDocumentation(root)
		in.Icon = readIcon(root)
	}
}

// readLocalFile reads name through root, so it cannot resolve outside the
// package's extracted directory (including via a symlink). It stats before
// opening and refuses anything that isn't a regular file, so a FIFO shipped
// by a malicious package is rejected without ever calling open on it: open
// on a FIFO blocks until a writer shows up, which would otherwise stall
// Query forever. The read is bounded to maxLocalFileSize. ok is false
// whenever the content can't be produced (missing file, not a regular file,
// stat or read error); the caller must treat that as "no data" rather than
// fail Query.
func readLocalFile(root *os.Root, name string) (data []byte, ok bool) {
	fi, err := root.Stat(name)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, false
	}

	fp, err := root.Open(name)
	if err != nil {
		return nil, false
	}
	defer fp.Close()

	data, err = io.ReadAll(io.LimitReader(fp, maxLocalFileSize))
	if err != nil {
		return nil, false
	}
	return data, true
}

// readValidator reads and JSON-decodes the schema at path, opened through
// root so it cannot resolve outside the package's extracted directory
// (including via a symlink). It returns nil whenever the schema can't be
// produced (no validator declared, path escaping root, missing file,
// invalid JSON): a bad schema only leaves the connector without a
// validator, it must not fail Query.
func readValidator(root *os.Root, path string) any {
	if path == "" {
		return nil
	}

	data, ok := readLocalFile(root, path)
	if !ok {
		return nil
	}

	var schema any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil
	}
	return schema
}

// readDocumentation returns the content of README.md at the root of the
// package's extracted directory, or the empty string if it is missing,
// unreadable, not a regular file, or (per [os.Root]) a symlink escaping that
// directory.
func readDocumentation(root *os.Root) string {
	data, ok := readLocalFile(root, "README.md")
	if !ok {
		return ""
	}
	return string(data)
}

// readIcon returns assets/icon.svg or, failing that, assets/icon.png from
// the root of the package's extracted directory, encoded as a data URI.
// It returns "" if neither is present, readable, a regular file, within
// maxLocalIconSize, or (per [os.Root]) reachable without escaping that
// directory via a symlink.
func readIcon(root *os.Root) string {
	if data, ok := readLocalIcon(root, "assets/icon.svg"); ok {
		return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(data)
	}
	if data, ok := readLocalIcon(root, "assets/icon.png"); ok {
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	}
	return ""
}

// readLocalIcon reads name through root under the same containment and
// regular-file rules as readLocalFile, but for maxLocalIconSize: unlike
// readLocalFile it refuses a file over that bound instead of truncating it,
// since a truncated image is broken rather than merely shorter.
func readLocalIcon(root *os.Root, name string) (data []byte, ok bool) {
	fi, err := root.Stat(name)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxLocalIconSize {
		return nil, false
	}
	return readLocalFile(root, name)
}
