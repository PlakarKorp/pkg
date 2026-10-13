package validator

import "os"

// ValidateRepo validates an integration's source directory, as checked out in
// CI.
func (v *Validator) ValidateRepo(dir string) error {
	return v.runValidate(os.DirFS(dir))
}
