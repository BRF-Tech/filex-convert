package purego

import (
	"bytes"
	"fmt"
	htmlpkg "html"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"

	"github.com/brf-tech/filex-convert/internal/doc"
	"github.com/brf-tech/filex-convert/internal/options"
)

// Text formats. Markdown renders through goldmark (CommonMark + GFM tables,
// strikethrough, task lists) into a small standalone HTML document; plain
// text becomes paragraphs; HTML is reduced to text by dropping tags and
// script/style bodies. txt ↔ md is a copy: plain text is valid Markdown
// and a Markdown file read as text is what it is.

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(html.WithHardWraps()),
)

const htmlHead = "<!DOCTYPE html>\n<html%s>\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n<title>%s</title>\n<style>body{max-width:48rem;margin:2rem auto;padding:0 1rem;font:16px/1.6 system-ui,sans-serif;color:#222}pre{overflow:auto;padding:.75rem;background:#f4f4f4}code{font-family:ui-monospace,monospace}table{border-collapse:collapse}td,th{border:1px solid #ccc;padding:.25rem .5rem}img{max-width:100%%}</style>\n</head>\n<body>\n"
const htmlTail = "</body>\n</html>\n"

// wrapDocument makes a standalone page; lang is "" when nobody knows it.
func wrapDocument(title, lang string, body []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf(htmlHead, doc.HTMLLangAttr(lang), htmlEscape(title)))
	buf.Write(body)
	buf.WriteString(htmlTail)
	return buf.Bytes()
}

func htmlEscape(s string) string { return htmlpkg.EscapeString(s) }

// firstHeading picks a title for the document: the first Markdown heading
// or the first non-empty line, clipped.
func firstHeading(src []byte, markdown bool) string {
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if markdown && strings.HasPrefix(line, "#") {
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
		if len(line) > 80 {
			line = line[:80]
		}
		return line
	}
	return "Document"
}

// mdToHTML renders through goldmark. A YAML front matter is metadata
// (doc.SplitFrontMatter), the same as on the block-model path: its title
// and language go in the head, not in the body.
func mdToHTML(in []byte, opts options.Options) ([]byte, error) {
	fm, src := doc.SplitFrontMatter(in)
	var body bytes.Buffer
	if err := md.Convert(src, &body); err != nil {
		return nil, err
	}
	title := fm.Title
	if title == "" {
		title = firstHeading(src, true)
	}
	return wrapDocument(title, doc.FirstLang(fm.DeclaredLang(), opts.String(JobLocale)), body.Bytes()), nil
}

func txtToHTML(in []byte, opts options.Options) ([]byte, error) {
	text := strings.ReplaceAll(string(in), "\r\n", "\n")
	var body bytes.Buffer
	for _, para := range strings.Split(text, "\n\n") {
		para = strings.TrimRight(para, "\n")
		if strings.TrimSpace(para) == "" {
			continue
		}
		body.WriteString("<p>")
		body.WriteString(strings.ReplaceAll(htmlEscape(para), "\n", "<br>\n"))
		body.WriteString("</p>\n")
	}
	return wrapDocument(firstHeading(in, false), doc.NormalizeLang(opts.String(JobLocale)), body.Bytes()), nil
}

// htmlToText drops tags. Block-level elements become line breaks, script
// and style bodies vanish, entities are decoded, runs of blank lines are
// collapsed.
func htmlToText(in []byte, _ options.Options) ([]byte, error) {
	s := string(in)
	var out strings.Builder
	i := 0
	skipUntil := ""
	for i < len(s) {
		if skipUntil != "" {
			j := strings.Index(strings.ToLower(s[i:]), skipUntil)
			if j < 0 {
				break
			}
			i += j + len(skipUntil)
			if gt := strings.IndexByte(s[i:], '>'); gt >= 0 {
				i += gt + 1
			} else {
				i = len(s)
			}
			skipUntil = ""
			continue
		}
		c := s[i]
		if c != '<' {
			out.WriteByte(c)
			i++
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			break
		}
		tag := s[i+1 : i+end]
		i += end + 1
		if strings.HasPrefix(tag, "!--") {
			if k := strings.Index(s[i-end-1:], "-->"); k >= 0 {
				i = i - end - 1 + k + 3
			}
			continue
		}
		name := strings.ToLower(strings.TrimLeft(tag, "/"))
		if k := strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || r == '/' }); k >= 0 {
			name = name[:k]
		}
		closing := strings.HasPrefix(tag, "/")
		switch name {
		case "script", "style", "head", "noscript", "template":
			if !closing {
				skipUntil = "</" + name
			}
		case "br":
			out.WriteByte('\n')
		case "p", "h1", "h2", "h3", "h4", "h5", "h6", "pre", "blockquote", "table", "ul", "ol", "hr", "title":
			out.WriteString("\n\n")
		case "div", "section", "article", "header", "footer", "li", "tr", "dt", "dd":
			if !closing {
				out.WriteByte('\n')
			}
		case "td", "th":
			if closing {
				out.WriteByte('\t')
			}
		}
	}
	text := htmlpkg.UnescapeString(out.String())
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\u00a0", " ")
	var lines []string
	blank := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			blank++
			if blank > 1 {
				continue
			}
			line = ""
		} else {
			blank = 0
		}
		lines = append(lines, line)
	}
	res := strings.TrimSpace(strings.Join(lines, "\n"))
	if res != "" {
		res += "\n"
	}
	return []byte(res), nil
}

func copyBytes(in []byte, _ options.Options) ([]byte, error) {
	return append([]byte(nil), in...), nil
}

func init() {
	register("md", "html", mdToHTML)
	register("txt", "html", txtToHTML)
	register("html", "txt", htmlToText)
	register("txt", "md", copyBytes)
}
