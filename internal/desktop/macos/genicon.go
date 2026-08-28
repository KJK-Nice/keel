//go:build ignore

// Generate Keel's macOS app icon (PNG + ICNS) from the same chevron as icon.svg.
//
//	go run genicon.go
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

const (
	srcSize       = 32.0
	srcStroke     = 3.2
	bgR, bgG, bgB = 0x14, 0x14, 0x14
	fgR, fgG, fgB = 0xec, 0xec, 0xec
)

var chevron = [3][2]float64{
	{4, 25},
	{16, 5},
	{28, 25},
}

func main() {
	dir, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	png1024 := render(1024)
	if err := writePNG(filepath.Join(dir, "icon.png"), png1024); err != nil {
		fatal(err)
	}

	sizes := []int{16, 32, 64, 128, 256, 512, 1024}
	encoded := make(map[int][]byte, len(sizes))
	for _, s := range sizes {
		img := png1024
		if s != 1024 {
			img = render(s)
		}
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&buf, img); err != nil {
			fatal(err)
		}
		encoded[s] = buf.Bytes()
	}

	// PNG-based ICNS elements. Largest first; @2x types reuse the 2× raster.
	chunks := []icnsChunk{
		{typ: "ic10", data: encoded[1024]},
		{typ: "ic09", data: encoded[512]},
		{typ: "ic14", data: encoded[512]},
		{typ: "ic08", data: encoded[256]},
		{typ: "ic13", data: encoded[256]},
		{typ: "ic07", data: encoded[128]},
		{typ: "icp6", data: encoded[64]},
		{typ: "ic12", data: encoded[64]},
		{typ: "icp5", data: encoded[32]},
		{typ: "ic11", data: encoded[32]},
		{typ: "icp4", data: encoded[16]},
	}
	if err := os.WriteFile(filepath.Join(dir, "Keel.icns"), packICNS(chunks), 0o644); err != nil {
		fatal(err)
	}
	fmt.Println("wrote icon.png and Keel.icns")
}

func render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	scale := float64(size) / srcSize
	stroke := srcStroke * scale
	if size <= 32 && stroke < 2.2 {
		stroke = 2.2
	}
	r := stroke / 2
	p0 := [2]float64{chevron[0][0] * scale, chevron[0][1] * scale}
	p1 := [2]float64{chevron[1][0] * scale, chevron[1][1] * scale}
	p2 := [2]float64{chevron[2][0] * scale, chevron[2][1] * scale}
	poly := miterStroke(p0, p1, p2, r)

	bg := color.NRGBA{R: bgR, G: bgG, B: bgB, A: 255}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px := float64(x) + 0.5
			py := float64(y) + 0.5
			d := sdfPoly(px, py, poly)
			a := clamp(0.5-d, 0, 1)
			if a <= 0 {
				img.SetNRGBA(x, y, bg)
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{
				R: lerp(bgR, fgR, a),
				G: lerp(bgG, fgG, a),
				B: lerp(bgB, fgB, a),
				A: 255,
			})
		}
	}
	return img
}

// miterStroke is a chevron outline with butt caps and a mitered peak.
func miterStroke(p0, p1, p2 [2]float64, r float64) [][2]float64 {
	d0x, d0y := norm(p1[0]-p0[0], p1[1]-p0[1])
	d1x, d1y := norm(p2[0]-p1[0], p2[1]-p1[1])
	n0x, n0y := -d0y, d0x
	n1x, n1y := -d1y, d1x
	jx, jy := n0x+n1x, n0y+n1y
	jl := math.Hypot(jx, jy)
	if jl < 1e-9 {
		return [][2]float64{p0, p1, p2}
	}
	jx, jy = jx/jl, jy/jl
	m := r / (jx*n0x + jy*n0y)
	return [][2]float64{
		{p0[0] - n0x*r, p0[1] - n0y*r},
		{p1[0] - jx*m, p1[1] - jy*m},
		{p2[0] - n1x*r, p2[1] - n1y*r},
		{p2[0] + n1x*r, p2[1] + n1y*r},
		{p1[0] + jx*m, p1[1] + jy*m},
		{p0[0] + n0x*r, p0[1] + n0y*r},
	}
}

func norm(x, y float64) (float64, float64) {
	l := math.Hypot(x, y)
	if l == 0 {
		return 0, 0
	}
	return x / l, y / l
}

func sdfPoly(px, py float64, poly [][2]float64) float64 {
	minD := math.Inf(1)
	inside := false
	n := len(poly)
	for i := 0; i < n; i++ {
		ax, ay := poly[i][0], poly[i][1]
		bx, by := poly[(i+1)%n][0], poly[(i+1)%n][1]
		minD = math.Min(minD, distToSeg(px, py, ax, ay, bx, by))
		// even-odd ray cast to +x
		if (ay > py) != (by > py) {
			xint := ax + (py-ay)/(by-ay)*(bx-ax)
			if px < xint {
				inside = !inside
			}
		}
	}
	if inside {
		return -minD
	}
	return minD
}

func distToSeg(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return hypot(px-ax, py-ay)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / l2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return hypot(px-(ax+t*dx), py-(ay+t*dy))
}

func hypot(x, y float64) float64 { return math.Hypot(x, y) }

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func lerp(a, b uint8, t float64) uint8 {
	return uint8(math.Round(float64(a) + (float64(b)-float64(a))*t))
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	return enc.Encode(f, img)
}

type icnsChunk struct {
	typ  string
	data []byte
}

func packICNS(chunks []icnsChunk) []byte {
	var body bytes.Buffer
	for _, c := range chunks {
		if len(c.typ) != 4 {
			fatal(fmt.Errorf("icns type %q must be 4 bytes", c.typ))
		}
		body.WriteString(c.typ)
		if err := binary.Write(&body, binary.BigEndian, uint32(8+len(c.data))); err != nil {
			fatal(err)
		}
		body.Write(c.data)
	}
	out := make([]byte, 8+body.Len())
	copy(out[0:4], "icns")
	binary.BigEndian.PutUint32(out[4:8], uint32(len(out)))
	copy(out[8:], body.Bytes())
	return out
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
