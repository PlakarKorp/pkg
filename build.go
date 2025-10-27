package pkg

import "errors"

type BuildOptions struct{}

func Build(*BuildOptions) error {
	return errors.ErrUnsupported
}
