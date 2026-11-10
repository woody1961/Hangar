// Draws Hangar's application icon: a deep-space blue tile with a white hangar arch and a ship launching from it.
//
//	go run ./scripts/icon out.png [out-windows.syso]
//
// The PNG is 1024×1024 on Apple's icon grid (an 824 px rounded tile with room for its shadow). With a second
// argument it also writes the Windows icon resource (icon 1, which the window and taskbar use).
package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"math/rand"
	"os"

	"github.com/tc-hib/winres"
	"golang.org/x/image/vector"
)

const size = 1024

type pt struct{ x, y float32 }

// path builds one shape for the rasterizer.
type path struct{ r *vector.Rasterizer }

func newPath() *path { return &path{vector.NewRasterizer(size, size)} }

func (p *path) move(x, y float32) { p.r.MoveTo(x, y) }
func (p *path) line(x, y float32) { p.r.LineTo(x, y) }
func (p *path) cube(a, b, c pt)   { p.r.CubeTo(a.x, a.y, b.x, b.y, c.x, c.y) }
func (p *path) close()            { p.r.ClosePath() }
func (p *path) fill(dst *image.RGBA, src image.Image) {
	p.r.Draw(dst, dst.Bounds(), src, image.Point{})
}

const k = 0.5522847 // cubic Bézier handle length for a quarter circle

func (p *path) roundRect(x0, y0, x1, y1, r float32) {
	p.move(x0+r, y0)
	p.line(x1-r, y0)
	p.cube(pt{x1 - r + r*k, y0}, pt{x1, y0 + r - r*k}, pt{x1, y0 + r})
	p.line(x1, y1-r)
	p.cube(pt{x1, y1 - r + r*k}, pt{x1 - r + r*k, y1}, pt{x1 - r, y1})
	p.line(x0+r, y1)
	p.cube(pt{x0 + r - r*k, y1}, pt{x0, y1 - r + r*k}, pt{x0, y1 - r})
	p.line(x0, y0+r)
	p.cube(pt{x0, y0 + r - r*k}, pt{x0 + r - r*k, y0}, pt{x0 + r, y0})
	p.close()
}

// arch: the top half of an ellipse standing on the line y = base, from left to right (clockwise when reversed is false).
func (p *path) arch(cx, base, rx, ry float32, reversed bool) {
	if !reversed {
		p.move(cx-rx, base)
		p.cube(pt{cx - rx, base - ry*k}, pt{cx - rx*k, base - ry}, pt{cx, base - ry})
		p.cube(pt{cx + rx*k, base - ry}, pt{cx + rx, base - ry*k}, pt{cx + rx, base})
		p.close()
		return
	}
	p.move(cx+rx, base)
	p.cube(pt{cx + rx, base - ry*k}, pt{cx + rx*k, base - ry}, pt{cx, base - ry})
	p.cube(pt{cx - rx*k, base - ry}, pt{cx - rx, base - ry*k}, pt{cx - rx, base})
	p.close()
}

func (p *path) circle(cx, cy, r float32) {
	p.move(cx+r, cy)
	p.cube(pt{cx + r, cy + r*k}, pt{cx + r*k, cy + r}, pt{cx, cy + r})
	p.cube(pt{cx - r*k, cy + r}, pt{cx - r, cy + r*k}, pt{cx - r, cy})
	p.cube(pt{cx - r, cy - r*k}, pt{cx - r*k, cy - r}, pt{cx, cy - r})
	p.cube(pt{cx + r*k, cy - r}, pt{cx + r, cy - r*k}, pt{cx + r, cy})
	p.close()
}

func uniform(c color.NRGBA) image.Image { return image.NewUniform(c) }

// gradient fills the canvas from top to bottom between two colours.
func gradient(top, bottom color.NRGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		t := float64(y) / (size - 1)
		mix := func(a, b uint8) uint8 { return uint8(float64(a)*(1-t) + float64(b)*t + 0.5) }
		c := color.RGBA{mix(top.R, bottom.R), mix(top.G, bottom.G), mix(top.B, bottom.B), 255}
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func draw1024() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	const x0, y0, x1, y1, radius = 100, 100, 924, 924, 185

	// a soft shadow under the tile
	for i := 0; i < 12; i++ {
		s := newPath()
		off := float32(i) * 2.2
		s.roundRect(x0-off/2, y0+10+off/2, x1+off/2, y1+12+off, radius+off/2)
		s.fill(img, uniform(color.NRGBA{0, 0, 0, 6}))
	}

	// the tile: deep blue, brighter at the top, so it reads clearly in the Dock in light and dark mode
	tile := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(tile, tile.Bounds(), gradient(color.NRGBA{0x1f, 0x7a, 0xff, 255}, color.NRGBA{0x06, 0x16, 0x4a, 255}), image.Point{}, draw.Src)
	// a glow behind the hangar
	for i := 0; i < 30; i++ {
		g := newPath()
		g.circle(512, 640, float32(330-i*9))
		g.fill(tile, uniform(color.NRGBA{0x5a, 0xb4, 0xff, 5}))
	}
	// stars
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 26; i++ {
		x, y := 140+rng.Float32()*744, 140+rng.Float32()*300
		if math.Hypot(float64(x-512), float64(y-560)) < 330 {
			continue
		}
		st := newPath()
		st.circle(x, y, 2.5+rng.Float32()*4.5)
		st.fill(tile, uniform(color.NRGBA{255, 255, 255, uint8(150 + rng.Intn(105))}))
	}
	// the floor
	floor := newPath()
	floor.roundRect(170, 752, 854, 782, 15)
	floor.fill(tile, uniform(color.NRGBA{255, 255, 255, 235}))
	// the hangar: a white arch (an outer arch with the inner one cut out), with a light-tinted bay inside
	bay := newPath()
	bay.arch(512, 756, 280, 300, false)
	bay.fill(tile, uniform(color.NRGBA{0xb8, 0xdc, 0xff, 52}))
	ring := newPath()
	ring.arch(512, 756, 312, 332, false)
	ring.arch(512, 756, 272, 292, true)
	ring.fill(tile, uniform(color.NRGBA{255, 255, 255, 255}))
	// the ship, nose up, with an orange engine flame and cockpit
	flame := newPath()
	flame.move(470, 664)
	flame.line(554, 664)
	flame.cube(pt{548, 700}, pt{530, 728}, pt{512, 748})
	flame.cube(pt{494, 728}, pt{476, 700}, pt{470, 664})
	flame.close()
	flame.fill(tile, uniform(color.NRGBA{0xff, 0x9f, 0x0a, 255}))
	ship := newPath()
	ship.move(512, 392)
	ship.cube(pt{540, 430}, pt{556, 500}, pt{560, 560})
	ship.line(650, 640)
	ship.line(650, 676)
	ship.line(556, 650)
	ship.line(540, 672)
	ship.line(484, 672)
	ship.line(468, 650)
	ship.line(374, 676)
	ship.line(374, 640)
	ship.line(464, 560)
	ship.cube(pt{468, 500}, pt{484, 430}, pt{512, 392})
	ship.close()
	ship.fill(tile, uniform(color.NRGBA{255, 255, 255, 255}))
	cockpit := newPath()
	cockpit.move(512, 460)
	cockpit.cube(pt{526, 478}, pt{532, 506}, pt{532, 528})
	cockpit.line(492, 528)
	cockpit.cube(pt{492, 506}, pt{498, 478}, pt{512, 460})
	cockpit.close()
	cockpit.fill(tile, uniform(color.NRGBA{0x0a, 0x3d, 0xa8, 255}))

	// cut the tile to its rounded square and lay it on the canvas
	mask := image.NewAlpha(image.Rect(0, 0, size, size))
	m := newPath()
	m.roundRect(x0, y0, x1, y1, radius)
	m.r.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	draw.DrawMask(img, img.Bounds(), tile, image.Point{}, mask, image.Point{}, draw.Over)
	return img
}

func main() {
	if len(os.Args) < 2 {
		os.Stderr.WriteString("usage: icon out.png [out.syso]\n")
		os.Exit(2)
	}
	img := draw1024()
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
	f.Close()
	if len(os.Args) > 2 {
		icon, err := winres.NewIconFromResizedImage(img, nil)
		if err != nil {
			panic(err)
		}
		rs := winres.ResourceSet{}
		if err := rs.SetIcon(winres.ID(1), icon); err != nil {
			panic(err)
		}
		out, err := os.Create(os.Args[2])
		if err != nil {
			panic(err)
		}
		defer out.Close()
		if err := rs.WriteObject(out, winres.ArchAMD64); err != nil {
			panic(err)
		}
	}
}
