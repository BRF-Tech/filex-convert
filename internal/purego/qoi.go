package purego

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"io"
)

// QOI ("Quite OK Image") codec written from the one-page specification
// (qoiformat.org): a 14-byte header, then chunks RGB / RGBA / INDEX /
// DIFF / LUMA / RUN, then an 8-byte end marker.

const (
	qoiOpIndex = 0x00
	qoiOpDiff  = 0x40
	qoiOpLuma  = 0x80
	qoiOpRun   = 0xc0
	qoiOpRGB   = 0xfe
	qoiOpRGBA  = 0xff
	qoiMask2   = 0xc0
)

var qoiMagic = []byte("qoif")
var qoiEnd = []byte{0, 0, 0, 0, 0, 0, 0, 1}

func qoiHash(r, g, b, a uint8) int {
	return (int(r)*3 + int(g)*5 + int(b)*7 + int(a)*11) % 64
}

func decodeQOI(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(data) < 22 || string(data[:4]) != "qoif" {
		return nil, errors.New("qoi: bad header")
	}
	w := int(binary.BigEndian.Uint32(data[4:8]))
	h := int(binary.BigEndian.Uint32(data[8:12]))
	if w <= 0 || h <= 0 || w > 1<<15 || h > 1<<15 || w*h > 64<<20 {
		return nil, errors.New("qoi: unreasonable dimensions")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	var index [64][4]uint8
	px := [4]uint8{0, 0, 0, 255}
	p := 14
	run := 0
	n := len(data) - 8
	for i := 0; i < w*h; i++ {
		if run > 0 {
			run--
		} else if p < n {
			b1 := data[p]
			p++
			switch {
			case b1 == qoiOpRGB:
				if p+3 > n {
					return nil, errors.New("qoi: truncated")
				}
				px[0], px[1], px[2] = data[p], data[p+1], data[p+2]
				p += 3
			case b1 == qoiOpRGBA:
				if p+4 > n {
					return nil, errors.New("qoi: truncated")
				}
				px[0], px[1], px[2], px[3] = data[p], data[p+1], data[p+2], data[p+3]
				p += 4
			case b1&qoiMask2 == qoiOpIndex:
				px = index[b1]
			case b1&qoiMask2 == qoiOpDiff:
				px[0] += uint8((b1>>4)&3) - 2
				px[1] += uint8((b1>>2)&3) - 2
				px[2] += uint8(b1&3) - 2
			case b1&qoiMask2 == qoiOpLuma:
				if p >= n {
					return nil, errors.New("qoi: truncated")
				}
				b2 := data[p]
				p++
				vg := uint8(b1&0x3f) - 32
				px[0] += vg - 8 + ((b2 >> 4) & 0x0f)
				px[1] += vg
				px[2] += vg - 8 + (b2 & 0x0f)
			case b1&qoiMask2 == qoiOpRun:
				run = int(b1 & 0x3f)
			}
			index[qoiHash(px[0], px[1], px[2], px[3])] = px
		}
		o := i * 4
		img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = px[0], px[1], px[2], px[3]
	}
	return img, nil
}

func encodeQOI(w io.Writer, img image.Image) error {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	if width <= 0 || height <= 0 {
		return errors.New("qoi: empty image")
	}
	out := make([]byte, 0, width*height/2+22)
	out = append(out, qoiMagic...)
	out = binary.BigEndian.AppendUint32(out, uint32(width))
	out = binary.BigEndian.AppendUint32(out, uint32(height))
	out = append(out, 4, 0) // channels, sRGB with linear alpha
	var index [64][4]uint8
	prev := [4]uint8{0, 0, 0, 255}
	run := 0
	total := width * height
	nrgba, isNRGBA := img.(*image.NRGBA)
	for i := 0; i < total; i++ {
		var px [4]uint8
		if isNRGBA {
			o := (i/width)*nrgba.Stride + (i%width)*4
			px = [4]uint8{nrgba.Pix[o], nrgba.Pix[o+1], nrgba.Pix[o+2], nrgba.Pix[o+3]}
		} else {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+i%width, b.Min.Y+i/width)).(color.NRGBA)
			px = [4]uint8{c.R, c.G, c.B, c.A}
		}
		if px == prev {
			run++
			if run == 62 || i == total-1 {
				out = append(out, byte(qoiOpRun|(run-1)))
				run = 0
			}
			continue
		}
		if run > 0 {
			out = append(out, byte(qoiOpRun|(run-1)))
			run = 0
		}
		h := qoiHash(px[0], px[1], px[2], px[3])
		if index[h] == px {
			out = append(out, byte(qoiOpIndex|h))
		} else {
			index[h] = px
			if px[3] == prev[3] {
				vr := int8(px[0] - prev[0])
				vg := int8(px[1] - prev[1])
				vb := int8(px[2] - prev[2])
				vgr := vr - vg
				vgb := vb - vg
				switch {
				case vr > -3 && vr < 2 && vg > -3 && vg < 2 && vb > -3 && vb < 2:
					out = append(out, byte(qoiOpDiff|int(vr+2)<<4|int(vg+2)<<2|int(vb+2)))
				case vgr > -9 && vgr < 8 && vg > -33 && vg < 32 && vgb > -9 && vgb < 8:
					out = append(out, byte(qoiOpLuma|int(vg+32)), byte(int(vgr+8)<<4|int(vgb+8)))
				default:
					out = append(out, qoiOpRGB, px[0], px[1], px[2])
				}
			} else {
				out = append(out, qoiOpRGBA, px[0], px[1], px[2], px[3])
			}
		}
		prev = px
	}
	out = append(out, qoiEnd...)
	_, err := w.Write(out)
	return err
}
