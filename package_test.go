package pkg

import (
	"errors"
	"testing"
)

func TestPackageParseNameValid(t *testing.T) {
	var p Package
	if err := p.parseName("s3_v1.2.3_linux_amd64.ptar"); err != nil {
		t.Fatalf("parseName: %v", err)
	}
	if p.Name != "s3" {
		t.Errorf("Name = %q, want s3", p.Name)
	}
	if p.Version != "v1.2.3" {
		t.Errorf("Version = %q, want v1.2.3", p.Version)
	}
	if p.OperatingSystem != "linux" {
		t.Errorf("OperatingSystem = %q, want linux", p.OperatingSystem)
	}
	if p.Architecture != "amd64" {
		t.Errorf("Architecture = %q, want amd64", p.Architecture)
	}
}

func TestPackageParseNameErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"missing .ptar suffix", "s3_v1.2.3_linux_amd64"},
		{"too few fields", "s3_v1.2.3_linux.ptar"},
		{"too many fields", "s3_v1.2.3_linux_amd64_extra.ptar"},
		{"empty name field", "_v1.2.3_linux_amd64.ptar"},
		{"version without v prefix", "s3_1.2.3_linux_amd64.ptar"},
		{"invalid version", "s3_notaversion_linux_amd64.ptar"},
		{"invalid char in name", "s3$_v1.2.3_linux_amd64.ptar"},
		{"invalid char in os", "s3_v1.2.3_lin/ux_amd64.ptar"},
		{"invalid char in arch", "s3_v1.2.3_linux_amd-64.ptar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p Package
			err := p.parseName(tt.in)
			if err == nil {
				t.Fatalf("parseName(%q) = nil, want error", tt.in)
			}
			if !errors.Is(err, ErrBadPackageName) {
				t.Errorf("parseName(%q) error = %v, want ErrBadPackageName", tt.in, err)
			}
		})
	}
}

func TestPackageValidate(t *testing.T) {
	tests := []struct {
		name    string
		pkg     Package
		wantErr bool
	}{
		{
			name: "valid",
			pkg:  Package{Name: "s3", Version: "v1.2.3", OperatingSystem: "linux", Architecture: "amd64"},
		},
		{
			name: "valid with hyphen in name",
			pkg:  Package{Name: "my-plugin", Version: "v1.0.0", OperatingSystem: "darwin", Architecture: "arm64"},
		},
		{
			name:    "empty name",
			pkg:     Package{Name: "", Version: "v1.2.3", OperatingSystem: "linux", Architecture: "amd64"},
			wantErr: true,
		},
		{
			name:    "version missing v prefix",
			pkg:     Package{Name: "s3", Version: "1.2.3", OperatingSystem: "linux", Architecture: "amd64"},
			wantErr: true,
		},
		{
			name:    "underscore in name is invalid",
			pkg:     Package{Name: "s3_extra", Version: "v1.2.3", OperatingSystem: "linux", Architecture: "amd64"},
			wantErr: true,
		},
		{
			name:    "hyphen in os is invalid",
			pkg:     Package{Name: "s3", Version: "v1.2.3", OperatingSystem: "li-nux", Architecture: "amd64"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pkg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestPackageFilename(t *testing.T) {
	p := Package{Name: "s3", Version: "v1.2.3", OperatingSystem: "linux", Architecture: "amd64"}
	if got, want := p.Filename(), "s3_v1.2.3_linux_amd64.ptar"; got != want {
		t.Errorf("Filename() = %q, want %q", got, want)
	}
}

// Filename and parseName must round-trip: a name produced by Filename
// must parse back into an equal Package.
func TestPackageFilenameRoundTrip(t *testing.T) {
	orig := Package{Name: "my-plugin", Version: "v2.0.0-beta.1", OperatingSystem: "darwin", Architecture: "arm64"}

	var got Package
	if err := got.parseName(orig.Filename()); err != nil {
		t.Fatalf("parseName(%q): %v", orig.Filename(), err)
	}
	if got != orig {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, orig)
	}
}
