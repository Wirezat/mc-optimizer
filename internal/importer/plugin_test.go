package importer

import (
	"strings"
	"testing"
)

func TestParsePluginFileReadsManifest(t *testing.T) {
	def, err := ParsePluginFile([]byte("display_name: Test Mod\nversion: 1.2.3\napi_version: 1\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if def.DisplayName != "Test Mod" || def.Version != "1.2.3" || def.APIVersion != 1 {
		t.Errorf("got %+v, want Test Mod / 1.2.3 / 1", def)
	}
}

func TestParsePluginFileRequiresDisplayName(t *testing.T) {
	if _, err := ParsePluginFile([]byte("version: 1.0.0\napi_version: 1\n")); err == nil {
		t.Fatal("want error for missing display_name, got nil")
	}
}

func TestParsePluginFileRejectsUnknownAPIVersion(t *testing.T) {
	_, err := ParsePluginFile([]byte("display_name: X\nversion: 1.0.0\napi_version: 99\n"))
	if err == nil {
		t.Fatal("want error for unsupported api_version, got nil")
	}
	if !strings.Contains(err.Error(), "api_version") {
		t.Errorf("error = %q, want it to mention api_version", err)
	}
}

func TestValidatePluginJSAcceptsValidPlugin(t *testing.T) {
	if _, err := validatePluginJS("var plugin = { api_version: 1, evaluate: function () { return [] } }"); err != nil {
		t.Fatalf("valid plugin rejected: %v", err)
	}
}

// has_wizard drives whether the mod gets a config button.
func TestValidatePluginJSDetectsWizard(t *testing.T) {
	const evaluate = "evaluate: function () { return [] }"
	for name, tc := range map[string]struct {
		source string
		want   bool
	}{
		"mounts":                     {"var plugin = { " + evaluate + ", wizard: { mount: function () {} } }", true},
		"names it only in a comment": {"// wizard-authored config\nvar plugin = { " + evaluate + " }", false},
		"wizard without mount":       {"var plugin = { " + evaluate + ", wizard: {} }", false},
		"mount is not a function":    {"var plugin = { " + evaluate + ", wizard: { mount: 1 } }", false},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := validatePluginJS(tc.source)
			if err != nil {
				t.Fatalf("validatePluginJS: %v", err)
			}
			if got != tc.want {
				t.Errorf("hasWizard = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidatePluginJSRejectsMissingEvaluate(t *testing.T) {
	if _, err := validatePluginJS("var plugin = { api_version: 1 }"); err == nil {
		t.Fatal("want error for missing evaluate, got nil")
	}
}

func TestValidatePluginJSRejectsSyntaxError(t *testing.T) {
	if _, err := validatePluginJS("var plugin = {"); err == nil {
		t.Fatal("want error for syntax error, got nil")
	}
}
