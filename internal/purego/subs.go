package purego

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/brf-tech/filex-convert/internal/options"
)

// Subtitles: SubRip and WebVTT share the cue model (start, end, lines);
// they differ in the header, the millisecond separator and the tags
// allowed in text. ASS/SSA needs a style header and goes through ffmpeg.

type cue struct {
	start, end int // milliseconds
	text       string
}

var timeRe = regexp.MustCompile(`(\d+):(\d\d):(\d\d)[,.](\d{1,3})|(\d\d):(\d\d)[,.](\d{1,3})`)

func parseTime(s string) (int, bool) {
	m := timeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	atoi := func(x string) int { n, _ := strconv.Atoi(x); return n }
	if m[1] != "" {
		ms := m[4]
		for len(ms) < 3 {
			ms += "0"
		}
		return (atoi(m[1])*3600+atoi(m[2])*60+atoi(m[3]))*1000 + atoi(ms), true
	}
	ms := m[7]
	for len(ms) < 3 {
		ms += "0"
	}
	return (atoi(m[5])*60+atoi(m[6]))*1000 + atoi(ms), true
}

// parseCues reads SRT or VTT cue blocks: an optional identifier line, a
// "start --> end" line (with any trailing settings) and the text lines.
func parseCues(in []byte) ([]cue, error) {
	text := strings.ReplaceAll(string(in), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\ufeff")
	var cues []cue
	for _, block := range strings.Split(text, "\n\n") {
		lines := strings.Split(strings.Trim(block, "\n"), "\n")
		var i int
		for i = 0; i < len(lines); i++ {
			if strings.Contains(lines[i], "-->") {
				break
			}
		}
		if i >= len(lines) {
			continue
		}
		parts := strings.SplitN(lines[i], "-->", 2)
		start, ok1 := parseTime(parts[0])
		endField := strings.Fields(parts[1])
		if len(endField) == 0 {
			continue
		}
		end, ok2 := parseTime(endField[0])
		if !ok1 || !ok2 {
			continue
		}
		cues = append(cues, cue{start: start, end: end, text: strings.Join(lines[i+1:], "\n")})
	}
	if len(cues) == 0 {
		return nil, errors.New("subtitles: no cues found")
	}
	return cues, nil
}

func fmtTime(ms int, sep string) string {
	h := ms / 3600000
	m := ms / 60000 % 60
	s := ms / 1000 % 60
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", h, m, s, sep, ms%1000)
}

var vttTagRe = regexp.MustCompile(`</?(?:c|v|ruby|rt|lang)[^>]*>`)

func srtToVTT(in []byte, _ options.Options) ([]byte, error) {
	cues, err := parseCues(in)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for i, c := range cues {
		txt := strings.ReplaceAll(c.text, "&", "&amp;")
		txt = strings.ReplaceAll(txt, "<font", "<c")
		txt = strings.ReplaceAll(txt, "</font>", "</c>")
		b.WriteString(fmt.Sprintf("%d\n%s --> %s\n%s\n\n", i+1, fmtTime(c.start, "."), fmtTime(c.end, "."), txt))
	}
	return []byte(b.String()), nil
}

func vttToSRT(in []byte, _ options.Options) ([]byte, error) {
	cues, err := parseCues(in)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	for i, c := range cues {
		txt := vttTagRe.ReplaceAllString(c.text, "")
		txt = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&nbsp;", " ").Replace(txt)
		b.WriteString(fmt.Sprintf("%d\n%s --> %s\n%s\n\n", i+1, fmtTime(c.start, ","), fmtTime(c.end, ","), txt))
	}
	return []byte(b.String()), nil
}

// subsToText drops the timing and keeps the lines, one cue per paragraph.
func subsToText(in []byte, _ options.Options) ([]byte, error) {
	cues, err := parseCues(in)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	for _, c := range cues {
		txt := vttTagRe.ReplaceAllString(c.text, "")
		txt = regexp.MustCompile(`</?[bius]>`).ReplaceAllString(txt, "")
		b.WriteString(strings.TrimSpace(txt) + "\n")
	}
	return []byte(b.String()), nil
}

func init() {
	register("srt", "vtt", srtToVTT)
	register("vtt", "srt", vttToSRT)
	register("srt", "txt", subsToText)
	register("vtt", "txt", subsToText)
}
