package purego

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"strconv"
)

// Netpbm codec: reads P1–P6 (ASCII and binary bitmap, graymap and
// pixmap) and writes P6 (binary RGB), the widest-understood member of the
// family.

func pnmToken(r *bufio.Reader) (string, error) {
	var tok []byte
	for {
		c, err := r.ReadByte()
		if err != nil {
			if len(tok) > 0 && err == io.EOF {
				return string(tok), nil
			}
			return "", err
		}
		if c == '#' {
			for c != '\n' {
				c, err = r.ReadByte()
				if err != nil {
					return "", err
				}
			}
			continue
		}
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			if len(tok) > 0 {
				return string(tok), nil
			}
			continue
		}
		tok = append(tok, c)
	}
}

func pnmInt(r *bufio.Reader) (int, error) {
	t, err := pnmToken(r)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(t)
	if err != nil {
		return 0, fmt.Errorf("pnm: bad number %q", t)
	}
	return n, nil
}

func decodePNM(rd io.Reader) (image.Image, error) {
	r := bufio.NewReader(rd)
	magic, err := pnmToken(r)
	if err != nil {
		return nil, err
	}
	if len(magic) != 2 || magic[0] != 'P' || magic[1] < '1' || magic[1] > '6' {
		return nil, errors.New("pnm: bad magic")
	}
	kind := magic[1]
	w, err := pnmInt(r)
	if err != nil {
		return nil, err
	}
	h, err := pnmInt(r)
	if err != nil {
		return nil, err
	}
	if w <= 0 || h <= 0 || w > 1<<15 || h > 1<<15 || w*h > 64<<20 {
		return nil, errors.New("pnm: unreasonable dimensions")
	}
	maxv := 1
	if kind != '1' && kind != '4' {
		maxv, err = pnmInt(r)
		if err != nil {
			return nil, err
		}
		if maxv <= 0 || maxv > 65535 {
			return nil, errors.New("pnm: bad maxval")
		}
	}
	scale := func(v int) uint8 {
		if maxv == 255 {
			return uint8(v)
		}
		return uint8(v * 255 / maxv)
	}
	readBin := func() (int, error) {
		if maxv > 255 {
			hi, err := r.ReadByte()
			if err != nil {
				return 0, err
			}
			lo, err := r.ReadByte()
			if err != nil {
				return 0, err
			}
			return int(hi)<<8 | int(lo), nil
		}
		b, err := r.ReadByte()
		return int(b), err
	}
	switch kind {
	case '1', '4': // bitmap: 1 = black
		img := image.NewGray(image.Rect(0, 0, w, h))
		if kind == '1' {
			for i := 0; i < w*h; i++ {
				t, err := pnmToken(r)
				if err != nil {
					return nil, err
				}
				if t[0] == '1' {
					img.Pix[i] = 0
				} else {
					img.Pix[i] = 255
				}
			}
			return img, nil
		}
		rowBytes := (w + 7) / 8
		for y := 0; y < h; y++ {
			for bx := 0; bx < rowBytes; bx++ {
				b, err := r.ReadByte()
				if err != nil {
					return nil, err
				}
				for bit := 0; bit < 8; bit++ {
					x := bx*8 + bit
					if x >= w {
						break
					}
					if b&(0x80>>bit) != 0 {
						img.Pix[y*w+x] = 0
					} else {
						img.Pix[y*w+x] = 255
					}
				}
			}
		}
		return img, nil
	case '2', '5': // graymap
		img := image.NewGray(image.Rect(0, 0, w, h))
		for i := 0; i < w*h; i++ {
			var v int
			if kind == '2' {
				v, err = pnmInt(r)
			} else {
				v, err = readBin()
			}
			if err != nil {
				return nil, err
			}
			img.Pix[i] = scale(v)
		}
		return img, nil
	default: // pixmap
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for i := 0; i < w*h; i++ {
			var rgb [3]int
			for c := 0; c < 3; c++ {
				if kind == '3' {
					rgb[c], err = pnmInt(r)
				} else {
					rgb[c], err = readBin()
				}
				if err != nil {
					return nil, err
				}
			}
			img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = scale(rgb[0]), scale(rgb[1]), scale(rgb[2]), 255
		}
		return img, nil
	}
}

func encodePNM(w io.Writer, img image.Image) error {
	b := img.Bounds()
	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "P6\n%d %d\n255\n", b.Dx(), b.Dy())
	flat := flatten(img)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(flat.At(x, y)).(color.NRGBA)
			bw.Write([]byte{c.R, c.G, c.B})
		}
	}
	return bw.Flush()
}
