package model

// PluginDef is the parsed content of a mod bundle's plugin.yml.
type PluginDef struct {
	DisplayName string
	Version     string
	APIVersion  int
}
