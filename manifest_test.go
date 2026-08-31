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
	if c.Executable != "s3-storage" {
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
connectors:
  - type: storage
    executable: tool.exe
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

func TestManifestConnectorValidate(t *testing.T) {
	valid := func(c ManifestConnector) error { return c.Validate() }

	if err := valid(ManifestConnector{Type: "importer", Executable: "tool"}); err != nil {
		t.Errorf("native connector: %v", err)
	}
	if err := valid(ManifestConnector{Type: "importer", ImageID: "sha256:aa"}); err != nil {
		t.Errorf("image connector: %v", err)
	}
	if err := valid(ManifestConnector{Type: "importer", ImageID: "sha256:aa", Image: "reg/img:1"}); err != nil {
		t.Errorf("image connector with registry ref: %v", err)
	}
	if err := valid(ManifestConnector{Type: "importer", ImageID: "sha256:aa", ExtraFiles: []string{"icon.png"}}); err != nil {
		t.Errorf("extra_files on an image connector: %v", err)
	}
	if err := valid(ManifestConnector{Type: "importer", Executable: "tool", LocationFlags: []string{"localfs"}}); err != nil {
		t.Errorf("localfs native connector: %v", err)
	}

	if err := valid(ManifestConnector{Type: "importer"}); err == nil {
		t.Error("connector with neither executable nor image_id should fail")
	}
	if err := valid(ManifestConnector{Type: "importer", Executable: "tool", ImageID: "sha256:aa"}); err == nil {
		t.Error("connector with both executable and image_id should fail")
	}
	if err := valid(ManifestConnector{Type: "importer", Executable: "tool", Image: "reg/img:1"}); err == nil {
		t.Error("image without image_id should fail")
	}
	if err := valid(ManifestConnector{Type: "importer", ImageID: "sha256:aa", LocationFlags: []string{"localfs"}}); err == nil {
		t.Error("localfs image connector should fail")
	}
	if err := valid(ManifestConnector{Type: "importer", Executable: "tool", LocationFlags: []string{"bogus"}}); err == nil {
		t.Error("unknown location flag should fail")
	}

	if err := valid(ManifestConnector{Executable: "tool"}); err == nil {
		t.Error("a manifest without a type is invalid")
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
