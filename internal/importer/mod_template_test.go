package importer

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
)

// The mod template is a shipped artifact every plugin author starts from, and nothing else
// compiles it.
func TestShippedModTemplateIsAValidPlugin(t *testing.T) {
	const templatePath = "../../web/static/data/mod-template.zip"
	zr, err := zip.OpenReader(templatePath)
	if err != nil {
		t.Fatalf("open %s: %v", templatePath, err)
	}
	defer zr.Close()

	bundle, err := findPluginBundle(zr.File)
	if err != nil {
		t.Fatalf("findPluginBundle: %v", err)
	}
	if bundle == nil {
		t.Fatal("template ships no plugin bundle")
	}
	if _, err := ParsePluginFile(bundle.PluginYML); err != nil {
		t.Fatalf("plugin.yml: %v", err)
	}
	hasWizard, err := validatePluginJS(string(bundle.PluginJS))
	if err != nil {
		t.Fatalf("plugin.js: %v", err)
	}
	// The template documents the wizard contract by implementing it; an author who deletes the
	// key gets no config button, which is the point of the structural check.
	if !hasWizard {
		t.Error("hasWizard = false, want true — the template's wizard example is gone or malformed")
	}
}

func TestShippedModTemplateParses(t *testing.T) {
	zr, err := zip.OpenReader("../../web/static/data/mod-template.zip")
	if err != nil {
		t.Fatalf("open template: %v", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "mod.yml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open mod.yml: %v", err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read mod.yml: %v", err)
		}
		if _, err := ParseModFile(bytes.Replace(data, []byte("mod_id: _example_"), []byte("mod_id: tmpl"), 1)); err != nil {
			t.Fatalf("ParseModFile: %v", err)
		}
		return
	}
	t.Fatal("template ships no mod.yml")
}
