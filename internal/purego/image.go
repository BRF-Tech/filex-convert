package purego

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strings"

	"github.com/HugoSmits86/nativewebp"
	"golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"

	"github.com/brf-tech/filex-convert/internal/options"
	"github.com/brf-tech/filex-convert/internal/pdfw"
)

// Raster formats Go decodes and encodes on its own: the standard codecs,
// golang.org/x/image (BMP, TIFF, WebP decoding), nativewebp (lossless WebP
// encoding) and the small codecs in this package (QOI, Netpbm, Targa, ICO).

var decoders = map[string]func(io.Reader) (image.Image, error){
	"png":  png.Decode,
	"jpg":  jpeg.Decode,
	"gif":  gif.Decode,
	"bmp":  bmp.Decode,
	"tiff": tiff.Decode,
	"webp": decodeWebP,
	"qoi":  decodeQOI,
	"pnm":  decodePNM,
	"tga":  decodeTGA,
	"ico":  decodeICO,
}

// decodeWebP tries x/image (lossy VP8 and lossless) first, then nativewebp
// for the lossless variants x/image refuses.
func decodeWebP(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	img, err := webp.Decode(bytes.NewReader(data))
	if err == nil {
		return img, nil
	}
	if img2, err2 := nativewebp.Decode(bytes.NewReader(data)); err2 == nil {
		return img2, nil
	}
	return nil, err
}

var encoders = map[string]func(io.Writer, image.Image, options.Options) error{
	"png": func(w io.Writer, img image.Image, _ options.Options) error {
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		return enc.Encode(w, img)
	},
	"jpg": func(w io.Writer, img image.Image, o options.Options) error {
		q := clamp(o.Int(options.Quality), 1, 100)
		return jpeg.Encode(w, flatten(img), &jpeg.Options{Quality: q})
	},
	"gif": func(w io.Writer, img image.Image, _ options.Options) error {
		return gif.Encode(w, img, &gif.Options{NumColors: 256})
	},
	"bmp": func(w io.Writer, img image.Image, _ options.Options) error {
		return bmp.Encode(w, flatten(img))
	},
	"tiff": func(w io.Writer, img image.Image, _ options.Options) error {
		return tiff.Encode(w, img, &tiff.Options{Compression: tiff.Deflate})
	},
	"webp": func(w io.Writer, img image.Image, _ options.Options) error {
		return nativewebp.Encode(w, toNRGBA(img), nil)
	},
	"qoi": func(w io.Writer, img image.Image, _ options.Options) error {
		return encodeQOI(w, toNRGBA(img))
	},
	"pnm": func(w io.Writer, img image.Image, _ options.Options) error {
		return encodePNM(w, img)
	},
	"tga": func(w io.Writer, img image.Image, _ options.Options) error {
		return encodeTGA(w, img)
	},
	"ico": func(w io.Writer, img image.Image, o options.Options) error {
		return encodeICO(w, img, icoSizes(o.String(options.IconSizes), img))
	},
}

// flatten composites an image with alpha onto white: JPEG and BMP have no
// alpha and the encoders would otherwise drop it to black.
func flatten(img image.Image) image.Image {
	switch img.(type) {
	case *image.RGBA, *image.NRGBA, *image.RGBA64, *image.NRGBA64, *image.Paletted:
		b := img.Bounds()
		dst := image.NewRGBA(b)
		draw.Draw(dst, b, image.White, image.Point{}, draw.Src)
		draw.Draw(dst, b, img, b.Min, draw.Over)
		return dst
	}
	return img
}

// toNRGBA copies any image into an NRGBA with origin (0, 0).
func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok && n.Bounds().Min == (image.Point{}) {
		return n
	}
	b := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func decodeImage(from string, in []byte) (image.Image, error) {
	dec, ok := decoders[from]
	if !ok {
		return nil, fmt.Errorf("%w: no decoder for %s", ErrUnsupported, from)
	}
	img, err := dec(bytes.NewReader(in))
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", from, err)
	}
	return img, nil
}

func imageConverter(from, to string) Func {
	enc := encoders[to]
	return func(in []byte, opts options.Options) ([]byte, error) {
		img, err := decodeImage(from, in)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := enc(&buf, img, opts); err != nil {
			return nil, fmt.Errorf("encode %s: %w", to, err)
		}
		return buf.Bytes(), nil
	}
}

// imageToPDF puts the picture on one page; a JPEG is embedded as is.
func imageToPDF(from string) Func {
	return func(in []byte, _ options.Options) ([]byte, error) {
		pic, err := PictureOf(from, in)
		if err != nil {
			return nil, err
		}
		return pdfw.Pictures([]*pdfw.Picture{pic}, "")
	}
}

// PictureOf prepares one image for a PDF page (shared with the merge path
// in the job layer).
func PictureOf(from string, in []byte) (*pdfw.Picture, error) {
	if from == "jpg" {
		if pic, err := pdfw.ParseJPEG(in); err == nil {
			return pic, nil
		}
	}
	img, err := decodeImage(from, in)
	if err != nil {
		return nil, err
	}
	return pdfw.FromImage(img), nil
}

// imageToSVG wraps the bitmap in an SVG document at its pixel size. It is
// not a trace: the picture stays a picture, but it can now be placed where
// only SVG is accepted.
func imageToSVG(from string) Func {
	return func(in []byte, opts options.Options) ([]byte, error) {
		img, err := decodeImage(from, in)
		if err != nil {
			return nil, err
		}
		b := img.Bounds()
		var payload []byte
		mime := "image/png"
		if from == "jpg" {
			payload = in
			mime = "image/jpeg"
		} else {
			var buf bytes.Buffer
			if err := encoders["png"](&buf, img, opts); err != nil {
				return nil, err
			}
			payload = buf.Bytes()
		}
		var out bytes.Buffer
		fmt.Fprintf(&out, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<svg xmlns=\"http://www.w3.org/2000/svg\" xmlns:xlink=\"http://www.w3.org/1999/xlink\" width=\"%d\" height=\"%d\" viewBox=\"0 0 %d %d\">\n<image width=\"%d\" height=\"%d\" xlink:href=\"data:%s;base64,", b.Dx(), b.Dy(), b.Dx(), b.Dy(), b.Dx(), b.Dy(), mime)
		enc := base64.NewEncoder(base64.StdEncoding, &out)
		enc.Write(payload)
		enc.Close()
		out.WriteString("\"/>\n</svg>\n")
		return out.Bytes(), nil
	}
}

// asciiRamp goes from dark to light.
const asciiRamp = "@%#*+=-:. "

// imageToText renders the picture as ASCII art, 100 columns wide, with
// the 2:1 aspect of monospace cells corrected.
func imageToText(from string) Func {
	return func(in []byte, _ options.Options) ([]byte, error) {
		img, err := decodeImage(from, in)
		if err != nil {
			return nil, err
		}
		b := img.Bounds()
		cols := 100
		if b.Dx() < cols {
			cols = b.Dx()
		}
		rows := b.Dy() * cols / b.Dx() / 2
		if rows < 1 {
			rows = 1
		}
		small := image.NewGray(image.Rect(0, 0, cols, rows))
		xdraw.ApproxBiLinear.Scale(small, small.Bounds(), flatten(img), b, xdraw.Src, nil)
		var out strings.Builder
		for y := 0; y < rows; y++ {
			for x := 0; x < cols; x++ {
				v := small.GrayAt(x, y).Y
				out.WriteByte(asciiRamp[int(v)*(len(asciiRamp)-1)/255])
			}
			out.WriteByte('\n')
		}
		return []byte(out.String()), nil
	}
}

// GoImages are the raster ids the sandbox decodes and encodes on its own.
var GoImages = []string{"png", "jpg", "gif", "bmp", "tiff", "webp", "qoi", "pnm", "tga", "ico"}

func init() {
	for _, from := range GoImages {
		for _, to := range GoImages {
			if from != to {
				register(from, to, imageConverter(from, to))
			}
		}
		register(from, "pdf", imageToPDF(from))
		register(from, "svg", imageToSVG(from))
		register(from, "txt", imageToText(from))
	}
}
