package render

import (
	"image"
	"math"
)

// Vanilla's display.gui transform for block models — rotation [30, 225, 0] and
// scale 0.625 — is what gives every block in the inventory its three-quarter
// view. With this rotation the camera sees the top, north and east faces, which
// is why a furnace shows its front in the hotbar.
const (
	guiRotX  = 30.0
	guiRotY  = 225.0
	guiRotZ  = 0.0
	guiScale = 0.625

	// Rendering at a multiple of the target and box-filtering down keeps the
	// silhouette clean. Texels are sampled nearest-neighbour, but the box filter
	// still averages across them, so this softens the art as well as the edges —
	// the tradeoff is deliberate at icon sizes.
	supersample = 4
)

// Texture is a decoded texture, indexed row-major with v running downward as in
// the file. An animated texture is a sprite sheet; Cells says how many frames
// are stacked in it, so only the first is drawn.
type Texture struct {
	Pix   []uint8 // RGBA, 4 bytes per pixel
	W, H  int
	Cells int
}

// At samples a texel, clamping to the texture. u and v are in 0..1 of the
// visible cell, not of the whole sheet.
func (t *Texture) At(u, v float64) (r, g, b, a uint8) {
	cellH := t.H
	if t.Cells > 1 {
		cellH = t.H / t.Cells
	}
	if cellH < 1 {
		cellH = 1 // a sheet claiming more cells than it has rows must not index backwards
	}
	x := int(u * float64(t.W))
	y := int(v * float64(cellH))
	x = clampInt(x, 0, t.W-1)
	y = clampInt(y, 0, cellH-1)
	i := (y*t.W + x) * 4
	return t.Pix[i], t.Pix[i+1], t.Pix[i+2], t.Pix[i+3]
}

// Scene is a model with its textures already decoded, ready to draw.
type Scene struct {
	Model    *Model
	Textures map[string]*Texture // keyed by resolved texture reference
}

type vec3 [3]float64

type mat3 [3][3]float64

func (m mat3) mul(v vec3) vec3 {
	return vec3{
		m[0][0]*v[0] + m[0][1]*v[1] + m[0][2]*v[2],
		m[1][0]*v[0] + m[1][1]*v[1] + m[1][2]*v[2],
		m[2][0]*v[0] + m[2][1]*v[1] + m[2][2]*v[2],
	}
}

func (a mat3) mulM(b mat3) mat3 {
	var out mat3
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			out[i][j] = a[i][0]*b[0][j] + a[i][1]*b[1][j] + a[i][2]*b[2][j]
		}
	}
	return out
}

func rotX(deg float64) mat3 {
	c, s := math.Cos(rad(deg)), math.Sin(rad(deg))
	return mat3{{1, 0, 0}, {0, c, -s}, {0, s, c}}
}

func rotY(deg float64) mat3 {
	c, s := math.Cos(rad(deg)), math.Sin(rad(deg))
	return mat3{{c, 0, s}, {0, 1, 0}, {-s, 0, c}}
}

func rotZ(deg float64) mat3 {
	c, s := math.Cos(rad(deg)), math.Sin(rad(deg))
	return mat3{{c, -s, 0}, {s, c, 0}, {0, 0, 1}}
}

func rad(deg float64) float64 { return deg * math.Pi / 180 }

func identity() mat3 { return mat3{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}} }

// guiMatrix is Rx*Ry*Rz, matching the order Minecraft builds its display
// transform's quaternion from euler angles.
func guiMatrix() mat3 {
	return rotX(guiRotX).mulM(rotY(guiRotY)).mulM(rotZ(guiRotZ))
}

// Corner order per face: counter-clockwise seen from outside, starting at the
// face's uv origin. Each entry picks the low (0) or high (1) end of the box on
// each axis.
var faceCorners = map[string][4][3]int{
	"up":    {{0, 1, 0}, {0, 1, 1}, {1, 1, 1}, {1, 1, 0}},
	"down":  {{0, 0, 1}, {0, 0, 0}, {1, 0, 0}, {1, 0, 1}},
	"north": {{1, 1, 0}, {1, 0, 0}, {0, 0, 0}, {0, 1, 0}},
	"south": {{0, 1, 1}, {0, 0, 1}, {1, 0, 1}, {1, 1, 1}},
	"west":  {{0, 1, 0}, {0, 0, 0}, {0, 0, 1}, {0, 1, 1}},
	"east":  {{1, 1, 1}, {1, 0, 1}, {1, 0, 0}, {1, 1, 0}},
}

type vertex struct {
	x, y, depth float64
	u, v        float64
}

// Render draws the scene at size x size pixels.
func Render(scene *Scene, size int) *image.RGBA {
	n := size * supersample
	// acc holds plain, non-premultiplied colour plus the texel's own alpha, one
	// entry per supersampled pixel, overwritten whenever a nearer fragment wins
	// the z-test. Premultiplication happens once, in downsample.
	var (
		acc    = make([]float64, n*n*4)
		zbuf   = make([]float64, n*n)
		matrix = guiMatrix()
	)
	for i := range zbuf {
		zbuf[i] = math.Inf(-1)
	}

	for _, el := range scene.Model.Elements {
		// An element rotation happens in model space about the element's own
		// origin, before the display transform ever runs.
		local := identity()
		var origin vec3
		if el.Rotation != nil {
			local = elementMatrix(*el.Rotation)
			origin = vec3{el.Rotation.Origin[0], el.Rotation.Origin[1], el.Rotation.Origin[2]}
		}

		for _, dir := range directions {
			face, present := el.Faces[dir]
			if !present {
				continue
			}
			tex := scene.textureFor(face)
			if tex == nil {
				continue
			}

			normal := matrix.mul(local.mul(vec3(normals[dir])))
			if normal[2] <= 1e-9 {
				continue // facing away from the camera
			}

			shade := 1.0
			if el.shaded() {
				shade = faceShade[dir]
			}
			// tintindex -1 is vanilla's explicit "no tint"; any other index
			// selects a colour, and this renderer only knows one.
			tint := uint32(0xFFFFFF)
			if scene.Model.Tint != nil && face.TintIndex != nil && *face.TintIndex >= 0 {
				tint = *scene.Model.Tint
			}

			quad := faceQuad(el, dir, face, matrix, local, origin, n)
			rasterQuad(acc, zbuf, n, quad, tex, shade, tint)
		}
	}

	return downsample(acc, size)
}

// elementMatrix builds an element's own rotation. An unrecognised axis yields no
// rotation, which is what vanilla does with a malformed model rather than
// refusing to load it.
//
// Not implemented: `rescale`, which vanilla uses to grow a rotated element back
// out to its bounding box. No model here sets it.
func elementMatrix(r ElementRot) mat3 {
	switch r.Axis {
	case "x":
		return rotX(r.Angle)
	case "y":
		return rotY(r.Angle)
	case "z":
		return rotZ(r.Angle)
	}
	return identity()
}

// faceQuad projects one face to buffer coordinates with its uv per corner.
//
// local and origin carry the element's own rotation: a point is rotated about
// origin in model space first, and only the result goes through the display
// transform.
func faceQuad(el Element, dir string, face Face, matrix, local mat3, origin vec3, n int) [4]vertex {
	uv := defaultUV(el, dir)
	if face.UV != nil {
		uv = *face.UV
	}
	// uv runs a -> b -> c -> d around the face; rotation shifts which corner
	// the texture's origin lands on, in 90 degree steps as vanilla defines it.
	corners := [4][2]float64{
		{uv[0], uv[1]}, {uv[0], uv[3]}, {uv[2], uv[3]}, {uv[2], uv[1]},
	}
	steps := ((face.Rotation/90)%4 + 4) % 4

	var out [4]vertex
	for i, pick := range faceCorners[dir] {
		p := vec3{
			pickAxis(el, 0, pick[0]),
			pickAxis(el, 1, pick[1]),
			pickAxis(el, 2, pick[2]),
		}
		// Element rotation, in model space, about the element's own origin.
		rotated := local.mul(vec3{p[0] - origin[0], p[1] - origin[1], p[2] - origin[2]})
		p = vec3{rotated[0] + origin[0], rotated[1] + origin[1], rotated[2] + origin[2]}

		v := matrix.mul(vec3{
			(p[0]/16 - 0.5) * guiScale,
			(p[1]/16 - 0.5) * guiScale,
			(p[2]/16 - 0.5) * guiScale,
		})

		c := corners[(i+steps)%4]
		out[i] = vertex{
			x:     (v[0] + 0.5) * float64(n),
			y:     (0.5 - v[1]) * float64(n),
			depth: v[2],
			u:     c[0] / 16,
			v:     c[1] / 16,
		}
	}
	return out
}

// defaultUV is the uv Minecraft infers when a face declares none: the element's
// own extent on the two axes of that face, so a half-width element shows half
// the texture rather than the whole thing squeezed onto it.
func defaultUV(el Element, dir string) [4]float64 {
	x0, x1 := el.From[0], el.To[0]
	y0, y1 := el.From[1], el.To[1]
	z0, z1 := el.From[2], el.To[2]

	switch dir {
	case "down":
		return [4]float64{x0, 16 - z1, x1, 16 - z0}
	case "up":
		return [4]float64{x0, z0, x1, z1}
	case "north":
		return [4]float64{16 - x1, 16 - y1, 16 - x0, 16 - y0}
	case "south":
		return [4]float64{x0, 16 - y1, x1, 16 - y0}
	case "west":
		return [4]float64{z0, 16 - y1, z1, 16 - y0}
	case "east":
		return [4]float64{16 - z1, 16 - y1, 16 - z0, 16 - y0}
	}
	return [4]float64{0, 0, 16, 16}
}

func pickAxis(el Element, axis, high int) float64 {
	if high == 1 {
		return el.To[axis]
	}
	return el.From[axis]
}

// textureFor resolves a face's texture through the model's map.
func (s *Scene) textureFor(face Face) *Texture {
	ref, ok := s.Model.ResolveTexture(face.Texture)
	if !ok {
		return nil
	}
	return s.Textures[ref]
}

// rasterQuad fills a quad as two triangles with a z-buffer. Orthographic
// projection of a planar triangle is affine, so interpolating uv and depth
// linearly is exact — no perspective correction needed.
func rasterQuad(acc, zbuf []float64, n int, q [4]vertex, tex *Texture, shade float64, tint uint32) {
	tr, tg, tb := float64((tint>>16)&255)/255, float64((tint>>8)&255)/255, float64(tint&255)/255

	for _, tri := range [2][3]int{{0, 1, 2}, {0, 2, 3}} {
		a, b, c := q[tri[0]], q[tri[1]], q[tri[2]]

		minX := clampInt(int(math.Floor(min3(a.x, b.x, c.x))), 0, n)
		maxX := clampInt(int(math.Ceil(max3(a.x, b.x, c.x)))+1, 0, n)
		minY := clampInt(int(math.Floor(min3(a.y, b.y, c.y))), 0, n)
		maxY := clampInt(int(math.Ceil(max3(a.y, b.y, c.y)))+1, 0, n)
		if maxX <= minX || maxY <= minY {
			continue
		}

		denom := (b.y-c.y)*(a.x-c.x) + (c.x-b.x)*(a.y-c.y)
		if math.Abs(denom) < 1e-9 {
			continue
		}

		for py := minY; py < maxY; py++ {
			for px := minX; px < maxX; px++ {
				fx, fy := float64(px)+0.5, float64(py)+0.5
				w0 := ((b.y-c.y)*(fx-c.x) + (c.x-b.x)*(fy-c.y)) / denom
				w1 := ((c.y-a.y)*(fx-c.x) + (a.x-c.x)*(fy-c.y)) / denom
				w2 := 1 - w0 - w1
				if w0 < -1e-6 || w1 < -1e-6 || w2 < -1e-6 {
					continue
				}

				// Camera looks along -Z, so a larger depth is nearer. Ties go to
				// whatever is drawn later: the model format is painter-ordered,
				// so an overlay element declared after a base one at the same
				// coordinates is meant to cover it.
				depth := w0*a.depth + w1*b.depth + w2*c.depth
				idx := py*n + px
				if depth < zbuf[idx] {
					continue
				}

				u := w0*a.u + w1*b.u + w2*c.u
				v := w0*a.v + w1*b.v + w2*c.v
				r, g, bb, al := tex.At(u, v)
				// Cutout, not blending: a texel is either drawn or it is not.
				// A fully transparent one must not claim the pixel, or a hole
				// would occlude whatever lies behind it. Partial alpha is kept
				// for the edge filter but does not blend with what is underneath,
				// so a genuinely translucent texture (glass) will read as solid.
				if al == 0 {
					continue
				}

				zbuf[idx] = depth
				o := idx * 4
				acc[o+0] = float64(r) * shade * tr
				acc[o+1] = float64(g) * shade * tg
				acc[o+2] = float64(bb) * shade * tb
				acc[o+3] = float64(al)
			}
		}
	}
}

// downsample box-filters the supersampled buffer, weighting colour by alpha so
// edge pixels take the colour of the geometry rather than of the void.
func downsample(acc []float64, size int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	n := size * supersample

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var sr, sg, sb, sa float64
			for dy := 0; dy < supersample; dy++ {
				for dx := 0; dx < supersample; dx++ {
					o := ((y*supersample+dy)*n + (x*supersample + dx)) * 4
					a := acc[o+3] / 255
					sr += acc[o+0] * a
					sg += acc[o+1] * a
					sb += acc[o+2] * a
					sa += a
				}
			}
			i := out.PixOffset(x, y)
			// image.RGBA is alpha-premultiplied — png.Encode divides the colour
			// back out on the way to the file. Storing the plain average here
			// would have every partially covered edge pixel divided by its own
			// coverage, which blows the colour out and shifts its hue.
			coverage := sa / float64(supersample*supersample)
			if sa > 0 {
				out.Pix[i+0] = clampByte(sr / sa * coverage)
				out.Pix[i+1] = clampByte(sg / sa * coverage)
				out.Pix[i+2] = clampByte(sb / sa * coverage)
			}
			out.Pix[i+3] = clampByte(coverage * 255)
		}
	}
	return out
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func min3(a, b, c float64) float64 { return math.Min(a, math.Min(b, c)) }
func max3(a, b, c float64) float64 { return math.Max(a, math.Max(b, c)) }
