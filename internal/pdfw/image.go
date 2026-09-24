package pdfw

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"strings"
)

// Picture is one page's image, ready for the PDF: either a JPEG passed
// through untouched (DCTDecode) or raw 8-bit samples (Flate).
type Picture struct {
	Width, Height int
	Filter        string // "DCTDecode" or "" (Flate applied by the writer)
	ColorSpace    string // DeviceRGB, DeviceGray, DeviceCMYK
	Decode        string // e.g. "[1 0 1 0 1 0 1 0]" for Adobe CMYK JPEGs
	Data          []byte
}

// ErrNotJPEG is answered by ParseJPEG for anything else.
var ErrNotJPEG = errors.New("pdfw: not a JPEG")

// ParseJPEG reads the frame header of a JPEG to find its size and
// component count, so the file can be embedded as is.
func ParseJPEG(data []byte) (*Picture, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil, ErrNotJPEG
	}
	adobe := false
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			i++
			continue
		}
		marker := data[i+1]
		if marker == 0xFF {
			i++
			continue
		}
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) || marker == 0x01 {
			i += 2
			continue
		}
		if i+4 > len(data) {
			break
		}
		seglen := int(data[i+2])<<8 | int(data[i+3])
		if seglen < 2 || i+2+seglen > len(data) {
			return nil, errors.New("pdfw: truncated JPEG")
		}
		seg := data[i+4 : i+2+seglen]
		switch {
		case marker == 0xEE && len(seg) >= 5 && string(seg[:5]) == "Adobe":
			adobe = true
		case marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC:
			if len(seg) < 6 {
				return nil, errors.New("pdfw: bad JPEG frame header")
			}
			h := int(seg[1])<<8 | int(seg[2])
			w := int(seg[3])<<8 | int(seg[4])
			comps := int(seg[5])
			p := &Picture{Width: w, Height: h, Filter: "DCTDecode", Data: data}
			switch comps {
			case 1:
				p.ColorSpace = "DeviceGray"
			case 3:
				p.ColorSpace = "DeviceRGB"
			case 4:
				p.ColorSpace = "DeviceCMYK"
				if adobe {
					p.Decode = "[1 0 1 0 1 0 1 0]"
				}
			default:
				return nil, fmt.Errorf("pdfw: JPEG with %d components", comps)
			}
			if w == 0 || h == 0 {
				return nil, errors.New("pdfw: JPEG without dimensions")
			}
			return p, nil
		case marker == 0xDA:
			return nil, errors.New("pdfw: JPEG scan before frame header")
		}
		i += 2 + seglen
	}
	return nil, errors.New("pdfw: no JPEG frame header")
}

// FromImage flattens a decoded image onto white and packs it as RGB.
func FromImage(img image.Image) *Picture {
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Over)
	data := make([]byte, 0, b.Dx()*b.Dy()*3)
	for y := 0; y < b.Dy(); y++ {
		row := rgba.Pix[y*rgba.Stride : y*rgba.Stride+b.Dx()*4]
		for x := 0; x < b.Dx()*4; x += 4 {
			data = append(data, row[x], row[x+1], row[x+2])
		}
	}
	return &Picture{Width: b.Dx(), Height: b.Dy(), ColorSpace: "DeviceRGB", Data: data}
}

// maxSide is the longest page side in points (A4's long edge) an image
// page gets; pixels are otherwise 1:1 points.
const maxSide = 841.89

// Pictures writes one page per picture: the page takes the image's aspect
// ratio, scaled so its longer side is at most an A4 long edge, no margins.
func Pictures(pics []*Picture, title string) ([]byte, error) {
	if len(pics) == 0 {
		return nil, errors.New("pdfw: no pictures")
	}
	f := newFile()
	pagesNum := f.reserve()
	var kids []string
	for i, p := range pics {
		if p.Width <= 0 || p.Height <= 0 {
			return nil, fmt.Errorf("pdfw: picture %d has no size", i+1)
		}
		dict := fmt.Sprintf(" /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /%s /BitsPerComponent 8", p.Width, p.Height, p.ColorSpace)
		if p.Decode != "" {
			dict += " /Decode " + p.Decode
		}
		var img int
		if p.Filter == "DCTDecode" {
			img = f.addStream(dict+" /Filter /DCTDecode", p.Data, false)
		} else {
			img = f.addStream(dict, p.Data, true)
		}
		pw, ph := float64(p.Width), float64(p.Height)
		longest := pw
		if ph > longest {
			longest = ph
		}
		if longest > maxSide {
			s := maxSide / longest
			pw, ph = pw*s, ph*s
		}
		content := fmt.Sprintf("q %s 0 0 %s 0 0 cm /Im1 Do Q", ftoa(pw), ftoa(ph))
		cn := f.addStream("", []byte(content), false)
		pn := f.add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %s %s] /Resources << /XObject << /Im1 %d 0 R >> >> /Contents %d 0 R >>", pagesNum, ftoa(pw), ftoa(ph), img, cn))
		kids = append(kids, fmt.Sprintf("%d 0 R", pn))
	}
	f.writeAt(pagesNum, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(kids)))
	root := f.add(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesNum))
	return f.finish(root, title), nil
}

// IsPDF is a cheap header check used by tests and the job layer.
func IsPDF(b []byte) bool { return bytes.HasPrefix(b, []byte("%PDF-")) }
