package purego

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"io"

	"golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
)

// ICO codec. Reading takes the largest entry (PNG-compressed or BMP DIB
// with its AND mask); writing emits one PNG-compressed entry per size,
// the form every browser and Windows since Vista accept. Sizes come from
// the icon_sizes knob: favicon (16, 32, 48), app (16 … 256) or single.

var iconSizeSets = map[string][]int{
	"favicon": {16, 32, 48},
	"app":     {16, 24, 32, 48, 64, 128, 256},
}

func decodeICO(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(data) < 6 || binary.LittleEndian.Uint16(data[0:2]) != 0 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return nil, errors.New("ico: bad header")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count == 0 || 6+count*16 > len(data) {
		return nil, errors.New("ico: no entries")
	}
	best := -1
	bestSize := 0
	for i := 0; i < count; i++ {
		e := data[6+i*16:]
		w, h := int(e[0]), int(e[1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		if w*h > bestSize {
			bestSize = w * h
			best = i
		}
	}
	e := data[6+best*16:]
	size := int(binary.LittleEndian.Uint32(e[8:12]))
	off := int(binary.LittleEndian.Uint32(e[12:16]))
	if off <= 0 || size <= 0 || off+size > len(data) {
		return nil, errors.New("ico: entry out of range")
	}
	entry := data[off : off+size]
	if bytes.HasPrefix(entry, []byte("\x89PNG")) {
		return png.Decode(bytes.NewReader(entry))
	}
	return decodeDIB(entry)
}

// decodeDIB reads the BITMAPINFOHEADER + pixels of an icon entry. The
// height field counts the XOR and AND masks together, so it is halved.
func decodeDIB(dib []byte) (image.Image, error) {
	if len(dib) < 40 {
		return nil, errors.New("ico: short DIB")
	}
	hdrSize := int(binary.LittleEndian.Uint32(dib[0:4]))
	w := int(int32(binary.LittleEndian.Uint32(dib[4:8])))
	h := int(int32(binary.LittleEndian.Uint32(dib[8:12]))) / 2
	bpp := int(binary.LittleEndian.Uint16(dib[14:16]))
	if w <= 0 || h <= 0 || w > 1024 || h > 1024 || hdrSize < 40 {
		return nil, errors.New("ico: bad DIB header")
	}
	if bpp == 32 {
		// straight BGRA rows, bottom-up; x/image/bmp drops alpha, so read it here
		stride := w * 4
		pix := dib[hdrSize:]
		if len(pix) < stride*h {
			return nil, errors.New("ico: truncated 32-bit DIB")
		}
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			row := pix[(h-1-y)*stride:]
			for x := 0; x < w; x++ {
				o := y*img.Stride + x*4
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = row[x*4+2], row[x*4+1], row[x*4], row[x*4+3]
			}
		}
		return img, nil
	}
	// other depths: synthesise a BMP file (header + DIB with the true height)
	// and let the BMP decoder handle palettes and bit packing
	fixed := append([]byte(nil), dib...)
	binary.LittleEndian.PutUint32(fixed[8:12], uint32(h))
	palette := 0
	if bpp <= 8 {
		colors := int(binary.LittleEndian.Uint32(dib[32:36]))
		if colors == 0 {
			colors = 1 << bpp
		}
		palette = colors * 4
	}
	var buf bytes.Buffer
	buf.Write([]byte{'B', 'M'})
	total := 14 + len(fixed)
	buf.Write(binary.LittleEndian.AppendUint32(nil, uint32(total)))
	buf.Write([]byte{0, 0, 0, 0})
	buf.Write(binary.LittleEndian.AppendUint32(nil, uint32(14+hdrSize+palette)))
	buf.Write(fixed)
	img, err := bmp.Decode(&buf)
	if err != nil {
		return nil, err
	}
	// apply the 1-bit AND mask as transparency
	rowBytes := ((w + 31) / 32) * 4
	xorBytes := ((w*bpp + 31) / 32) * 4 * h
	maskStart := hdrSize + palette + xorBytes
	if maskStart+rowBytes*h > len(dib) {
		return img, nil
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), img, image.Point{}, draw.Src)
	for y := 0; y < h; y++ {
		row := dib[maskStart+(h-1-y)*rowBytes:]
		for x := 0; x < w; x++ {
			if row[x/8]&(0x80>>(x%8)) != 0 {
				out.Pix[y*out.Stride+x*4+3] = 0
			}
		}
	}
	return out, nil
}

// encodeICO writes PNG entries at the requested sizes (the source is
// scaled down with Catmull-Rom; it is never scaled up beyond its size
// except for the smallest favicon sizes, where a 16 px icon from a large
// source is the point).
func encodeICO(w io.Writer, img image.Image, sizes []int) error {
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return errors.New("ico: empty image")
	}
	var entries [][]byte
	var dims []int
	for _, s := range sizes {
		if s > 256 {
			s = 256
		}
		dst := image.NewNRGBA(image.Rect(0, 0, s, s))
		// fit the image into the square, centred, keeping its aspect
		sw, sh := b.Dx(), b.Dy()
		var tw, th int
		if sw >= sh {
			tw, th = s, max(1, s*sh/sw)
		} else {
			tw, th = max(1, s*sw/sh), s
		}
		r := image.Rect((s-tw)/2, (s-th)/2, (s-tw)/2+tw, (s-th)/2+th)
		xdraw.CatmullRom.Scale(dst, r, img, b, xdraw.Over, nil)
		var pb bytes.Buffer
		if err := png.Encode(&pb, dst); err != nil {
			return err
		}
		entries = append(entries, pb.Bytes())
		dims = append(dims, s)
	}
	var out bytes.Buffer
	out.Write([]byte{0, 0, 1, 0})
	out.Write(binary.LittleEndian.AppendUint16(nil, uint16(len(entries))))
	off := 6 + 16*len(entries)
	for i, e := range entries {
		d := dims[i]
		if d == 256 {
			d = 0
		}
		out.Write([]byte{byte(d), byte(d), 0, 0, 1, 0, 32, 0})
		out.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(e))))
		out.Write(binary.LittleEndian.AppendUint32(nil, uint32(off)))
		off += len(e)
	}
	for _, e := range entries {
		out.Write(e)
	}
	_, err := w.Write(out.Bytes())
	return err
}

// icoSizes resolves the icon_sizes knob against the source size.
func icoSizes(set string, img image.Image) []int {
	if s, ok := iconSizeSets[set]; ok {
		return s
	}
	b := img.Bounds()
	s := max(b.Dx(), b.Dy())
	if s > 256 {
		s = 256
	}
	if s < 1 {
		s = 1
	}
	return []int{s}
}
