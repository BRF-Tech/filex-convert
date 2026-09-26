# filex-convert

**Convert** is an [app plugin](https://github.com/BRF-Tech/filex/blob/main/docs/APP-PLUGINS.md)
for [filex](https://github.com/BRF-Tech/filex): right-click one or more files,
pick a target format, get the converted files next to the originals. It runs
as a WebAssembly module *inside* filex — no service to deploy, no port to
open — and converts *anything to anything* that makes sense: Markdown to
PDF, a Word file to EPUB, an MP3 to a JPEG of its spectrogram, a folder's
worth of photos into one PDF, a RAR into a tar.xz.

Most of it happens in pure Go inside the sandbox, so a filex host with no
engines at all still converts documents, images, e-books, data, archives,
subtitles and fonts. For video, audio, HEIC/AVIF, SVG rendering, PDF pages
and layout-faithful office exports it asks filex for its own engines
(ffmpeg, ImageMagick, LibreOffice, Ghostscript, poppler, rsvg) through one
host function under one permission each — and shows an administrator,
under the format buttons, which targets a missing engine would unlock.

It is also the reference example for the plugin kit: a manifest, one
action, one screen, the five exports, engine calls, and the tests that keep
them honest. If you are writing your own app, start from
[PLUGIN-KIT.md](https://github.com/BRF-Tech/filex/blob/main/docs/PLUGIN-KIT.md)
and read this repository alongside it.

## What it converts

73 formats in 9 categories, 1223 direct edges; a conversion may chain up to
three edges (`pptx → pdf → png`). `go` means the work happens inside the
sandbox with no engine; the others need that engine present on the filex
host and the matching `engines:<name>` permission granted at install. The
planner prefers the engine when it gives better fidelity (LibreOffice for
Word → PDF) and falls back to pure Go when it is absent. The full per-pair
table, plus the list of what is deliberately **not** offered and why, is
generated from the code into [MATRIX.md](MATRIX.md).

### Without any engine (pure Go)

| From | To | Notes |
|---|---|---|
| **Documents**: Markdown, HTML, plain text, Word (.docx), ODT, RTF, PowerPoint (.pptx), EPUB | PDF, Word (.docx), EPUB, Markdown, HTML, plain text | one block model (headings, paragraphs, lists, code, quotes, tables, links, bold/italic); the PDF writer embeds the Go fonts (Turkish letters included), text stays searchable; EPUB, Word and HTML declare the source's language (else the language of whoever converts, else `und`); a Markdown YAML **front matter is metadata, not content** — `title:` names the document, `lang:` sets its language, and the block is no longer printed as a rule and a run of text |
| **Images**: PNG, JPEG, GIF, BMP, TIFF, WebP, QOI, Netpbm, Targa, ICO | each other | WebP is written lossless; ICO with a favicon (16/32/48), application (16 … 256) or single size set |
| Images | PDF | one page per image, JPEGs embedded as they are (DCTDecode); several images → one PDF with *Combine* |
| Images | SVG, plain text | SVG wraps the bitmap (not a trace); text is ASCII art |
| **Data**: CSV, TSV, JSON, YAML, XML, TOML | each other | CSV/TSV ↔ JSON/YAML/XML/TOML through rows or the generic tree (XML attributes become `@name`) |
| CSV, TSV, JSON, YAML | Excel (.xlsx), Markdown table, HTML table | numbers stay numbers, `007` stays text; header row bold |
| Excel (.xlsx), ODS | CSV, TSV, JSON, YAML, Markdown, HTML | first sheet; shared and inline strings, booleans, cached formula values |
| CSV, Excel, ODS | PDF, Word, EPUB | via the Markdown table |
| **Archives**: ZIP, TAR, tar.gz, tar.zst, tar.xz, tar.bz2, RAR | ZIP, TAR, tar.gz, tar.zst, tar.xz | re-packed entry by entry; RAR and tar.bz2 are read only |
| **Any file** | ZIP, TAR, tar.gz, tar.zst, tar.xz | packed as the single member; several files → one archive with *Combine* |
| **Subtitles**: SRT, WebVTT | each other, plain text | |
| **Fonts**: TTF, OTF | WOFF (and back) | exact table repackaging; TTF → OTF relabels |

### With an engine

| From | To | Engine |
|---|---|---|
| MP4, MKV, WebM, MOV, AVI, FLV, WMV, MPEG, TS, 3GP, OGV | each other | ffmpeg (remux with *Copy streams*, or x264 / VP9 / WMV2 / MPEG-2 / Theora with CRF, preset, max height) |
| Video | animated GIF, WebP, APNG | ffmpeg (12 fps, palette, max height) |
| Video | PNG, JPEG frames | ffmpeg (one frame at a time, or one every *N* seconds, at most 64) |
| Video | MP3, WAV, FLAC, Ogg, AAC, M4A, Opus, WMA, AIFF, AC-3 | ffmpeg (audio track extracted) |
| Audio | each other | ffmpeg (bitrate for lossy targets) |
| Audio | PNG, JPEG | ffmpeg — a **waveform or spectrogram picture** (that is "mp3 → jpg") |
| Audio | MP4, WebM, MKV | ffmpeg — the waveform or spectrum drawn as a video with the sound |
| Still image | MP4, WebM, MKV, MOV | ffmpeg (plays for *Duration* seconds); several images → one slideshow or GIF with *Combine* |
| Animated GIF / WebP / APNG | MP4, WebM, MKV, MOV, each other | ffmpeg |
| SRT, WebVTT, ASS | each other | ffmpeg |
| HEIC, PSD, XCF, DDS, EXR, AVIF | PNG, JPEG, WebP, TIFF, BMP, GIF | ImageMagick |
| Rasters | AVIF, PSD, DDS, EXR | ImageMagick |
| SVG | PNG, PDF, PostScript, EPS | rsvg (ImageMagick for PNG/JPEG/WebP) |
| Word (.docx, .doc), ODT, RTF | PDF, DOCX, ODT, RTF, HTML, TXT, EPUB | LibreOffice (PDF/A optional) — layout-faithful; the sandbox does the same without it, text and tables only |
| HTML, TXT, EPUB | PDF, DOCX, ODT | LibreOffice |
| Excel (.xlsx), ODS, CSV | PDF, XLSX, ODS, CSV, HTML | LibreOffice |
| PowerPoint (.pptx), ODP | PDF, PPTX, ODP | LibreOffice |
| PDF | PDF | Ghostscript (compression presets *screen / ebook / printer / prepress*, PDF/A) → `name.compressed.pdf` / `name.pdfa.pdf` |
| PDF | PNG, JPEG, TIFF | poppler (dpi, page range; one file per page); Ghostscript and ImageMagick as fallbacks |
| PDF | TXT, SVG, EPS, PostScript | poppler (Ghostscript for TXT/EPS/PS) |
| PostScript, EPS | PDF, PNG, JPEG | Ghostscript |

Not offered, with reasons: PDF → Word (no engine path that keeps a document
a document), MOBI/AZW3, 7z, WOFF2, OCR, vector tracing, several-sheet
workbooks — see [MATRIX.md](MATRIX.md#not-offered).

## Install

It needs filex **0.43.0** or later (the release that ships app plugins).

**Admin → Plugins → Apps → Install → GitHub repository**, enter
`BRF-Tech/filex-convert` and a release tag (`v0.1.0`). filex fetches
`filex-app.json` from the repository at that tag, downloads `plugin.wasm`
from the matching GitHub release and refuses it unless its sha256 is the one
the manifest names. The install wizard then shows the permission review:

| Permission | Why the app asks for it |
|---|---|
| `files:read` | to read the files you selected |
| `files:write` | to write the converted file next to the original |
| `engines:ffmpeg` | video and audio, animated images, frames, waveform/spectrogram pictures, slideshows, ASS subtitles |
| `engines:imagemagick` | HEIC, AVIF, PSD, XCF, DDS, EXR |
| `engines:libreoffice` | layout-faithful office documents; EPUB from Word |
| `engines:ghostscript` | PDF compression, PDF/A, PostScript/EPS |
| `engines:poppler` | PDF pages to images, PDF text |
| `engines:rsvg` | SVG rendering |

filex grants the list exactly as declared. The app asks for nothing else: no
network (`http:*`), no mail, no notifications, no settings, no per-file
state. An engine that is not installed on the host is simply not used: the
format buttons hold what the present engines and the sandbox can reach, and
a list under them names every other target with the engine it needs. filex
looks for the engines once, when it starts: install one later and restart
filex for the converter (and the admin panel's *About* and *Apps* pages,
which read the same answer) to see it.

Two other install paths work too: **Files** (upload `plugin.wasm` and
`filex-app.json` from a release) and **URL** (the two release-asset URLs
plus the sha256 from `plugin.wasm.sha256`).

## Use

Right-click → **Convert…** opens a wizard with a step strip at the top.
Every step asks one thing and carries one button, plus **Back**. Nothing is
converted until you press **Convert** on the last step.

1. **Format** — what it should become. Every reachable format is a button,
   grouped under its category's own heading (*Image*, *Video*, *Document*,
   *Archive*, …), so all of them are readable without a click. Pressing one
   **selects** it — the step says *Chosen: PDF* and lights that button —
   and **Next** carries on. The buttons hold only formats *every* selected
   file can reach; with a PDF and a DOCX selected you get PNG, TXT, ZIP …
   but not DOCX. A same-format entry (PDF → PDF) is marked *re-encode*.
   Below the groups, an administrator reads **Not available on this
   server**: the targets a missing engine would unlock, each with the
   engine's name. Nobody else is shown what the server lacks — only the
   formats they can have.
2. **Combine** — on the strip when several files are selected and at least
   one target can hold them all (PDF, MP4/WebM/MKV or GIF for images, any
   archive for any files): *one file for all of them* or *one file for
   each*, as two buttons. Combined, the selection becomes one output named
   after the first file: pages of one PDF, frames of one slideshow (each
   shown for *Duration* seconds), members of one archive.
3. **Settings** — every knob the route honours, on one step: **Quality**
   (1–100), **Video quality (CRF)**, **Max height**, **Preset** (video speed
   or PDF compression), **Audio bitrate**, **Resolution (dpi)**, **Pages**
   (`3`, `2-5`, `2-`), **PDF/A**, **Picture** (waveform / spectrogram),
   **Frame at** and **A frame every** (seconds), **Duration** (seconds a
   still image plays), **Icon sizes**. There is no "advanced" section: a
   setting is on the step or it is not in the plugin.
4. **Where** — only when a selected file is on a read-only storage: the
   result cannot go beside it, so the step asks for a folder (filex's own
   folder picker), starting at the person's default — the first storage
   they may write. filex checks the folder before the job is queued (the
   person must be able to write there) and writes the result into it. The
   manifest says the result may go elsewhere (`output.elsewhere`), which is
   what makes filex offer **Convert…** on a read-only storage at all.
5. **Review** — the files, the target, the answers and the route the
   conversion will take ("JPEG → PDF (built in)", "PNG → AVIF
   (ImageMagick)"), in a table. **Convert** queues one job for the whole
   selection. The tray shows progress per file, and **Open** on the outputs
   once they land. One file failing does not stop the others; the job's
   message names each failure and its reason, in the reader's language.

Which steps are on the strip is decided by the selection, before anything
is pressed, and does not change while you pick: a step the chosen
conversion does not need (a JPEG → PDF has nothing to set) stays on the
strip, is walked past when you press Next, and is marked *(not needed)*.

Outputs are siblings of the input named `<stem>.<ext>` (`clip.mp4`,
`report.pdf`); a taken name gets a suffix, nothing is overwritten. PDF pages
and video frames come out as `<stem>-1.png`, `<stem>-2.png`, …; same-format
re-encodes as `<stem>.compressed.pdf` or `<stem>.pdfa.pdf`; a file packed
into an archive keeps its name as the member's name.

## Limits

| Limit | Value | Why |
|---|---|---|
| Pure-Go input | 64 MiB per file | the decoded image, document or unpacked archive lives in the sandbox's memory (256 MiB) next to the input; larger files still convert through an engine when one is present |
| Unpacked archive | 128 MiB | a "zip bomb" fails early instead of exhausting the sandbox |
| Engine input | filex's `FILEX_APP_PLUGIN_MAX_INPUT_MB` (256 MiB) | the host spools inputs for engines |
| Engine outputs | 64 files per run | the host's cap; frame extraction stops there |
| Job | 15 minutes | the action's `limits.timeout_s`; each engine step asks for 5–14 minutes of that |
| Route | 3 steps | anything longer is a conversion nobody asked for |
| Retry | once | when an engine step fails, the planner tries one alternative route that avoids the failed edge (poppler → Ghostscript → ImageMagick for PDF pages, LibreOffice → pure Go for Word) |
| Module | 14.5 MiB | the wasm; the build script warns above 8 MiB and fails above 16 MiB |

Document fidelity in the sandbox is "text, structure and tables": headings,
paragraphs, lists, code, quotes, tables, links, bold, italic, strike-through.
Images inside documents, footnotes, columns and page layout are LibreOffice's
job. PDF/A is best effort: LibreOffice exports PDF/A-2b directly; Ghostscript
converts to PDF/A-2 with an RGB output intent but without an embedded ICC
profile, which strict validators flag.

## How it works

```
filex-app.json ─ go generate ─▶ manifest.embed.json ─ go:embed ─▶ describe
                                                                       │
right-click ─▶ view_event (options) ─▶ internal/view ─ graph.Targets ─▶ form
                                                                       │ submit → job
action_run (convert) ─▶ internal/job ─ graph.Route ─▶ per input, per edge:
      pure Go   ─▶ purego.Convert  ─▶ file_create/file_write
      engine    ─▶ engines.Build   ─▶ engine_run {args, inputs{name: ref}} ─▶ artefact refs
      merge     ─▶ job.runMerged   ─▶ one PDF / archive (pure Go) or one ffmpeg run over N frames
```

- `internal/formats` — the catalogue (id, extensions, MIME, category, label).
- `internal/graph` — edges with engine, cost, lossy flag and option keys;
  Dijkstra bounded to three steps, lossy edges weighted ×1.4, `Terminal` and
  `Initial` flags so a route never passes *through* an animated GIF, a frame,
  an ASCII rendering or an archive. Pure-Go document edges that LibreOffice
  renders better cost 1.6, so the engine wins when present. Written from the
  specification; it shares no code with any other converter.
- `internal/doc` — the block model and its Markdown, HTML and plain-text
  readers and writers (goldmark for Markdown parsing; a small tokenizer for
  HTML).
- `internal/office` — DOCX (read and write), XLSX (read and write), EPUB
  (read and write), ODT, ODS and PPTX (read), RTF (read), all zip-of-XML or
  text parsing with `encoding/xml`.
- `internal/pdfw` — the PDF writer: A4 layout of blocks with word wrapping,
  tables as grids, code with a background, link annotations; the four Go
  fonts (regular, bold, italic, mono) embedded as CIDFontType2 with
  Identity-H and a ToUnicode map, so ı İ ş Ş ğ Ğ ü Ü ö Ö ç Ç render and copy
  out; images as pages with JPEG passthrough.
- `internal/purego` — the in-sandbox converters (Go image codecs,
  `golang.org/x/image`, nativewebp, own QOI/Netpbm/Targa/ICO codecs,
  `archive/*`, gzip, zstd, xz, bzip2, rardecode, `encoding/csv|json|xml`,
  yaml.v3, BurntSushi/toml, the doc/office/pdfw packages, WOFF, SRT/VTT).
- `internal/engines` — argument builders for the six engines with golden
  tests. Every argument is a bare token (no path separators, no `..`, no
  `@list`, no `file:`/`http:` schemes, none of the flags that would open the
  sandbox); inputs and outputs are just names inside the engine's private
  run directory. A `/` cannot be spelled, so filter rates are decimals
  (`fps=0.2`), even-size scaling is `trunc(iw*0.5)*2`, and Ghostscript's
  presets are written out parameter by parameter.
- `internal/job` — runs one input through its route step by step (a pure-Go
  step reads the previous ref and writes a new output; an engine step hands
  the previous ref to the host and takes the artefact back), names the
  results, reports progress, collects per-file failures, retries once; the
  merge path handles the whole selection at once.
- `internal/view` — the Convert wizard: pure, no host calls, so it is unit
  tested like the graph.
- `cmd/plugin` — `pluginkit.Run` from `init()` (a `c-shared` wasip1 module is
  a reactor; `main` never runs).

## Development

Stock Go 1.25, no TinyGo.

```bash
go test ./...                 # host-side tests: catalogue, routing, pure-Go round trips (images,
                              # documents, data, archives, fonts, subtitles), PDF writer (Turkish
                              # text, font embedding, pagination), engine argv goldens, job runs
                              # against a fake host (including merge), screens
bash scripts/build.sh         # runs `go test ./...` FIRST, then
                              # GOOS=wasip1 GOARCH=wasm … -buildmode=c-shared → plugin.wasm
                              # prints the sha256 and enforces the size gate (8 MiB soft, 16 MiB hard)
                              # a failing test produces NO module (SKIP_TESTS=1 only for bisecting)
go test ./... -update         # rewrite the golden screens under internal/app/testdata/golden
go generate ./...             # refresh manifest.embed.json after editing filex-app.json
go run ./tools/matrix > MATRIX.md
go run ./tools/render md pdf in.md out.pdf   # run one pure-Go conversion on the host to eyeball it
bash scripts/size-probe.sh    # rough wasm cost per dependency (how 7z got refused)
```

### The test kit: what a screen is measured against

`internal/app` assembles the plugin over a host it is handed, so the test
suite drives the **same** assembly the wasm module registers — only with
filex faked in memory
([`pluginkit/plugintest`](https://github.com/BRF-Tech/filex/blob/main/docs/PLUGIN-KIT.md#testing-with-plugintest)).
The fake host holds the manifest's permissions as the administrator's
grants and enforces the job-versus-screen rules, so a test cannot pass by
doing something a real instance would refuse.

### Languages

The converter speaks **English, Turkish, Spanish, German and French**. Every
word it shows — the wizard, format and category names, the knobs, the route
line, the job's messages and its errors — is a key in
`internal/i18n/catalogue/<lang>.json`; the manifest's texts are in
`filex-app.json`. `internal/i18n/i18n_test.go` refuses a catalogue with a
missing key, an empty value, a placeholder spelled differently from the
English one, English left in another language, a key the code asks for that
is not there, or a key nothing asks for. Terminology and register follow the
filex language packs (*usted*, *Sie*, *vous*); names of formats (PDF, JPEG,
MP4) are never translated.

> **Spanish, German and French are AI-translated, awaiting review by a native
> speaker — corrections welcome.** English and Turkish are the source
> languages, written in the code. Nothing the converter says carries legal
> weight, but a wrong word is still a wrong word: open an issue.

filex 0.43.0 — which this app requires — hands an app the reader's own
language, a language pack's included, so Spanish, German and French readers
get the translations. (Hosts that narrowed every locale to English or Turkish
predate app plugins and cannot install this.)

⚠ **No word on a screen is picked for the call's language.** The language a
call carries is filex's *guess* at the reader's — the account's saved
language before the window's own — and an embedded filex (the web component
in a host page) is where the guess is wrong: a Turkish window on an account
saved as English. So every word the wizard shows travels as a Text in all
five languages — a form field's label, help and placeholder and each
button's label too (`wire.Field.I18n`, `wire.FieldOption.LabelI18n`) — and
the reader's own filex picks one. The one string that cannot be a Text, the
tray's progress line, names formats by names that read the same everywhere
("notes.md → TXT"). A failure's technical detail goes to the plugin's log,
not to the person.

`internal/app/app_test.go` measures, on every `go test`:

- **the manifest** filex would install: the closed permission set, the
  action's view, the output mode, the five declared languages;
- **every step of the wizard** as the renderer will draw it: known node
  types, a step spine with exactly one step active, one primary button and
  one form per step, and a `select` whose options are readable (filex draws
  a select as a row of buttons, never a dropdown) — so the category groups
  are literally the buttons a person sees;
- **the grey list**: a format that needs an engine this server does not
  have is not a button — for an administrator it is a row under the groups
  naming the engine, for everybody else it is not there at all, and it
  becomes a button again once the engine is installed;
- **every language**: every `Text` carries `en`, `tr`, `es`, `de` and `fr`,
  and the screen drawn once per language has the same shape, with the group
  headings, the knob labels and the buttons actually translated;
- **the reader's language, not the call's**: every step drawn with the call
  in English and in Turkish, read as a Turkish and as an English reader —
  nothing may read the same to both unless it is a name or a number
  (`reader_language_test.go`, here and in `internal/job`);
- **a conversion end to end**: the wizard walked step by step → the queued
  job → a JPEG in the fake host's files, with no engine asked for on a
  pure-Go route;
- **golden screens** (`internal/app/testdata/golden/`): the stored shape of
  the format, settings and review steps, so an accidental redesign is a
  diff.

### Building it yourself: where the SDK comes from

The plugin imports filex's guest SDK,
`github.com/brf-tech/filex/backend/pkg/pluginkit` (and `…/pluginkit/wire`),
which ships with filex 0.43.0. `go.mod` pins the published module:

```
require github.com/brf-tech/filex/backend v0.43.0
```

so a fresh clone builds on its own — `go build` fetches it from the Go
module proxy. `plugin.wasm`'s sha256 depends on the exact SDK version it
was compiled against (Go records it in the module), so moving to a newer
filex means `go get github.com/brf-tech/filex/backend@<tag>` and a new
stamp (see *Releasing*).

### Testing against filex itself

The host-side tests never call a host function directly (they answer
`ErrNotWasm` off-wasm); everything goes through the `Host` interface, which
`plugintest` implements in memory. To exercise the
real thing, install the built module into a filex instance (**Files**
install) and convert a few files; the plugin's log in the admin drawer
shows every route taken and every engine exit code. filex's
`scripts/smoke-app-plugin.sh` does the install → convert → inspect → remove
cycle from the command line.

### Releasing

1. Bump `version` in `filex-app.json` and write the CHANGELOG entry (with
   the date of the tag).
2. `bash scripts/release-prepare.sh` — regenerates the embedded manifest
   (so `describe` reports the new version; filex refuses a module whose
   `describe` disagrees with its manifest), builds `plugin.wasm`
   reproducibly **against the same SDK the release workflow will use**,
   writes its sha256 into `filex-app.json`, regenerates `MATRIX.md`, runs
   the tests.
3. Commit, tag `vX.Y.Z`, push the tag.

The release workflow rebuilds the module with the pinned Go version, refuses
to publish unless its hash equals the committed one (the module's bytes do
not depend on the manifest's `wasm` block, which is why the hash can live in
the same commit as the tag), and attaches `plugin.wasm`,
`plugin.wasm.sha256`, `filex-app.json` and `MATRIX.md` to the GitHub release.

## License

MIT — see [LICENSE](LICENSE). Copyright (c) 2026 BRF Teknoloji.

The conversion graph and every converter here were written from the
plugin's own specification and the public format specifications (PDF 1.7,
ECMA-376, OASIS ODF, EPUB 3, WOFF 1.0, QOI, Netpbm, Targa, ICO, RTF 1.9);
nothing was ported from other converters.
