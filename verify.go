/*
 * Copyright (c) 2026 Gilles Chehade <gilles@poolp.org>
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
	"errors"
	"io"
)

var ErrUnverified = errors.New("package signature verification failed")

// LocalOrigin is the Origin of a package installed from a filesystem path.
// Not a URL, so that no URL-scoped trust can match it.
const LocalOrigin = "local"

const sigSuffix = ".sum.sig"

// Bounds how much of a signature response is read, so a hostile registry
// cannot stream indefinitely into memory.
const maxSignatureSize = 64 << 10

type Artifact struct {
	Filename  string
	Package   *Package // nil for files that are not packages, such as a recipe
	Origin    string   // registry base URL, or LocalOrigin
	Signature []byte   // the .sum.sig, or nil if it shipped without one
}

// A Verifier decides whether an artifact may be installed. rd is not seekable,
// and a non-nil error aborts the installation. This package holds no keys and
// no policy: the embedding application supplies them.
type Verifier interface {
	Verify(artifact *Artifact, rd io.Reader) error
}

type VerifierFunc func(artifact *Artifact, rd io.Reader) error

func (f VerifierFunc) Verify(artifact *Artifact, rd io.Reader) error {
	return f(artifact, rd)
}
