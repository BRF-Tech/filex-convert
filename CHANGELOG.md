# Changelog

All notable changes to filex-convert are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/). The version in `filex-app.json`
is the one a tag must match.

## [Unreleased]

## [0.1.0] — 2026-09-21

The first public release. Convert is an app plugin for filex 0.43.0 and
later: right-click files, choose a target format, get the converted files
next to the originals. 73 formats in nine categories, 1223 direct edges;
most of it runs in pure Go inside filex's WebAssembly sandbox, and the rest
through the server's own ffmpeg, ImageMagick, LibreOffice, Ghostscript,
poppler and rsvg. The full table, and the list of what is deliberately not
offered with the reason for each, is `MATRIX.md`.

### The Convert wizard

- **Four steps, one question each**: Format → Combine → Settings → Review,
  under a step strip, with one primary button plus Back on every step.
- **A read-only source asks where the result goes** (filex 0.43). On a
  read-only storage filex still offers *Convert…* (the manifest's output
  says `elsewhere`), and the wizard gains a **Where** step: a folder picker
  starting at the person's default (the first storage they may write), the
  chosen folder shown on the review, and the job answering
  `output: {mode: "folder", dir}` — filex checks the folder before the job
  is queued. A file that can be written beside is unchanged.
- **Format**: every reachable format is a button under its category's own
  heading (*Image*, *Document*, *Archive* …). Pressing one **selects** it —
  the step says "Chosen: PDF" and lights that one button — and **Next**
  moves on. An event carrying several new answers at once (a client that
  fills every group, not a person) changes nothing: the answer the step
  shows stays. The buttons hold only what every selected file can reach with
  the engines present; below them, an administrator reads *Not available on
  this server*: each other target with the engine it would need, by the
  engine's own name ("ImageMagick", "FFmpeg"). What the server lacks is the
  administrator's to fix, so nobody else is shown it — filex's rule for
  anything that depends on the server's setup.
- **The strip never changes under the pointer.** Which steps exist is
  decided by the selection (Combine for several files that can become one;
  Settings when any reachable route has something to set), never by the
  format pressed. A step the chosen conversion does not need stays on the
  strip, is walked past, and says so: *Settings (not needed)*.
- **Combine** asks *one file for all of them* or *one file for each* as two
  buttons: several images → one PDF, one slideshow video or one GIF; any
  files → one archive.
- **Settings** carries every knob the route honours on one step — quality,
  video quality (CRF), max height, preset, audio bitrate, resolution,
  pages, PDF/A, waveform or spectrogram, frame times, duration, icon
  sizes — with its default filled in. There is no "advanced" section.
- **Review** lists the files, the target, every answer in words and the
  route in words ("JPEG → PDF (built in)", "PNG → AVIF (ImageMagick)").
  Its **Convert** button is the only thing that queues a job.
- **English, Turkish, Spanish, German and French** throughout: every
  screen, every format name that is a description rather than a name
  ("Plain text (.txt)" is "Düz metin (.txt)", "Texto sin formato (.txt)",
  "Reiner Text (.txt)", "Texte brut (.txt)"), the knobs, the route line, the
  tray's progress line and the job's final message. One catalogue per
  language (`internal/i18n/catalogue/`), checked by a test for missing keys,
  empty values, mismatched placeholders and English left behind.
  ⚠ Spanish, German and French are AI-translated and have not been read by a
  native speaker yet; the manifest's description says so in each of the five
  languages, and so does the README's *Languages* section. English and
  Turkish are the source languages and carry no such label.

### Conversions

- **Documents without LibreOffice**: Markdown, HTML, plain text, Word
  (.docx), ODT, RTF, PowerPoint (.pptx) and EPUB → PDF, Word, EPUB,
  Markdown, HTML and plain text through one block model; the PDF writer
  embeds the Go fonts (Turkish letters render and copy out) and keeps text
  searchable. With LibreOffice, office documents convert with their layout
  and optional PDF/A.
- **A document keeps its language.** An EPUB, a Word file or an HTML page
  declares the language its source declares — a Word, OpenDocument or
  PowerPoint file's language settings (weighed by how much text each
  marking covers, so English Word with Turkish text is Turkish), RTF
  `\deflang`/`\lang`, an EPUB's `dc:language`, HTML `lang`, a Markdown front
  matter's `lang:` — else the language of the person who ran the
  conversion, else `und` (undetermined). Nothing is declared Turkish, or
  anything else, by default. A Markdown front matter is metadata: its
  `title:` names the document and it is no longer printed as text.
- **Images**: PNG, JPEG, GIF, BMP, TIFF, WebP (lossless), QOI, Netpbm,
  Targa and ICO between each other in pure Go; images → PDF one page per
  image with JPEGs embedded untouched; HEIC, AVIF, PSD, XCF, DDS and EXR
  through ImageMagick; SVG rendering through rsvg.
- **Data**: CSV, TSV, JSON, YAML, XML and TOML between each other; tables
  to Excel (.xlsx), Markdown and HTML; Excel and ODS back out.
- **Archives**: ZIP, TAR, tar.gz, tar.zst and tar.xz between each other,
  RAR and tar.bz2 read; any file can be packed into an archive.
- **Video and audio** (ffmpeg): every common container and codec, animated
  GIF/WebP/APNG, video frames, audio extraction, a waveform or spectrogram
  picture of a sound, still images and slideshows as video, subtitles.
- **PDF** (Ghostscript, poppler): compression presets, PDF/A, pages to
  images with a dpi and a page range, text extraction, PostScript/EPS.
- **Fonts**: TTF/OTF ↔ WOFF; **subtitles**: SRT ↔ WebVTT in pure Go.

### Jobs

- A selection is one job: one output per input, named `<stem>.<ext>` next
  to it (a taken name gets a suffix, nothing is overwritten), progress per
  file in the tray, and a final message that names each failed file and
  why. One failing file does not stop the others.
- When an engine step fails, the planner retries once over a different
  route (poppler → Ghostscript → ImageMagick for PDF pages, LibreOffice →
  pure Go for Word).

### For plugin authors

- `filex-app.json` is the one manifest: the module embeds a copy generated
  from it (`go generate`), so `describe` cannot disagree with the file an
  administrator installs.
- `scripts/build.sh` runs the whole test suite first and produces no module
  when it fails; `--stamp` writes the module's sha256 into the manifest.
- The test suite drives the real plugin assembly over filex's
  `pluginkit/plugintest` fake host: every wizard step against the
  renderer's rules, every language side by side, the grey list, a
  conversion end to end and golden screens.

[Unreleased]: https://github.com/BRF-Tech/filex-convert/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/BRF-Tech/filex-convert/releases/tag/v0.1.0
