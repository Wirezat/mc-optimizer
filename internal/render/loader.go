package render

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/assets"
)

// Loader reads models and textures out of the extracted asset tree
// (assets/<mod>/...).
type Loader struct {
	assetsDir string
}

func NewLoader(assetsDir string) *Loader {
	return &Loader{assetsDir: assetsDir}
}

// maxParentDepth caps parent chains.
const maxParentDepth = 8

// ResolveModel returns ref's fully merged model (parent chain walked, item
// definition/blockstate fallback applied), without decoding any textures.
func (l *Loader) ResolveModel(ref string) (*Model, error) {
	return l.loadModelChain(ref, 0)
}

// LoadScene assembles a drawable scene for one model reference, e.g.
// "modern_industrialization:block/pipes/item_pipe". Parents are merged the
// way the game does: a child's textures/elements win, inheriting whatever it
// doesn't declare itself.
func (l *Loader) LoadScene(ref string) (*Scene, error) {
	model, err := l.ResolveModel(ref)
	if err != nil {
		return nil, err
	}
	if len(model.Elements) == 0 {
		return nil, fmt.Errorf("render: model %q has no elements to draw", ref)
	}

	textures := map[string]*Texture{}
	for _, el := range model.Elements {
		for _, face := range el.Faces {
			path, ok := model.ResolveTexture(face.Texture)
			if !ok {
				continue
			}
			if _, done := textures[path]; done {
				continue
			}
			tex, err := l.LoadTexture(path)
			if err != nil {
				continue
			}
			textures[path] = tex
		}
	}
	return &Scene{Model: model, Textures: textures}, nil
}

func (l *Loader) loadModelChain(ref string, depth int) (*Model, error) {
	if depth > maxParentDepth {
		return nil, fmt.Errorf("render: model %q: parent chain too deep, likely a cycle", ref)
	}
	if err := SanitizeRef(ref); err != nil {
		return nil, err
	}
	modID, rel := TexturePath(ref)
	path := filepath.Join(l.assetsDir, modID, "models", filepath.FromSlash(rel)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		// Only tried at the top of the chain.
		if depth == 0 {
			blockID := rel
			if i := strings.LastIndexByte(rel, '/'); i >= 0 {
				blockID = rel[i+1:]
			}
			if resolved, ok := l.resolveItemDefinition(modID, blockID); ok {
				return l.loadModelChain(resolved, depth+1)
			}
			if resolved, ok := l.resolveBlockState(modID, blockID); ok {
				return l.loadModelChain(resolved, depth+1)
			}
		}
		return nil, fmt.Errorf("render: read model %q: %w", ref, err)
	}
	model, err := ParseModel(data)
	if err != nil {
		return nil, err
	}
	if model.Parent == "" {
		return model, nil
	}

	parent, err := l.loadModelChain(model.Parent, depth+1)
	if err != nil {
		return nil, err
	}
	return mergeModel(parent, model), nil
}

// mergeModel layers a child over its parent.
func mergeModel(parent, child *Model) *Model {
	out := &Model{
		Textures: map[string]TextureRef{},
		Elements: child.Elements,
		Tint:     child.Tint,
	}
	for k, v := range parent.Textures {
		out.Textures[k] = v
	}
	for k, v := range child.Textures {
		out.Textures[k] = v
	}
	if len(out.Elements) == 0 {
		out.Elements = parent.Elements
	}
	if out.Tint == nil {
		out.Tint = parent.Tint
	}
	return out
}

// TextureURL resolves ref to its public URL under the asset tree, or
// ok=false if no such file exists.
func (l *Loader) TextureURL(ref string) (url string, ok bool) {
	if err := SanitizeRef(ref); err != nil {
		return "", false
	}
	modID, rel := TexturePath(ref)
	rel = filepath.FromSlash(rel)
	if _, err := os.Stat(filepath.Join(l.assetsDir, modID, "textures", rel+".png")); err != nil {
		return "", false
	}
	return fmt.Sprintf("/assets/%s/textures/%s.png", modID, filepath.ToSlash(rel)), true
}

// LoadTexture decodes one texture by its namespaced reference.
func (l *Loader) LoadTexture(ref string) (*Texture, error) {
	if err := SanitizeRef(ref); err != nil {
		return nil, err
	}
	modID, rel := TexturePath(ref)
	base := filepath.Join(l.assetsDir, modID, "textures", filepath.FromSlash(rel))
	data, err := os.ReadFile(base + ".png")
	if err != nil {
		return nil, fmt.Errorf("render: read texture %q: %w", ref, err)
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("render: decode texture %q: %w", ref, err)
	}
	bounds := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, bounds.Min, draw.Src)

	// An animated sheet's first cell is the still image an icon should show.
	cells := 1
	if sheet, ok := assets.InspectAnimation(base + ".png"); ok {
		cells = sheet.Cells
	}

	return &Texture{
		Pix:   rgba.Pix,
		W:     bounds.Dx(),
		H:     bounds.Dy(),
		Cells: cells,
	}, nil
}

// SanitizeRef rejects references that would escape the asset tree.
func SanitizeRef(ref string) error {
	if ref == "" {
		return fmt.Errorf("render: empty model reference")
	}
	if strings.Contains(ref, "..") || strings.Contains(ref, `\`) {
		return fmt.Errorf("render: model reference %q is not allowed", ref)
	}
	for _, part := range strings.Split(ref, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return fmt.Errorf("render: model reference %q is not allowed", ref)
		}
	}
	return nil
}
