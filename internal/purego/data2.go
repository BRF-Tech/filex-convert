package purego

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"

	"github.com/brf-tech/filex-convert/internal/doc"
	"github.com/brf-tech/filex-convert/internal/office"
	"github.com/brf-tech/filex-convert/internal/options"
)

// More data conversions: XML and TOML join the JSON/YAML tree family;
// tabular data reaches Markdown, HTML and XLSX; XLSX and ODS are read
// back into rows.

// ── XML ↔ tree ──

// xmlName makes a string usable as an element name.
func xmlName(s string) string {
	var b strings.Builder
	for i, r := range s {
		ok := unicode.IsLetter(r) || r == '_' || (i > 0 && (unicode.IsDigit(r) || r == '-' || r == '.'))
		if ok {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "item"
	}
	out := b.String()
	if strings.HasPrefix(strings.ToLower(out), "xml") {
		out = "_" + out
	}
	return out
}

// treeToXML serialises generic decoded data: objects become elements per
// key (sorted), arrays repeat the element, scalars are text.
func treeToXML(v any) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(xml.Header)
	writeXMLNode(&b, "root", v, 0)
	return b.Bytes(), nil
}

func writeXMLNode(b *bytes.Buffer, name string, v any, depth int) {
	indent := strings.Repeat("  ", depth)
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString(indent + "<" + name + ">\n")
		for _, k := range keys {
			writeXMLNode(b, xmlName(k), x[k], depth+1)
		}
		b.WriteString(indent + "</" + name + ">\n")
	case []any:
		if depth == 0 {
			b.WriteString(indent + "<" + name + ">\n")
			for _, item := range x {
				writeXMLNode(b, "item", item, depth+1)
			}
			b.WriteString(indent + "</" + name + ">\n")
			return
		}
		for _, item := range x {
			writeXMLNode(b, name, item, depth)
		}
	case nil:
		b.WriteString(indent + "<" + name + "/>\n")
	default:
		var esc bytes.Buffer
		xml.EscapeText(&esc, []byte(cellString(x)))
		b.WriteString(indent + "<" + name + ">" + esc.String() + "</" + name + ">\n")
	}
}

// xmlToTree decodes XML into the generic tree: attributes become "@name"
// keys, text of a leaf becomes the value, text beside children becomes
// "#text", repeated child names become arrays.
func xmlToTree(in []byte) (any, error) {
	dec := xml.NewDecoder(bytes.NewReader(in))
	dec.Strict = false
	dec.CharsetReader = func(charset string, r io.Reader) (io.Reader, error) { return r, nil }
	type frame struct {
		name     string
		attrs    map[string]any
		children map[string][]any
		order    []string
		text     strings.Builder
	}
	var stack []*frame
	var rootName string
	var result any
	finish := func(f *frame) any {
		text := strings.TrimSpace(f.text.String())
		if len(f.attrs) == 0 && len(f.children) == 0 {
			return text
		}
		obj := map[string]any{}
		for k, v := range f.attrs {
			obj["@"+k] = v
		}
		for _, name := range f.order {
			vals := f.children[name]
			if len(vals) == 1 {
				obj[name] = vals[0]
			} else {
				obj[name] = vals
			}
		}
		if text != "" {
			obj["#text"] = text
		}
		return obj
	}
	depth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > 512 {
				return nil, errors.New("xml: nesting too deep")
			}
			f := &frame{name: t.Name.Local, attrs: map[string]any{}, children: map[string][]any{}}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				f.attrs[a.Name.Local] = a.Value
			}
			stack = append(stack, f)
		case xml.EndElement:
			depth--
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			v := finish(f)
			if len(stack) == 0 {
				rootName = f.name
				result = v
				continue
			}
			p := stack[len(stack)-1]
			if _, seen := p.children[f.name]; !seen {
				p.order = append(p.order, f.name)
			}
			p.children[f.name] = append(p.children[f.name], v)
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write(t)
			}
		}
	}
	if result == nil {
		return nil, errors.New("xml: no root element")
	}
	// a <root> wrapper written by treeToXML unwraps to its content; a
	// wrapper holding only <item> children unwraps to the array
	if rootName == "root" {
		if obj, ok := result.(map[string]any); ok {
			if items, only := obj["item"]; only && len(obj) == 1 {
				if arr, isArr := items.([]any); isArr {
					return arr, nil
				}
				return []any{items}, nil
			}
		}
		return result, nil
	}
	return map[string]any{rootName: result}, nil
}

// ── TOML ↔ tree ──

func decodeTOML(in []byte) (any, error) {
	var v map[string]any
	if _, err := toml.Decode(string(in), &v); err != nil {
		return nil, fmt.Errorf("toml: %w", err)
	}
	return normalise(v), nil
}

func encodeTOML(v any) ([]byte, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		// TOML documents are tables; wrap anything else
		obj = map[string]any{"value": v}
	}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = "  "
	if err := enc.Encode(tomlSafe(obj)); err != nil {
		return nil, fmt.Errorf("toml: %w", err)
	}
	return buf.Bytes(), nil
}

// tomlSafe drops nils (TOML has no null) and writes whole numbers as
// integers (JSON numbers arrive as float64).
func tomlSafe(v any) any {
	switch x := v.(type) {
	case float64:
		if x == float64(int64(x)) && x > -1e15 && x < 1e15 {
			return int64(x)
		}
		return x
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			if val == nil {
				continue
			}
			out[k] = tomlSafe(val)
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, val := range x {
			if val == nil {
				continue
			}
			out = append(out, tomlSafe(val))
		}
		return out
	}
	return v
}

// ── rows helpers ──

func rowsFromTree(v any) ([][]string, error) { return recordsToRows(v) }

func rowsToJSON(rows [][]string) ([]byte, error) {
	recs := rowsToRecords(rows)
	if recs == nil {
		recs = []orderedRecord{}
	}
	return encodeJSON(recs)
}

func rowsToYAML(rows [][]string) ([]byte, error) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, r := range rowsToRecords(rows) {
		seq.Content = append(seq.Content, r.yamlNode())
	}
	return encodeYAML(seq)
}

func rowsToMarkdown(rows [][]string) ([]byte, error) {
	if len(rows) == 0 {
		return []byte{}, nil
	}
	return doc.ToMarkdown(doc.TableFromRows(rows, true)), nil
}

func rowsToHTML(rows [][]string) ([]byte, error) {
	return doc.ToHTML(doc.TableFromRows(rows, true)), nil
}

// rowsOut answers a converter that writes rows in the target format.
func rowsOut(to string) func([][]string) ([]byte, error) {
	switch to {
	case "csv":
		return func(rows [][]string) ([]byte, error) { return writeDelimited(rows, ',') }
	case "tsv":
		return func(rows [][]string) ([]byte, error) { return writeDelimited(rows, '\t') }
	case "json":
		return rowsToJSON
	case "yaml":
		return rowsToYAML
	case "md":
		return rowsToMarkdown
	case "html":
		return rowsToHTML
	case "xlsx":
		return func(rows [][]string) ([]byte, error) { return office.WriteXLSX(rows, true) }
	}
	return nil
}

// rowsIn answers a reader of rows for the source format.
func rowsIn(from string) func([]byte) ([][]string, error) {
	switch from {
	case "csv":
		return func(in []byte) ([][]string, error) { return readDelimited(in, ',') }
	case "tsv":
		return func(in []byte) ([][]string, error) { return readDelimited(in, '\t') }
	case "json":
		return func(in []byte) ([][]string, error) {
			v, err := decodeJSON(in)
			if err != nil {
				return nil, err
			}
			return rowsFromTree(v)
		}
	case "yaml":
		return func(in []byte) ([][]string, error) {
			v, err := decodeYAML(in)
			if err != nil {
				return nil, err
			}
			return rowsFromTree(v)
		}
	case "xlsx":
		return office.ReadXLSX
	case "ods":
		return office.ReadODS
	}
	return nil
}

func rowsConverter(from, to string) Func {
	read, write := rowsIn(from), rowsOut(to)
	return func(in []byte, _ options.Options) ([]byte, error) {
		rows, err := read(in)
		if err != nil {
			return nil, err
		}
		return write(rows)
	}
}

// treeIn / treeOut cover the tree formats.
func treeIn(from string) func([]byte) (any, error) {
	switch from {
	case "json":
		return decodeJSON
	case "yaml":
		return decodeYAML
	case "xml":
		return xmlToTree
	case "toml":
		return decodeTOML
	}
	return nil
}

func treeOut(to string) func(any) ([]byte, error) {
	switch to {
	case "json":
		return encodeJSON
	case "yaml":
		return encodeYAML
	case "xml":
		return treeToXML
	case "toml":
		return encodeTOML
	}
	return nil
}

func treeConverter(from, to string) Func {
	read, write := treeIn(from), treeOut(to)
	return func(in []byte, _ options.Options) ([]byte, error) {
		v, err := read(in)
		if err != nil {
			return nil, err
		}
		return write(v)
	}
}

func init() {
	// tree family: xml and toml with json and yaml
	for _, pair := range [][2]string{{"json", "xml"}, {"xml", "json"}, {"yaml", "xml"}, {"xml", "yaml"}, {"json", "toml"}, {"toml", "json"}, {"yaml", "toml"}, {"toml", "yaml"}, {"xml", "toml"}, {"toml", "xml"}} {
		register(pair[0], pair[1], treeConverter(pair[0], pair[1]))
	}
	// rows family
	for _, from := range []string{"csv", "tsv"} {
		for _, to := range []string{"md", "html", "xlsx"} {
			register(from, to, rowsConverter(from, to))
		}
	}
	register("tsv", "yaml", rowsConverter("tsv", "yaml"))
	register("yaml", "tsv", rowsConverter("yaml", "tsv"))
	for _, from := range []string{"json", "yaml"} {
		register(from, "xlsx", rowsConverter(from, "xlsx"))
	}
	for _, from := range []string{"xlsx", "ods"} {
		for _, to := range []string{"csv", "tsv", "json", "yaml", "md", "html"} {
			register(from, to, rowsConverter(from, to))
		}
	}
	register("ods", "xlsx", rowsConverter("ods", "xlsx"))
	// xml/toml → rows through the tree
	for _, from := range []string{"xml", "toml"} {
		read := treeIn(from)
		for _, to := range []string{"csv"} {
			write := rowsOut(to)
			register(from, to, func(in []byte, _ options.Options) ([]byte, error) {
				v, err := read(in)
				if err != nil {
					return nil, err
				}
				rows, err := rowsFromTree(v)
				if err != nil {
					return nil, err
				}
				return write(rows)
			})
		}
	}
}
