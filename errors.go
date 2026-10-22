package pkg

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// ArtifactKind is the file an [InstallError] is about.
type ArtifactKind int

const (
	ArtifactRecipe ArtifactKind = iota + 1
	ArtifactPackage
	ArtifactSignature
)

func (k ArtifactKind) String() string {
	switch k {
	case ArtifactRecipe:
		return "recipe"
	case ArtifactPackage:
		return "package"
	case ArtifactSignature:
		return "signature"
	}
	return "artifact"
}

// InstallError reports a package that could not be fetched or installed.
// Error gives the full detail, URL included, and is meant for logs; the
// fields let a caller build a shorter message for the user. Err is the cause,
// to be matched with errors.Is: [ErrNotFound], [ErrForbidden],
// [ErrAuthorizationRequired], [ErrUnexpectedStatus], [ErrRecipeMismatch], or
// whatever the transport or the recipe parser returned.
type InstallError struct {
	Name       string       // requested package name, always set
	Package    *Package     // nil until the version is resolved
	Edition    string       // the resolved edition
	Artifact   ArtifactKind // the file that failed
	URL        string       // where it was fetched from
	StatusCode int          // 0 when there was no HTTP response
	Err        error
}

func (e *InstallError) Error() string {
	what := e.Name
	if e.Package != nil {
		what += " " + e.Package.Version
	}
	return fmt.Sprintf("install %s: %s %s: %v", what, e.Artifact, e.URL, e.Err)
}

func (e *InstallError) Unwrap() error {
	return e.Err
}

func newInstallError(name string, pkg *Package, edition string, kind ArtifactKind, u *url.URL, err error) *InstallError {
	e := &InstallError{
		Name:     name,
		Package:  pkg,
		Edition:  edition,
		Artifact: kind,
		URL:      u.String(),
		Err:      err,
	}

	var se *statusError
	if errors.As(err, &se) {
		e.StatusCode = se.code
	}
	return e
}

// statusError is a non-200 response, unwrapping to the sentinel its status
// maps to.
type statusError struct {
	code   int
	status string
	err    error
}

func newStatusError(resp *http.Response) *statusError {
	se := &statusError{code: resp.StatusCode, status: resp.Status}
	switch resp.StatusCode {
	case http.StatusNotFound:
		se.err = ErrNotFound
	case http.StatusUnauthorized:
		se.err = ErrAuthorizationRequired
	case http.StatusForbidden:
		se.err = ErrForbidden
	default:
		se.err = ErrUnexpectedStatus
	}
	return se
}

func (e *statusError) Error() string {
	return fmt.Sprintf("%v (%s)", e.err, e.status)
}

func (e *statusError) Unwrap() error {
	return e.err
}
