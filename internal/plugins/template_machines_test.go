package plugins

import (
	"archive/zip"
	"io"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// templatePluginSource reads plugin.js out of the shipped mod template.
func templatePluginSource(t *testing.T) string {
	t.Helper()
	const path = "../../web/static/data/mod-template.zip"
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "plugin/plugin.js" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open plugin.js: %v", err)
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read plugin.js: %v", err)
		}
		return string(b)
	}
	t.Fatal("mod template ships no plugin/plugin.js")
	return ""
}

// It went stale once already: it described the config as save-wide after the config had
// become per production line, and it never showed the machines binding at all.
func TestShippedTemplateTeachesTheCurrentContract(t *testing.T) {
	src := templatePluginSource(t)

	if strings.Contains(src, "save-wide") {
		t.Error("template still calls the config save-wide; it belongs to one production line")
	}

	vm := goja.New()
	if _, err := vm.RunString(src); err != nil {
		t.Fatalf("run template plugin: %v", err)
	}
	plugin, ok := vm.Get("plugin").(*goja.Object)
	if !ok {
		t.Fatal("template binds no plugin object")
	}
	machines, ok := plugin.Get("machines").(*goja.Object)
	if !ok {
		t.Fatal("template shows no machines binding; authors have no example for tier selection")
	}
	if !goja.IsUndefined(machines.Get("axes")) && machines.Get("axes") != nil {
		t.Error("template declares machines.axes, which the host no longer reads")
	}

	if _, ok := goja.AssertFunction(machines.Get("resolve")); ok {
		t.Error("template still teaches machines.resolve; the solver picks the machine")
	}

	for _, want := range []string{"fewest machines", "highest utilisation", "rank"} {
		if !strings.Contains(src, want) {
			t.Errorf("template does not mention %q", want)
		}
	}
	if !strings.Contains(src, "machine_id") {
		t.Error("template never shows ctx.recipe.machine_id, so an author cannot tell blasting from smoking")
	}
	if !strings.Contains(src, "at most one variant with no items") {
		t.Error("template still promises exactly one base variant")
	}
}
