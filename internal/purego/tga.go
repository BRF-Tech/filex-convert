package purego

import (
	"errors"
	"image"
	"image/color"
	"io"
)

// Targa codec: reads uncompressed and RLE true-colour (24/32-bit),
// grayscale (8-bit) and colour-mapped (8-bit index) images in either
// origin; writes uncompressed 32-bit BGRA with the origin top-left.

func decodeTGA(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(data) < 18 {
		return nil, errors.New("tga: short header")
	}
	idLen := int(data[0])
	cmapType := data[1]
	imgType := data[2]
	cmapStart := int(data[3]) | int(data[4])<<8
	cmapLen := int(data[5]) | int(data[6])<<8
	cmapBits := int(data[7])
	w := int(data[12]) | int(data[13])<<8
	h := int(data[14]) | int(data[15])<<8
	bpp := int(data[16])
	desc := data[17]
	topLeft := desc&0x20 != 0
	rightToLeft := desc&0x10 != 0
	if w <= 0 || h <= 0 || w > 1<<15 || h > 1<<15 || w*h > 64<<20 {
		return nil, errors.New("tga: unreasonable dimensions")
	}
	p := 18 + idLen
	var cmap []color.NRGBA
	if cmapType == 1 {
		entry := (cmapBits + 7) / 8
		if p+cmapLen*entry > len(data) {
			return nil, errors.New("tga: truncated colour map")
		}
		cmap = make([]color.NRGBA, cmapStart+cmapLen)
		for i := 0; i < cmapLen; i++ {
			cmap[cmapStart+i] = tgaPixel(data[p+i*entry:], cmapBits)
		}
		p += cmapLen * entry
	}
	rle := imgType >= 9
	base := imgType
	if rle {
		base -= 8
	}
	bytesPP := (bpp + 7) / 8
	if bytesPP < 1 || bytesPP > 4 {
		return nil, errors.New("tga: unsupported depth")
	}
	pix := make([]byte, w*h*bytesPP)
	if rle {
		o := 0
		for o < len(pix) {
			if p >= len(data) {
				return nil, errors.New("tga: truncated RLE data")
			}
			hdr := data[p]
			p++
			count := int(hdr&0x7f) + 1
			if hdr&0x80 != 0 {
				if p+bytesPP > len(data) {
					return nil, errors.New("tga: truncated RLE data")
				}
				for i := 0; i < count && o < len(pix); i++ {
					copy(pix[o:o+bytesPP], data[p:p+bytesPP])
					o += bytesPP
				}
				p += bytesPP
				continue
			}
			n := count * bytesPP
			if p+n > len(data) {
				return nil, errors.New("tga: truncated RLE data")
			}
			n = min(n, len(pix)-o)
			copy(pix[o:o+n], data[p:p+n])
			o += n
			p += count * bytesPP
		}
	} else {
		if p+len(pix) > len(data) {
			return nil, errors.New("tga: truncated pixel data")
		}
		copy(pix, data[p:p+len(pix)])
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		dy := y
		if !topLeft {
			dy = h - 1 - y
		}
		for x := 0; x < w; x++ {
			dx := x
			if rightToLeft {
				dx = w - 1 - x
			}
			src := pix[(y*w+x)*bytesPP:]
			var c color.NRGBA
			switch base {
			case 1: // colour-mapped
				idx := int(src[0])
				if bytesPP == 2 {
					idx |= int(src[1]) << 8
				}
				if idx < len(cmap) {
					c = cmap[idx]
				}
			case 3: // grayscale
				c = color.NRGBA{R: src[0], G: src[0], B: src[0], A: 255}
			default:
				c = tgaPixel(src, bpp)
			}
			o := dy*img.Stride + dx*4
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = c.R, c.G, c.B, c.A
		}
	}
	return img, nil
}

func tgaPixel(b []byte, bits int) color.NRGBA {
	switch bits {
	case 32:
		return color.NRGBA{R: b[2], G: b[1], B: b[0], A: b[3]}
	case 24:
		return color.NRGBA{R: b[2], G: b[1], B: b[0], A: 255}
	case 15, 16:
		v := uint16(b[0]) | uint16(b[1])<<8
		r := uint8((v>>10)&0x1f) << 3
		g := uint8((v>>5)&0x1f) << 3
		bl := uint8(v&0x1f) << 3
		return color.NRGBA{R: r, G: g, B: bl, A: 255}
	}
	return color.NRGBA{R: b[0], G: b[0], B: b[0], A: 255}
}

func encodeTGA(w io.Writer, img image.Image) error {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	if width > 65535 || height > 65535 {
		return errors.New("tga: image too large")
	}
	out := make([]byte, 18, 18+width*height*4)
	out[2] = 2 // uncompressed true-colour
	out[12], out[13] = byte(width), byte(width>>8)
	out[14], out[15] = byte(height), byte(height>>8)
	out[16] = 32
	out[17] = 0x28 // 8 alpha bits, top-left origin
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			out = append(out, c.B, c.G, c.R, c.A)
		}
	}
	_, err := w.Write(out)
	return err
}
