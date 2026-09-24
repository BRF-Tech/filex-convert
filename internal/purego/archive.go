package purego

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/brf-tech/filex-convert/internal/options"
)

// Archives are re-packed entry by entry: names, modes, modification times
// and directory entries survive; symlinks, hard links and other special
// entries are skipped (an archive is never a way to plant a link on the
// server). Entry names are normalised to forward slashes with no leading
// slash and no ".." element.

// entry is one member of an archive in memory.
type entry struct {
	Name    string
	Dir     bool
	Mode    int64
	ModTime time.Time
	Data    []byte
}

// ErrArchiveEntries is answered when an archive has too many members.
var ErrArchiveEntries = errors.New("archive has too many entries")

// maxEntries bounds the member count of one archive.
const maxEntries = 100000

// maxUnpacked bounds the unpacked bytes an archive may hold in memory (a
// "zip bomb" must fail early, not exhaust the sandbox).
const maxUnpacked = 2 * MaxInput

func cleanName(name string, dir bool) (string, bool) {
	name = strings.ReplaceAll(name, `\`, "/")
	name = strings.TrimLeft(name, "/")
	if name == "" {
		return "", false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return "", false
		}
	}
	name = path.Clean(name)
	if name == "." {
		return "", false
	}
	if dir {
		name += "/"
	}
	return name, true
}

// ── readers ──

func readZip(in []byte) ([]entry, error) {
	zr, err := zip.NewReader(bytes.NewReader(in), int64(len(in)))
	if err != nil {
		return nil, fmt.Errorf("zip: %w", err)
	}
	if len(zr.File) > maxEntries {
		return nil, ErrArchiveEntries
	}
	var out []entry
	total := 0
	for _, f := range zr.File {
		mode := f.Mode()
		if mode&(fs.ModeSymlink|fs.ModeDevice|fs.ModeNamedPipe|fs.ModeSocket) != 0 {
			continue
		}
		dir := f.FileInfo().IsDir()
		name, ok := cleanName(f.Name, dir)
		if !ok {
			continue
		}
		e := entry{Name: name, Dir: dir, Mode: int64(mode.Perm()), ModTime: f.Modified}
		if !dir {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("zip %s: %w", f.Name, err)
			}
			e.Data, err = io.ReadAll(io.LimitReader(rc, int64(maxUnpacked-total)+1))
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("zip %s: %w", f.Name, err)
			}
			total += len(e.Data)
			if total > maxUnpacked {
				return nil, ErrTooLarge
			}
		}
		out = append(out, e)
	}
	return out, nil
}

func readTar(r io.Reader) ([]entry, error) {
	tr := tar.NewReader(r)
	var out []entry
	total := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if len(out) >= maxEntries {
			return nil, ErrArchiveEntries
		}
		switch h.Typeflag {
		case tar.TypeReg, tar.TypeDir:
		default:
			continue
		}
		dir := h.Typeflag == tar.TypeDir
		name, ok := cleanName(h.Name, dir)
		if !ok {
			continue
		}
		e := entry{Name: name, Dir: dir, Mode: h.Mode & 0o777, ModTime: h.ModTime}
		if !dir {
			e.Data, err = io.ReadAll(io.LimitReader(tr, int64(maxUnpacked-total)+1))
			if err != nil {
				return nil, fmt.Errorf("tar %s: %w", h.Name, err)
			}
			total += len(e.Data)
			if total > maxUnpacked {
				return nil, ErrTooLarge
			}
		}
		out = append(out, e)
	}
	return out, nil
}

func readTgz(in []byte) ([]entry, error) {
	gr, err := gzip.NewReader(bytes.NewReader(in))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()
	return readTar(gr)
}

func readTzst(in []byte) ([]entry, error) {
	zr, err := zstd.NewReader(bytes.NewReader(in))
	if err != nil {
		return nil, fmt.Errorf("zstd: %w", err)
	}
	defer zr.Close()
	return readTar(zr)
}

// ── writers ──

func writeZip(entries []entry) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.Name, Method: zip.Deflate, Modified: e.ModTime}
		if e.Dir {
			h.Method = zip.Store
			h.SetMode(fs.ModeDir | fs.FileMode(e.Mode))
		} else {
			h.SetMode(fs.FileMode(e.Mode))
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return nil, fmt.Errorf("zip %s: %w", e.Name, err)
		}
		if !e.Dir {
			if _, err := w.Write(e.Data); err != nil {
				return nil, fmt.Errorf("zip %s: %w", e.Name, err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeTar(w io.Writer, entries []entry) error {
	tw := tar.NewWriter(w)
	for _, e := range entries {
		h := &tar.Header{Name: e.Name, Mode: e.Mode, ModTime: e.ModTime, Format: tar.FormatPAX}
		if e.Dir {
			h.Typeflag = tar.TypeDir
			if h.Mode == 0 {
				h.Mode = 0o755
			}
		} else {
			h.Typeflag = tar.TypeReg
			h.Size = int64(len(e.Data))
			if h.Mode == 0 {
				h.Mode = 0o644
			}
		}
		if err := tw.WriteHeader(h); err != nil {
			return fmt.Errorf("tar %s: %w", e.Name, err)
		}
		if !e.Dir {
			if _, err := tw.Write(e.Data); err != nil {
				return fmt.Errorf("tar %s: %w", e.Name, err)
			}
		}
	}
	return tw.Close()
}

func writeTarBytes(entries []entry) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeTar(&buf, entries); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeTgz(entries []entry) ([]byte, error) {
	var buf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if err := writeTar(gw, entries); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeTzst(entries []entry) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
	if err != nil {
		return nil, err
	}
	if err := writeTar(zw, entries); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var archiveReaders = map[string]func([]byte) ([]entry, error){
	"zip":  readZip,
	"tar":  func(in []byte) ([]entry, error) { return readTar(bytes.NewReader(in)) },
	"tgz":  readTgz,
	"tzst": readTzst,
	"txz":  readTxz,
	"tbz2": readTbz2,
	"rar":  readRar,
}

func archiveConverter(rd func([]byte) ([]entry, error), wr func([]entry) ([]byte, error)) Func {
	return func(in []byte, _ options.Options) ([]byte, error) {
		entries, err := rd(in)
		if err != nil {
			return nil, err
		}
		return wr(entries)
	}
}
