package purego

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/brf-tech/filex-convert/internal/options"
)

// Fonts. An sfnt file (TrueType .ttf or OpenType .otf) is a table
// directory plus tables; WOFF 1.0 is the same tables, each zlib-compressed,
// behind a different directory. So ttf/otf → woff and woff → ttf/otf are
// exact, lossless repackagings written from the WOFF 1.0 specification.
// WOFF2 (Brotli plus a transformed glyf table) is not offered.

const woffSignature = 0x774F4646 // "wOFF"

// sfntTables parses an sfnt directory and answers flavor + tables in
// directory order.
type sfntTable struct {
	tag  [4]byte
	data []byte
	orig uint32 // original checksum, kept as given
}

func parseSFNT(data []byte) (flavor uint32, tables []sfntTable, err error) {
	if len(data) < 12 {
		return 0, nil, errors.New("font: too short")
	}
	flavor = binary.BigEndian.Uint32(data[0:4])
	switch flavor {
	case 0x00010000, 0x74727565, 0x4F54544F: // 1.0, 'true', 'OTTO'
	default:
		return 0, nil, errors.New("font: not a TrueType or OpenType font")
	}
	n := int(binary.BigEndian.Uint16(data[4:6]))
	if n == 0 || n > 512 || 12+n*16 > len(data) {
		return 0, nil, errors.New("font: bad table count")
	}
	for i := 0; i < n; i++ {
		rec := data[12+i*16:]
		var t sfntTable
		copy(t.tag[:], rec[0:4])
		t.orig = binary.BigEndian.Uint32(rec[4:8])
		off := int(binary.BigEndian.Uint32(rec[8:12]))
		length := int(binary.BigEndian.Uint32(rec[12:16]))
		if off < 0 || length < 0 || off+length > len(data) {
			return 0, nil, fmt.Errorf("font: table %q out of range", t.tag[:])
		}
		t.data = data[off : off+length]
		tables = append(tables, t)
	}
	return flavor, tables, nil
}

func pad4(n int) int { return (n + 3) &^ 3 }

// writeSFNT packs tables back into a TrueType/OpenType file.
func writeSFNT(flavor uint32, tables []sfntTable) []byte {
	n := len(tables)
	// binary search fields per the spec
	entrySelector := 0
	for (1 << (entrySelector + 1)) <= n {
		entrySelector++
	}
	searchRange := (1 << entrySelector) * 16
	rangeShift := n*16 - searchRange
	var out bytes.Buffer
	out.Write(binary.BigEndian.AppendUint32(nil, flavor))
	out.Write(binary.BigEndian.AppendUint16(nil, uint16(n)))
	out.Write(binary.BigEndian.AppendUint16(nil, uint16(searchRange)))
	out.Write(binary.BigEndian.AppendUint16(nil, uint16(entrySelector)))
	out.Write(binary.BigEndian.AppendUint16(nil, uint16(rangeShift)))
	off := 12 + n*16
	for _, t := range tables {
		out.Write(t.tag[:])
		out.Write(binary.BigEndian.AppendUint32(nil, t.orig))
		out.Write(binary.BigEndian.AppendUint32(nil, uint32(off)))
		out.Write(binary.BigEndian.AppendUint32(nil, uint32(len(t.data))))
		off += pad4(len(t.data))
	}
	for _, t := range tables {
		out.Write(t.data)
		out.Write(make([]byte, pad4(len(t.data))-len(t.data)))
	}
	return out.Bytes()
}

func sfntToWOFF(in []byte, _ options.Options) ([]byte, error) {
	flavor, tables, err := parseSFNT(in)
	if err != nil {
		return nil, err
	}
	type packed struct {
		t    sfntTable
		comp []byte
	}
	var packs []packed
	total := 44 + 20*len(tables)
	for _, t := range tables {
		var zb bytes.Buffer
		zw, _ := zlib.NewWriterLevel(&zb, zlib.BestCompression)
		zw.Write(t.data)
		zw.Close()
		comp := zb.Bytes()
		if len(comp) >= len(t.data) {
			comp = t.data
		}
		packs = append(packs, packed{t, comp})
		total += pad4(len(comp))
	}
	origLen := 12 + 16*len(tables)
	for _, t := range tables {
		origLen += pad4(len(t.data))
	}
	var out bytes.Buffer
	out.Write(binary.BigEndian.AppendUint32(nil, woffSignature))
	out.Write(binary.BigEndian.AppendUint32(nil, flavor))
	out.Write(binary.BigEndian.AppendUint32(nil, uint32(total)))
	out.Write(binary.BigEndian.AppendUint16(nil, uint16(len(tables))))
	out.Write([]byte{0, 0})
	out.Write(binary.BigEndian.AppendUint32(nil, uint32(origLen)))
	out.Write([]byte{0, 1, 0, 0}) // major 1, minor 0
	out.Write(make([]byte, 20))   // no metadata, no private block
	off := 44 + 20*len(tables)
	for _, p := range packs {
		out.Write(p.t.tag[:])
		out.Write(binary.BigEndian.AppendUint32(nil, uint32(off)))
		out.Write(binary.BigEndian.AppendUint32(nil, uint32(len(p.comp))))
		out.Write(binary.BigEndian.AppendUint32(nil, uint32(len(p.t.data))))
		out.Write(binary.BigEndian.AppendUint32(nil, p.t.orig))
		off += pad4(len(p.comp))
	}
	for _, p := range packs {
		out.Write(p.comp)
		out.Write(make([]byte, pad4(len(p.comp))-len(p.comp)))
	}
	return out.Bytes(), nil
}

func woffToSFNT(in []byte, _ options.Options) ([]byte, error) {
	if len(in) < 44 || binary.BigEndian.Uint32(in[0:4]) != woffSignature {
		return nil, errors.New("font: not a WOFF 1.0 file (WOFF2 is not supported)")
	}
	flavor := binary.BigEndian.Uint32(in[4:8])
	n := int(binary.BigEndian.Uint16(in[12:14]))
	if n == 0 || n > 512 || 44+n*20 > len(in) {
		return nil, errors.New("font: bad WOFF table count")
	}
	var tables []sfntTable
	for i := 0; i < n; i++ {
		rec := in[44+i*20:]
		var t sfntTable
		copy(t.tag[:], rec[0:4])
		off := int(binary.BigEndian.Uint32(rec[4:8]))
		compLen := int(binary.BigEndian.Uint32(rec[8:12]))
		origLen := int(binary.BigEndian.Uint32(rec[12:16]))
		t.orig = binary.BigEndian.Uint32(rec[16:20])
		if off < 0 || compLen < 0 || off+compLen > len(in) || origLen > 64<<20 {
			return nil, fmt.Errorf("font: table %q out of range", t.tag[:])
		}
		raw := in[off : off+compLen]
		if compLen < origLen {
			zr, err := zlib.NewReader(bytes.NewReader(raw))
			if err != nil {
				return nil, fmt.Errorf("font: table %q: %w", t.tag[:], err)
			}
			t.data, err = io.ReadAll(io.LimitReader(zr, int64(origLen)+1))
			zr.Close()
			if err != nil || len(t.data) != origLen {
				return nil, fmt.Errorf("font: table %q: bad inflate", t.tag[:])
			}
		} else {
			t.data = raw
		}
		tables = append(tables, t)
	}
	return writeSFNT(flavor, tables), nil
}

// ttfToOTF relabels a TrueType-flavoured font as OpenType: an OpenType file
// may carry glyf outlines, so the bytes are valid as they are once they
// parse. The reverse (CFF outlines under .ttf) is not offered.
func ttfToOTF(in []byte, _ options.Options) ([]byte, error) {
	if _, _, err := parseSFNT(in); err != nil {
		return nil, err
	}
	return append([]byte(nil), in...), nil
}

func init() {
	register("ttf", "woff", sfntToWOFF)
	register("otf", "woff", sfntToWOFF)
	register("woff", "ttf", woffToSFNT)
	register("woff", "otf", woffToSFNT)
	register("ttf", "otf", ttfToOTF)
}
