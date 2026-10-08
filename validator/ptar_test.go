package validator

import (
	"bytes"
	"io/fs"
	"log"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

const (
	goodSchema = `{"type": "object"}`
	badSchema  = `{"type": "nope"}`
)

var readme = &fstest.MapFile{Data: []byte("# Test\n")}

func manifestWith(connectors string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("name: test\ndisplay_name: Test\ndescription: Test.\nconnectors:\n" + connectors)}
}

const (
	importerA = "  - {type: importer, executable: x, protocols: [x], validator: ./a.json}\n"
	exporterA = "  - {type: exporter, executable: x, protocols: [x], validator: ./a.json}\n"
	exporterB = "  - {type: exporter, executable: x, protocols: [x], validator: sub/b.json}\n"
	noSchema  = "  - {type: storage, executable: x, protocols: [x]}\n"
)

func TestValidateFS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fsys    fstest.MapFS
		wantErr []string // substrings the error must contain, nil for success
	}{
		{
			name: "valid",
			fsys: fstest.MapFS{
				"README.md":     readme,
				"manifest.yaml": manifestWith(importerA + exporterB),
				"a.json":        {Data: []byte(goodSchema)},
				"sub/b.json":    {Data: []byte(goodSchema)},
			},
		},
		{
			name:    "connector without a schema",
			fsys:    fstest.MapFS{"README.md": readme, "manifest.yaml": manifestWith(importerA + noSchema)},
			wantErr: []string{"manifest.yaml", "connector #1: validator is required"},
		},
		{
			name: "shared schema",
			fsys: fstest.MapFS{
				"README.md":     readme,
				"manifest.yaml": manifestWith(importerA + exporterA),
				"a.json":        {Data: []byte(goodSchema)},
			},
		},
		{
			name: "missing README",
			fsys: fstest.MapFS{
				"manifest.yaml": manifestWith(importerA),
				"a.json":        {Data: []byte(goodSchema)},
			},
			wantErr: []string{"README.md", "file does not exist"},
		},
		{
			name: "README is a directory",
			fsys: fstest.MapFS{
				"README.md/x":   {Data: []byte("x")},
				"manifest.yaml": manifestWith(importerA),
				"a.json":        {Data: []byte(goodSchema)},
			},
			wantErr: []string{"README.md", "not a regular file"},
		},
		{
			name: "README not at the root",
			fsys: fstest.MapFS{
				"docs/README.md": readme,
				"manifest.yaml":  manifestWith(importerA),
				"a.json":         {Data: []byte(goodSchema)},
			},
			wantErr: []string{"README.md", "file does not exist"},
		},
		{
			name:    "missing manifest",
			fsys:    fstest.MapFS{},
			wantErr: []string{"manifest.yaml"},
		},
		{
			name:    "invalid manifest",
			fsys:    fstest.MapFS{"manifest.yaml": {Data: []byte("name: test\n")}},
			wantErr: []string{"manifest.yaml", "display_name is required"},
		},
		{
			name:    "missing schema",
			fsys:    fstest.MapFS{"README.md": readme, "manifest.yaml": manifestWith(importerA)},
			wantErr: []string{"./a.json"},
		},
		{
			name: "invalid schema",
			fsys: fstest.MapFS{
				"README.md":     readme,
				"manifest.yaml": manifestWith(importerA),
				"a.json":        {Data: []byte(badSchema)},
			},
			wantErr: []string{"./a.json", "compile schema"},
		},
		{
			name: "reports every bad schema",
			fsys: fstest.MapFS{
				"README.md":     readme,
				"manifest.yaml": manifestWith(importerA + exporterB),
				"a.json":        {Data: []byte(badSchema)},
			},
			wantErr: []string{"./a.json", "sub/b.json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := new(Validator).runValidate(tt.fsys)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			for _, want := range tt.wantErr {
				require.ErrorContains(t, err, want)
			}
		})
	}
}

func TestValidateFSMissingSchemaIsNotExist(t *testing.T) {
	t.Parallel()

	err := new(Validator).runValidate(fstest.MapFS{"README.md": readme, "manifest.yaml": manifestWith(importerA)})
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestValidateLogsEachSchema(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	v := Validator{Logger: log.New(&out, "", 0)}

	err := v.runValidate(fstest.MapFS{
		"README.md":     readme,
		"manifest.yaml": manifestWith(importerA + exporterB),
		"a.json":        {Data: []byte(goodSchema)},
		"sub/b.json":    {Data: []byte(badSchema)},
	})
	require.Error(t, err)
	require.Contains(t, out.String(), "validating README.md\n")
	require.Contains(t, out.String(), "validating schema ./a.json\n")
	require.Contains(t, out.String(), "validating schema sub/b.json\n")
}
