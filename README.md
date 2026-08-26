# pkg

[![Go Reference](https://pkg.go.dev/badge/github.com/PlakarKorp/pkg.svg)](https://pkg.go.dev/github.com/PlakarKorp/pkg)
[![License: ISC](https://img.shields.io/badge/License-ISC-blue.svg)](LICENSE)

pkg is the Plakar package manager, extracted to its own repo for
easier reuse across projects.


## Concepts

There are two layers of APIs:

 - manager: the object that implements the package manager logic,
   constructed with `New`.
 - backend: the logic for how to list, add and remove package locally.
   One implementation, the `FlatBackend` is provided.


## Example usage

```go
var plugindir, cachedir string  // fill these
var token string                // for authentication

var kctx *kcontext.KContext

backend, err := pkg.NewFlatBackend(kctx, plugdir, cachedir, &pkg.FlatBackendOptions{
	PreLoadHook: pkgpreloadhook,
	LoadHook:    pkgloadhook,
	UnloadHook:  pkgunloadhook,
})
if err != nil {
	return fmt.Errorf("failed to init the package manager: %w", err)
}

manager, err := pkg.New(backend, &pkg.Options{
	InstallURL:      "https://plakar.io/dist/plugins/kloset/community/",
	ApiURL:          "https://api.plakar.io/",
	BinaryNeedsAuth: true,
	UserAgent:       "myclient/v1.2.3",
	RequestHook:     pkg.WithBearer(func() (string, error) { return token, nil }),
})
if err != nil {
	return fmt.Errorf("failed to init the package manager: %w", err)
}

if err := backend.LoadAll(); err != nil {
	return fmt.Errorf("failed to load packages: %w", err)
}
```
