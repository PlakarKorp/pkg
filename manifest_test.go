package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PlakarKorp/kloset/location"
)

const sampleManifest = `
name: s3
display_name: Amazon S3
description: S3 object storage connector
tier: community
contact: support@plakar.io
homepage: https://plakar.io
license: ISC
api_version: v1.1.0
tags:
  - cloud
  - aws
connectors:
  - type: storage
    class: object-storage
    subclass: s3
    protocols:
      - s3
    location_flags:
      - localfs
      - file
    executable: s3-storage
    args:
      - --verbose
    extra_files:
      - icon.png
`

func TestManifestParse(t *testing.T) {
	var m Manifest
	if err := m.Parse(strings.NewReader(sampleManifest)); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if m.Name != "s3" {
		t.Errorf("Name = %q, want s3", m.Name)
	}
	if m.DisplayName != "Amazon S3" {
		t.Errorf("DisplayName = %q", m.DisplayName)
	}
	if m.APIVersion != "v1.1.0" {
		t.Errorf("APIVersion = %q, want v1.1.0", m.APIVersion)
	}
	if len(m.Tags) != 2 || m.Tags[0] != "cloud" || m.Tags[1] != "aws" {
		t.Errorf("Tags = %v", m.Tags)
	}
	if len(m.Connectors) != 1 {
		t.Fatalf("len(Connectors) = %d, want 1", len(m.Connectors))
	}

	c := m.Connectors[0]
	if c.Type != ConnectorTypeStorage {
		t.Errorf("connector Type = %q, want storage", c.Type)
	}
	if c.Class != ResourceClassObjectStorage {
		t.Errorf("connector Class = %q, want object-storage", c.Class)
	}
	if c.SubClass != ResourceSubClassS3 {
		t.Errorf("connector SubClass = %q, want s3", c.SubClass)
	}
	if strings.TrimSuffix(c.Executable, ".exe") != "s3-storage" {
		t.Errorf("Executable = %q", c.Executable)
	}
	if len(c.Args) != 1 || c.Args[0] != "--verbose" {
		t.Errorf("Args = %v", c.Args)
	}
}

func TestManifestParseInvalidYAML(t *testing.T) {
	var m Manifest
	err := m.Parse(strings.NewReader("connectors: [oops"))
	if err == nil {
		t.Fatal("expected error for invalid yaml")
	}
	if !strings.Contains(err.Error(), "failed to decode the manifest") {
		t.Errorf("error = %v, want wrapped decode error", err)
	}
}

func TestNewManifestFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(path, []byte(sampleManifest), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManifestFromFile(path)
	if err != nil {
		t.Fatalf("NewManifestFromFile: %v", err)
	}
	if m.Name != "s3" {
		t.Errorf("Name = %q, want s3", m.Name)
	}
}

func TestNewManifestFromFileMissing(t *testing.T) {
	if _, err := NewManifestFromFile(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestManifestNoConnectors(t *testing.T) {
	rd := strings.NewReader(`name: s3`)
	var m Manifest

	if err := m.Parse(rd); err == nil {
		t.Errorf("expected to fail when parsing a manifest without a connector")
	}
}

func TestManifestFields(t *testing.T) {
	const connectors = "connectors: [{type: importer, executable: tool, protocols: [foo]}]\n"
	tests := []struct {
		name    string
		fields  string
		wantErr string
	}{
		{"all set", "name: s3\ndisplay_name: S3\ndescription: x\n", ""},
		{"only name", "name: s3\n", ""},
		{"missing name", "display_name: S3\ndescription: x\n", "name is required"},
		{"blank name", "name: ' '\n", "name is required"},
		{"name with hyphen", "name: aws-inventory\n", ""},
		{"name starting with hyphen", "name: -rf\n", `name: invalid package name "-rf": starts with '-'`},
		{"name with underscore", "name: my_plugin\n", `name: invalid package name "my_plugin"`},
		{"name with space", "name: my plugin\n", `name: invalid package name "my plugin"`},
		{"name with slash", "name: ../s3\n", `name: invalid package name "../s3"`},
		{"name with non-ascii", "name: café\n", `name: invalid package name "café"`},
		{"blank display_name", "name: s3\ndisplay_name: \"\\t\"\n", "display_name is blank"},
		{"blank description", "name: s3\ndescription: \"\\n \"\n", "description is blank"},
		{"https homepage", "name: s3\nhomepage: https://github.com/PlakarKorp/integrations/tree/main/s3\n", ""},
		{"http homepage", "name: s3\nhomepage: http://plakar.io\n", `homepage "http://plakar.io" is not an https URL`},
		{"homepage without scheme", "name: s3\nhomepage: plakar.io\n", `homepage "plakar.io" is not an https URL`},
		{"homepage without host", "name: s3\nhomepage: 'https:///path'\n", `homepage "https:///path" is not an https URL`},
		{"mailto contact", "name: s3\ncontact: mailto:help@plakar.io\n", ""},
		{"https contact", "name: s3\ncontact: https://plakar.io/support\n", ""},
		{"email contact", "name: s3\ncontact: help@plakar.io\n", ""},
		{"email contact with name", "name: s3\ncontact: Help <help@plakar.io>\n", ""},
		{"blank contact", "name: s3\ncontact: ' '\n", `contact " " is neither an email address nor a URL`},
		{"contact with neither", "name: s3\ncontact: help at plakar.io\n", `contact "help at plakar.io" is neither an email address nor a URL`},
		{"license", "name: s3\nlicense: ISC\n", ""},
		{"license expression", "name: s3\nlicense: MIT OR Apache-2.0\n", ""},
		{"blank license", "name: s3\nlicense: ' '\n", `license " " must be a single line without surrounding whitespace`},
		{"license with surrounding space", "name: s3\nlicense: ' ISC'\n", `license " ISC" must be a single line`},
		{"multi-line license", "name: s3\nlicense: \"ISC\\nMIT\"\n", `license "ISC\nMIT" must be a single line`},
		{"tags", "name: s3\ntags: [cloud, object-storage]\n", ""},
		{"api_version", "name: s3\napi_version: v1.1.0\n", ""},
		{"api_version without v", "name: s3\napi_version: 1.1.0\n", `api_version "1.1.0" is not a valid version`},
		{"shorthand api_version", "name: s3\napi_version: v1\n", `api_version "v1" is not a valid version`},
		{"blank api_version", "name: s3\napi_version: ' '\n", `api_version " " is not a valid version`},
		{"empty tag", "name: s3\ntags: ['']\n", `tag "" must be a non-empty word`},
		{"tag with space", "name: s3\ntags: [object storage]\n", `tag "object storage" must be a non-empty word`},
		{"duplicate tag", "name: s3\ntags: [aws, cloud, aws]\n", `tag "aws" listed twice`},
		{"contact without scheme", "name: s3\ncontact: plakar.io/support\n", `contact "plakar.io/support" is neither an email address nor a URL`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m Manifest
			err := m.Parse(strings.NewReader(tt.fields + connectors))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Parse: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestManifestValidation(t *testing.T) {
	// try first with a good connector type and different
	// combination of classes and subclasses
	suite := []struct {
		class    ResourceClass
		subclass ResourceSubClass
		ok       bool
	}{
		{ResourceClassUndefined, ResourceSubClassUndefined, true},
		{ResourceClassObjectStorage, ResourceSubClassS3, true},
		{ResourceClassObjectStorage, ResourceSubClassUndefined, true},
		{ResourceClassObjectStorage, ResourceSubClassPostgreSQL, false},
		{ResourceClassObjectStorage, ResourceSubClass("foobar"), false},
	}

	for _, test := range suite {
		rd := strings.NewReader(fmt.Sprintf(`name: foo
connectors:
  - type: importer
    executable: true
    protocols: [foo]
    class: %s
    subclass: %s`, test.class, test.subclass))

		var m Manifest
		err := m.Parse(rd)
		if test.ok && err != nil {
			t.Errorf("%s/%s: unexpected failure: %v",
				test.class, test.subclass, err)
		} else if !test.ok && err == nil {
			t.Errorf("%s/%s: expected failure", test.class, test.subclass)
		}
	}

	// now with a bad connector type
	var m Manifest
	err := m.Parse(strings.NewReader(`name: foo
connectors:
  - type: myamazingconnector`))
	if err == nil {
		t.Errorf("expected failure with bad connector name")
	}
}

func TestManifestWindowsExeSuffix(t *testing.T) {
	// The GOOS env var forces the windows code path regardless of the
	// host OS, so this test is portable.
	t.Setenv("GOOS", "windows")

	var m Manifest
	if err := m.Parse(strings.NewReader(sampleManifest)); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := m.Connectors[0].Executable; got != "s3-storage.exe" {
		t.Errorf("Executable = %q, want s3-storage.exe", got)
	}

	// Idempotent: an executable that already ends in .exe is untouched.
	const withExe = `
name: tool
connectors:
  - type: storage
    executable: tool.exe
    protocols: [tool]
`
	var m2 Manifest
	if err := m2.Parse(strings.NewReader(withExe)); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := m2.Connectors[0].Executable; got != "tool.exe" {
		t.Errorf("Executable = %q, want tool.exe (no double suffix)", got)
	}
}

const dockerManifest = `
name: postgres
api_version: v1.1.0
connectors:
  - type: importer
    protocols:
      - postgres
    image_id: sha256:1111111111111111111111111111111111111111111111111111111111111111
    image: registry.plakar.io/plugin-postgres:1.0.0
`

func TestManifestParseImage(t *testing.T) {
	var m Manifest
	if err := m.Parse(strings.NewReader(dockerManifest)); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	c := m.Connectors[0]
	if !strings.HasPrefix(c.ImageID, "sha256:") {
		t.Errorf("ImageID = %q", c.ImageID)
	}
	if c.Image != "registry.plakar.io/plugin-postgres:1.0.0" {
		t.Errorf("Image = %q", c.Image)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestManifestWindowsExeSuffixSkipsImageConnectors(t *testing.T) {
	t.Setenv("GOOS", "windows")

	var m Manifest
	if err := m.Parse(strings.NewReader(dockerManifest)); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := m.Connectors[0].Executable; got != "" {
		t.Errorf("Executable = %q, want empty (no .exe suffix on image connectors)", got)
	}
}

const testImageID = "sha256:2222222222222222222222222222222222222222222222222222222222222222"

func TestManifestConnectorValidate(t *testing.T) {
	valid := func(c ManifestConnector) error { return c.Validate() }

	if err := valid(ManifestConnector{Type: "importer", Protocols: []string{"foo"}, Executable: "tool"}); err != nil {
		t.Errorf("native connector: %v", err)
	}
	if err := valid(ManifestConnector{Type: "importer", Protocols: []string{"foo"}, ImageID: testImageID}); err != nil {
		t.Errorf("image connector: %v", err)
	}
	if err := valid(ManifestConnector{Type: "importer", Protocols: []string{"foo"}, ImageID: testImageID, Image: "reg/img:1"}); err != nil {
		t.Errorf("image connector with registry ref: %v", err)
	}
	if err := valid(ManifestConnector{Type: "importer", Protocols: []string{"foo"}, ImageID: testImageID, ExtraFiles: []string{"icon.png"}}); err != nil {
		t.Errorf("extra_files on an image connector: %v", err)
	}
	if err := valid(ManifestConnector{Type: "importer", Protocols: []string{"foo"}, Executable: "tool", LocationFlags: []string{"localfs"}}); err != nil {
		t.Errorf("localfs native connector: %v", err)
	}

	if err := valid(ManifestConnector{Type: "importer"}); err == nil {
		t.Error("connector with neither executable nor image_id should fail")
	}
	if err := valid(ManifestConnector{Type: "importer", Executable: "tool", ImageID: testImageID}); err == nil {
		t.Error("connector with both executable and image_id should fail")
	}
	if err := valid(ManifestConnector{Type: "importer", Executable: "tool", Image: "reg/img:1"}); err == nil {
		t.Error("image without image_id should fail")
	}
	if err := valid(ManifestConnector{Type: "importer", ImageID: testImageID, LocationFlags: []string{"localfs"}}); err == nil {
		t.Error("localfs image connector should fail")
	}
	if err := valid(ManifestConnector{Type: "importer", Executable: "tool", LocationFlags: []string{"bogus"}}); err == nil {
		t.Error("unknown location flag should fail")
	}

	if err := valid(ManifestConnector{Executable: "tool"}); err == nil {
		t.Error("a manifest without a type is invalid")
	}
}

func TestManifestConnectorProtocols(t *testing.T) {
	tests := []struct {
		name      string
		protocols []string
		wantErr   string
	}{
		{"single", []string{"s3"}, ""},
		{"several", []string{"dav", "davs"}, ""},
		{"scheme punctuation", []string{"k8s+csi", "mysql.proxy"}, ""},
		{"none", nil, "protocols is required"},
		{"empty", []string{""}, `protocol "" is not a valid scheme`},
		{"one char", []string{"x"}, `protocol "x" is not a valid scheme`},
		{"url", []string{"s3://"}, `protocol "s3://" is not a valid scheme`},
		{"space", []string{"s 3"}, `protocol "s 3" is not a valid scheme`},
		{"leading hyphen", []string{"-s3"}, `protocol "-s3" is not a valid scheme`},
		{"inner hyphen", []string{"aws-sm"}, ""},
		{"duplicate", []string{"s3", "gcs", "s3"}, `protocol "s3" listed twice`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ManifestConnector{Type: "importer", Executable: "tool", Protocols: tt.protocols}
			err := c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestManifestConnectorExecutable(t *testing.T) {
	tests := []struct {
		name       string
		executable string
		wantErr    string
	}{
		{"plain", "s3-importer", ""},
		{"dot slash", "./s3-importer", ""},
		{"subdirectory", "bin/s3-importer", ""},
		{"blank", "  ", "executable is blank"},
		{"parent", "../s3-importer", `executable "../s3-importer" escapes the integration directory`},
		{"inner parent", "bin/../../s3-importer", `executable "bin/../../s3-importer" escapes the integration directory`},
		{"sibling directory", "../s3_v1.0.0_linux_amd64-evil/x", "escapes the integration directory"},
		{"absolute", "/bin/sh", `executable "/bin/sh" escapes the integration directory`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ManifestConnector{Type: "importer", Executable: tt.executable, Protocols: []string{"s3"}}
			err := c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestManifestConnectorExtraFiles(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		wantErr string
	}{
		{"none", nil, ""},
		{"several", []string{"icon.png", "./lib/helper.so"}, ""},
		{"blank", []string{" "}, "extra_files entry is blank"},
		{"parent", []string{"../secret"}, `extra_files "../secret" escapes the integration directory`},
		{"inner parent", []string{"lib/../../secret"}, `extra_files "lib/../../secret" escapes the integration directory`},
		{"absolute", []string{"/etc/passwd"}, `extra_files "/etc/passwd" escapes the integration directory`},
		{"duplicate", []string{"icon.png", "lib.so", "icon.png"}, `extra_files "icon.png" listed twice`},
		{"duplicate after cleaning", []string{"icon.png", "./icon.png"}, `extra_files "./icon.png" listed twice`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ManifestConnector{Type: "importer", Executable: "tool", Protocols: []string{"s3"}, ExtraFiles: tt.files}
			err := c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestManifestConnectorImageID(t *testing.T) {
	tests := []struct {
		name    string
		imageID string
		wantErr string
	}{
		{"valid", testImageID, ""},
		{"short", "sha256:aa", `image_id "sha256:aa" is not sha256:<64 lowercase hex digits>`},
		{"no algorithm", strings.TrimPrefix(testImageID, "sha256:"), "is not sha256:<64 lowercase hex digits>"},
		{"other algorithm", "sha512:" + strings.Repeat("2", 64), "is not sha256:<64 lowercase hex digits>"},
		{"uppercase", "sha256:" + strings.Repeat("A", 64), "is not sha256:<64 lowercase hex digits>"},
		{"too long", testImageID + "2", "is not sha256:<64 lowercase hex digits>"},
		{"blank", " ", "is not sha256:<64 lowercase hex digits>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ManifestConnector{Type: "importer", ImageID: tt.imageID, Protocols: []string{"s3"}}
			err := c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestManifestConnectorImage(t *testing.T) {
	tests := []struct {
		name    string
		image   string
		wantErr string
	}{
		{"unset", "", ""},
		{"tag", "ghcr.io/plakarkorp/redis-importer:v1.0.0", ""},
		{"digest", "registry.plakar.io/plugin-postgres@" + testImageID, ""},
		{"registry port", "localhost:5000/importer", ""},
		{"blank", " ", `image " " is not a registry reference`},
		{"space", "ghcr.io/x :v1", `image "ghcr.io/x :v1" is not a registry reference`},
		{"url", "https://ghcr.io/x:v1", `image "https://ghcr.io/x:v1" is not a registry reference`},
		{"leading hyphen", "--privileged", `image "--privileged" is not a registry reference`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ManifestConnector{Type: "importer", ImageID: testImageID, Image: tt.image, Protocols: []string{"s3"}}
			err := c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestManifestProtocolOwners(t *testing.T) {
	tests := []struct {
		name       string
		connectors string
		wantErr    string
	}{
		{"same protocol, different types",
			`[{type: importer, executable: a, protocols: [s3]},
			  {type: exporter, executable: b, protocols: [s3]},
			  {type: storage, executable: c, protocols: [s3]}]`, ""},
		{"same type, different protocols",
			`[{type: importer, executable: a, protocols: [k8s]},
			  {type: importer, executable: a, protocols: [k8s+pvc]}]`, ""},
		{"same type and protocol",
			`[{type: importer, executable: a, protocols: [s3]},
			  {type: importer, executable: b, protocols: [gcs, s3]}]`,
			`connector #1: importer protocol "s3" already declared by connector #0`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m Manifest
			err := m.Parse(strings.NewReader("name: s3\nconnectors: " + tt.connectors + "\n"))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Parse: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestManifestConnectorFlags(t *testing.T) {
	c := ManifestConnector{LocationFlags: []string{"localfs", "file", "stream", "needack", "nomerge"}}
	flags, err := c.Flags()
	if err != nil {
		t.Fatalf("Flags: %v", err)
	}

	want := location.FLAG_LOCALFS | location.FLAG_FILE | location.FLAG_STREAM |
		location.FLAG_NEEDACK | location.FLAG_NOMERGE
	if flags != want {
		t.Errorf("Flags() = %d, want %d", flags, want)
	}
}

func TestManifestConnectorFlagsEmpty(t *testing.T) {
	var c ManifestConnector
	flags, err := c.Flags()
	if err != nil {
		t.Fatalf("Flags: %v", err)
	}
	if flags != 0 {
		t.Errorf("Flags() = %d, want 0", flags)
	}
}

func TestManifestConnectorFlagsUnknown(t *testing.T) {
	c := ManifestConnector{LocationFlags: []string{"localfs", "bogus"}}
	_, err := c.Flags()
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error = %v, want it to mention the bad flag", err)
	}
}
