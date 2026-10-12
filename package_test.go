package pkg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPackageParseNameValid(t *testing.T) {
	var p Package
	require.NoError(t, p.parseName("s3_v1.2.3_linux_amd64.ptar"))
	require.Equal(t, Package{Name: "s3", Version: "v1.2.3", OperatingSystem: "linux", Architecture: "amd64"}, p)
}

func TestPackageParseNameContainer(t *testing.T) {
	// Container packages use OSContainer as their OS atom, keeping the
	// regular 4-atom name format.
	var p Package
	require.NoError(t, p.parseName("s3_v1.2.3_oci_amd64.ptar"))
	require.Equal(t, OSContainer, p.OperatingSystem)
	require.Equal(t, "s3_v1.2.3_oci_amd64.ptar", p.Filename())
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
		{"empty version field", "s3__linux_amd64.ptar"},
		{"empty os field", "s3_v1.2.3__amd64.ptar"},
		{"empty arch field", "s3_v1.2.3_linux_.ptar"},
		{"version without v prefix", "s3_1.2.3_linux_amd64.ptar"},
		{"invalid version", "s3_notaversion_linux_amd64.ptar"},
		{"major-only version", "s3_v1_linux_amd64.ptar"},
		{"major.minor version", "s3_v1.2_linux_amd64.ptar"},
		{"version with build metadata", "s3_v1.2.3+build.1_linux_amd64.ptar"},
		{"invalid char in name", "s3$_v1.2.3_linux_amd64.ptar"},
		{"name starting with hyphen", "-rf_v1.2.3_linux_amd64.ptar"},
		{"invalid char in os", "s3_v1.2.3_lin/ux_amd64.ptar"},
		{"invalid char in arch", "s3_v1.2.3_linux_amd-64.ptar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p Package
			require.ErrorIs(t, p.parseName(tt.in), ErrBadPackageName)
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
			name: "valid prerelease",
			pkg:  Package{Name: "s3", Version: "v1.2.3-rc.1", OperatingSystem: "linux", Architecture: "amd64"},
		},
		{
			name:    "shorthand version",
			pkg:     Package{Name: "s3", Version: "v1.2", OperatingSystem: "linux", Architecture: "amd64"},
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
			if tt.wantErr {
				require.ErrorIs(t, err, ErrBadPackageName)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestPackageFilename(t *testing.T) {
	p := Package{Name: "s3", Version: "v1.2.3", OperatingSystem: "linux", Architecture: "amd64"}
	require.Equal(t, "s3_v1.2.3_linux_amd64.ptar", p.Filename())
}

// Filename and parseName must round-trip: a name produced by Filename
// must parse back into an equal Package.
func TestPackageFilenameRoundTrip(t *testing.T) {
	orig := Package{Name: "my-plugin", Version: "v2.0.0-beta.1", OperatingSystem: "darwin", Architecture: "arm64"}

	var got Package
	require.NoError(t, got.parseName(orig.Filename()))
	require.Equal(t, orig, got)
}
