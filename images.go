package main

// Pictures for the desktop window: thumbnails are decoded in the background at the size they are shown, and the most
// recently used are kept.

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sync"

	"gioui.org/op/paint"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

type thumbKey struct {
	name string
	size image.Point
}

type thumbEntry struct {
	op     paint.ImageOp
	ok     bool
	used   uint64
	loaded bool
}

type thumbCache struct {
	mu         sync.Mutex
	entries    map[thumbKey]*thumbEntry
	clock      uint64
	slots      chan struct{}
	invalidate func()
}

var thumbs = &thumbCache{entries: map[thumbKey]*thumbEntry{}, slots: make(chan struct{}, 4)}

const thumbCacheSize = 500

// get returns the picture at the given pixel size, or false while it is still loading (or can't be read).
func (c *thumbCache) get(name string, size image.Point) (paint.ImageOp, bool) {
	k := thumbKey{filepath.Base(name), size}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	if e, found := c.entries[k]; found {
		e.used = c.clock
		return e.op, e.ok
	}
	e := &thumbEntry{used: c.clock}
	c.entries[k] = e
	if len(c.entries) > thumbCacheSize {
		c.evict()
	}
	go c.load(k, e)
	return paint.ImageOp{}, false
}

func (c *thumbCache) evict() {
	var oldest thumbKey
	var at uint64 = ^uint64(0)
	for k, e := range c.entries {
		if e.loaded && e.used < at {
			oldest, at = k, e.used
		}
	}
	delete(c.entries, oldest)
}

// forget drops every cached picture (after a restore, a fresh start or a move of the data folder).
func (c *thumbCache) forget() {
	c.mu.Lock()
	c.entries = map[thumbKey]*thumbEntry{}
	c.mu.Unlock()
}

func (c *thumbCache) load(k thumbKey, e *thumbEntry) {
	c.slots <- struct{}{}
	defer func() { <-c.slots }()
	img, err := decodeFile(filepath.Join(imageDir, k.name))
	var op paint.ImageOp
	ok := err == nil
	if ok {
		op = paint.NewImageOp(cover(img, k.size))
	}
	c.mu.Lock()
	e.op, e.ok, e.loaded = op, ok, true
	c.mu.Unlock()
	if c.invalidate != nil {
		c.invalidate()
	}
}

// Pictures are decoded only after their header says they are a JPEG, PNG, GIF or WebP of a sensible size, so a small
// file claiming enormous dimensions can't make Hangar run out of memory.
const (
	maxPictureSide   = 16384
	maxPicturePixels = 40_000_000
)

var pictureFormats = map[string]string{"jpeg": "image/jpeg", "png": "image/png", "gif": "image/gif", "webp": "image/webp"}

// checkPicture reads only a picture's header and returns its real type ("image/png"...), whatever it claims to be.
func checkPicture(data []byte) (string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", userErr("That isn't a picture Hangar can read (JPEG, PNG, GIF or WebP).")
	}
	ctype := pictureFormats[format]
	if ctype == "" {
		return "", userErr("That isn't a picture Hangar can read (JPEG, PNG, GIF or WebP).")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxPictureSide || cfg.Height > maxPictureSide ||
		int64(cfg.Width)*int64(cfg.Height) > maxPicturePixels {
		return "", userErr("That picture is too large (%d×%d pixels).", cfg.Width, cfg.Height)
	}
	return ctype, nil
}

// decodePicture decodes a picture after checkPicture has accepted it.
func decodePicture(data []byte) (image.Image, error) {
	if _, err := checkPicture(data); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func decodeFile(path string) (image.Image, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > 4*maxImageBytes {
		return nil, userErr("That picture file is too large.")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodePicture(data)
}

// cover scales a picture to fill size exactly, cropping what sticks out (like CSS object-fit: cover).
func cover(src image.Image, size image.Point) *image.RGBA {
	b := src.Bounds()
	if size.X <= 0 || size.Y <= 0 || b.Dx() == 0 || b.Dy() == 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	sw, sh := float64(b.Dx()), float64(b.Dy())
	scale := max64(float64(size.X)/sw, float64(size.Y)/sh)
	cw, ch := float64(size.X)/scale, float64(size.Y)/scale
	crop := image.Rect(int((sw-cw)/2), int((sh-ch)/2), int((sw+cw)/2), int((sh+ch)/2)).Add(b.Min)
	dst := image.NewRGBA(image.Rectangle{Max: size})
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, crop, draw.Src, nil)
	return dst
}

// contain scales a picture to fit inside size, keeping all of it (like object-fit: contain).
func contain(src image.Image, size image.Point) *image.RGBA {
	b := src.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	scale := min64(float64(size.X)/float64(b.Dx()), float64(size.Y)/float64(b.Dy()))
	w, h := int(float64(b.Dx())*scale+0.5), int(float64(b.Dy())*scale+0.5)
	dst := image.NewRGBA(image.Rect(0, 0, max(w, 1), max(h, 1)))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

// shrinkForStorage keeps stored pictures small: at most 480 px on the long side, saved as JPEG.
func shrinkForStorage(data []byte) ([]byte, string, error) {
	img, err := decodePicture(data)
	if err != nil {
		return nil, "", err
	}
	b := img.Bounds()
	scale := min64(1, 480/max64(float64(b.Dx()), float64(b.Dy())))
	w, h := max(1, int(float64(b.Dx())*scale+0.5)), max(1, int(float64(b.Dy())*scale+0.5))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src) // transparency becomes white
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", err
	}
	return out.Bytes(), "image/jpeg", nil
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
