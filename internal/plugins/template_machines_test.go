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

// The template is what every plugin author copies, so it has to demonstrate the
// current contract rather than a past one. It went stale once already: it
// described the config as save-wide after the config had become per production
// line, and it never showed the machines binding at all.
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

	// resolve must read the tier out of the config it is handed, not out of a
	// host-supplied selection.
	fn, ok := goja.AssertFunction(machines.Get("resolve"))
	if !ok {
		t.Fatal("machines.resolve is not a function")
	}
	cands := vm.ToValue([]any{
		map[string]any{"mod_id": "example", "machine_id": "basic_press"},
		map[string]any{"mod_id": "example", "machine_id": "advanced_press"},
	})
	for name, tc := range map[string]struct {
		config map[string]any
		want   int64
	}{
		"advanced from the config": {map[string]any{"tier": "advanced"}, 1},
		"basic from the config":    {map[string]any{"tier": "basic"}, 0},
		"empty config falls back":  {map[string]any{}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			res, err := fn(goja.Undefined(), cands, vm.ToValue(tc.config))
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got := res.ToInteger(); got != tc.want {
				t.Errorf("resolve = %d, want %d", got, tc.want)
			}
		})
	}
}
