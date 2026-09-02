package importer

import (
	"archive/zip"
	"fmt"
	"io"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/plugins"
	"gopkg.in/yaml.v3"
)

// supportedAPIVersion is the only plugin API this host executes.
const supportedAPIVersion = 1

const pluginPrefix = "plugin/"

// pluginBundle holds the raw contents of an optional plugin/ subtree in a
// mod ZIP.
type pluginBundle struct {
	PluginYML []byte
	PluginJS  []byte
}

// findPluginBundle looks for the plugin/ subtree. A nil, nil result means no
// plugin is bundled, which is allowed.
func findPluginBundle(files []*zip.File) (*pluginBundle, error) {
	var b pluginBundle
	found := false
	for _, f := range files {
		if !strings.HasPrefix(f.Name, pluginPrefix) || f.FileInfo().IsDir() {
			continue
		}
		found = true
		data, err := readZipFile(f)
		if err != nil {
			return nil, fmt.Errorf("plugin: read %s: %w", f.Name, err)
		}
		switch strings.TrimPrefix(f.Name, pluginPrefix) {
		case "plugin.yml", "plugin.yaml":
			b.PluginYML = data
		case "plugin.js":
			b.PluginJS = data
		}
	}
	if !found {
		return nil, nil
	}
	if b.PluginYML == nil {
		return nil, fmt.Errorf("plugin: plugin/ present but plugin.yml is missing")
	}
	if b.PluginJS == nil {
		return nil, fmt.Errorf("plugin: plugin/ present but plugin.js is missing")
	}
	return &b, nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// rawPluginFile mirrors the plugin.yml YAML structure.
type rawPluginFile struct {
	DisplayName string `yaml:"display_name"`
	Version     string `yaml:"version"`
	APIVersion  int    `yaml:"api_version"`
}

// ParsePluginFile parses a plugin.yml and rejects unknown API versions.
func ParsePluginFile(data []byte) (*model.PluginDef, error) {
	var raw rawPluginFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("plugin.yml: yaml parse: %w", err)
	}
	if raw.DisplayName == "" {
		return nil, fmt.Errorf("plugin.yml: display_name is required")
	}
	if raw.Version == "" {
		return nil, fmt.Errorf("plugin.yml: version is required")
	}
	if raw.APIVersion != supportedAPIVersion {
		return nil, fmt.Errorf("plugin.yml: api_version %d is not supported (this host runs %d)", raw.APIVersion, supportedAPIVersion)
	}
	return &model.PluginDef{DisplayName: raw.DisplayName, Version: raw.Version, APIVersion: raw.APIVersion}, nil
}

// validatePluginJS loads the source into a throwaway VM to make sure it
// binds a plugin object with an evaluate function, and reports whether it also
// binds a wizard. plugins.Compile also runs the plugin's top-level code under
// a bootstrap deadline, so this call can legitimately take up to that timeout
// and legitimately fail — callers must not add a second timeout around it or
// swallow the error.
func validatePluginJS(source string) (hasWizard bool, err error) {
	prog, err := plugins.Compile("import-check", source)
	if err != nil {
		return false, fmt.Errorf("plugin.js: %w", err)
	}
	return prog.HasWizard(), nil
}
