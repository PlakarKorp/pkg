package pkg

import "errors"

type CreateOptions struct{}

func (p *Manager) Create(manifest *Manifest, opts *CreateOptions) error {
	return errors.ErrUnsupported
}
