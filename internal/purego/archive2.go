package purego

import (
	"bytes"
	"compress/bzip2"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/nwaples/rardecode/v2"
	"github.com/ulikunitz/xz"

	"github.com/brf-tech/filex-convert/internal/formats"
	"github.com/brf-tech/filex-convert/internal/options"
)

// More archive families: TAR + xz (read and write), TAR + bzip2 (read; Go
// has no bzip2 compressor) and RAR (read only — RAR's compressor is
// proprietary). "Pack" wraps any single file into an archive.

// SourceName is the params key the job layer sets to the input's file
// name, so converters that need it (packing a file into an archive) can
// name the member after it.
const SourceName = "source_name"

// JobLocale is the params key the job layer sets to the locale of the
// person who ran the conversion. A document writer that must name a
// language uses it when the source declares none (doc/lang.go).
const JobLocale = "job_locale"

func readTxz(in []byte) ([]entry, error) {
	xr, err := xz.NewReader(bytes.NewReader(in))
	if err != nil {
		return nil, fmt.Errorf("xz: %w", err)
	}
	return readTar(xr)
}

func readTbz2(in []byte) ([]entry, error) {
	return readTar(bzip2.NewReader(bytes.NewReader(in)))
}

func readRar(in []byte) ([]entry, error) {
	rr, err := rardecode.NewReader(bytes.NewReader(in))
	if err != nil {
		return nil, fmt.Errorf("rar: %w", err)
	}
	var out []entry
	total := 0
	for {
		h, err := rr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("rar: %w", err)
		}
		if h.Encrypted || h.HeaderEncrypted {
			return nil, errors.New("rar: archive is encrypted")
		}
		if len(out) >= maxEntries {
			return nil, ErrArchiveEntries
		}
		if h.LinkType != 0 {
			continue
		}
		name, ok := cleanName(h.Name, h.IsDir)
		if !ok {
			continue
		}
		e := entry{Name: name, Dir: h.IsDir, Mode: int64(h.Mode().Perm()), ModTime: h.ModificationTime}
		if !h.IsDir {
			e.Data, err = io.ReadAll(io.LimitReader(rr, int64(maxUnpacked-total)+1))
			if err != nil {
				return nil, fmt.Errorf("rar %s: %w", h.Name, err)
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

func writeTxz(entries []entry) ([]byte, error) {
	var buf bytes.Buffer
	xw, err := xz.NewWriter(&buf)
	if err != nil {
		return nil, err
	}
	if err := writeTar(xw, entries); err != nil {
		return nil, err
	}
	if err := xw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// packOne makes a single-member archive out of any file, named after the
// source (params.source_name) or "file" when the name is unknown.
func packOne(to string) Func {
	write := archiveWriters[to]
	return func(in []byte, opts options.Options) ([]byte, error) {
		name := path.Base(strings.ReplaceAll(opts.String(SourceName), `\`, "/"))
		if name == "" || name == "." || name == "/" {
			name = "file"
		}
		return write([]entry{{Name: name, Mode: 0o644, ModTime: time.Now(), Data: in}})
	}
}

// PackEntries makes a multi-member archive (the job layer's merge path).
func PackEntries(to string, names []string, datas [][]byte) ([]byte, error) {
	write, ok := archiveWriters[to]
	if !ok {
		return nil, fmt.Errorf("%w: pack into %s", ErrUnsupported, to)
	}
	seen := map[string]int{}
	var entries []entry
	for i, n := range names {
		name := path.Base(strings.ReplaceAll(n, `\`, "/"))
		if name == "" || name == "." {
			name = "file"
		}
		if k := seen[name]; k > 0 {
			ext := path.Ext(name)
			name = strings.TrimSuffix(name, ext) + fmt.Sprintf("-%d", k+1) + ext
		}
		seen[path.Base(n)]++
		entries = append(entries, entry{Name: name, Mode: 0o644, ModTime: time.Now(), Data: datas[i]})
	}
	return write(entries)
}

// ArchiveTargets lists the archive ids the sandbox can write.
var ArchiveTargets = []string{"zip", "tar", "tgz", "tzst", "txz"}

var archiveWriters = map[string]func([]entry) ([]byte, error){
	"zip":  writeZip,
	"tar":  writeTarBytes,
	"tgz":  writeTgz,
	"tzst": writeTzst,
	"txz":  writeTxz,
}

func init() {
	for from, read := range archiveReaders {
		for to, write := range archiveWriters {
			if from == to {
				continue
			}
			register(from, to, archiveConverter(read, write))
		}
	}
	// pack: every non-archive format into every writable archive
	for _, f := range formats.All {
		if f.Category == formats.Archive {
			continue
		}
		for _, to := range ArchiveTargets {
			register(f.ID, to, packOne(to))
		}
	}
}
