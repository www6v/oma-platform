package skillzip

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"testing"
)

func TestParseSkillZipNestedFolder(t *testing.T) {
	t.Parallel()
	data := buildSkillZip(t, zipOpts{
		nested:      true,
		name:        "from-zip-nested",
		description: "Created from a zip",
	})

	parsed, err := ParseSkillZip(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != "from-zip-nested" {
		t.Fatalf("name=%q", parsed.Name)
	}
	if parsed.Description != "Created from a zip" {
		t.Fatalf("description=%q", parsed.Description)
	}
	names := make([]string, 0, len(parsed.Files))
	for _, file := range parsed.Files {
		names = append(names, file.Filename)
	}
	for _, want := range []string{"SKILL.md", "helper.py", "assets/pixel.png"} {
		if !contains(names, want) {
			t.Fatalf("missing %q in %v", want, names)
		}
	}
	if contains(names, ".DS_Store") {
		t.Fatalf("expected .DS_Store filtered out")
	}
}

func TestParseSkillZipBinaryBase64(t *testing.T) {
	t.Parallel()
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	}
	data := buildSkillZip(t, zipOpts{
		nested: true,
		name:   "binary-skill",
		png:    png,
	})
	parsed, err := ParseSkillZip(data)
	if err != nil {
		t.Fatal(err)
	}
	var pngFile *struct {
		content  string
		encoding string
	}
	for _, file := range parsed.Files {
		if file.Filename == "assets/pixel.png" {
			pngFile = &struct {
				content  string
				encoding string
			}{file.Content, file.Encoding}
			break
		}
	}
	if pngFile == nil {
		t.Fatal("missing png file")
	}
	if pngFile.encoding != "base64" {
		t.Fatalf("encoding=%q", pngFile.encoding)
	}
	raw, err := base64.StdEncoding.DecodeString(pngFile.content)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, png) {
		t.Fatalf("png bytes mismatch")
	}
}

func TestParseSkillZipMissingSkillMD(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("not a skill"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = ParseSkillZip(buf.Bytes())
	if err == nil {
		t.Fatal("expected error")
	}
}

type zipOpts struct {
	nested      bool
	name        string
	description string
	png         []byte
}

func buildSkillZip(t *testing.T, opts zipOpts) []byte {
	t.Helper()
	if opts.name == "" {
		opts.name = "zipped-skill"
	}
	if opts.description == "" {
		opts.description = "Created from a zip"
	}
	if opts.png == nil {
		opts.png = []byte{0x89, 0x50, 0x4e, 0x47}
	}
	root := ""
	if opts.nested {
		root = opts.name + "/"
	}
	md := "---\nname: " + opts.name + "\ndescription: " +
		opts.description + "\n---\n# " + opts.name + "\n"

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := map[string][]byte{
		root + "SKILL.md":         []byte(md),
		root + "helper.py":        []byte("def hello():\n    return 'hi'\n"),
		root + "assets/pixel.png": opts.png,
		root + ".DS_Store":        []byte("junk"),
	}
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
