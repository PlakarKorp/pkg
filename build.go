package pkg

import "errors"

type CreateOptions struct{}

func Create(*CreateOptions) error {
	return errors.ErrUnsupported
}

type BuildOptions struct{}

func Build(*CreateOptions) error {
	return errors.ErrUnsupported
}
