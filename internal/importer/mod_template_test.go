package importer

import (
	"archive/zip"
	"testing"
)

// The mod template is a shipped artifact every plugin author starts from, and
// nothing else compiles it. Verify it against the real import path so it
// cannot rot into an example the host would reject on upload.
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
	// The template documents the wizard contract by implementing it; an author
	// who deletes the key gets no config button, which is the point of the
	// structural check.
	if !hasWizard {
		t.Error("hasWizard = false, want true — the template's wizard example is gone or malformed")
	}
}
