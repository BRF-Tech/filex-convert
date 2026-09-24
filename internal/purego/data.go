package purego

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/brf-tech/filex-convert/internal/options"
)

// Tabular data. A CSV/TSV with a header row becomes an array of objects
// (one per row, keyed by header); the reverse accepts an array of objects
// (columns = the union of keys, first-seen order) or an array of arrays.
// Numbers and booleans in CSV cells stay strings — a converter must not
// guess that "007" is a number.

// ErrNotTabular is answered when JSON/YAML cannot be laid out as rows.
var ErrNotTabular = errors.New("data is not an array of objects or arrays")

func readDelimited(in []byte, comma rune) ([][]string, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(in, []byte("\xef\xbb\xbf"))))
	r.Comma = comma
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv: %w", err)
	}
	return rows, nil
}

func writeDelimited(rows [][]string, comma rune) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Comma = comma
	w.UseCRLF = false
	if err := w.WriteAll(rows); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// rowsToRecords turns header+rows into ordered objects.
func rowsToRecords(rows [][]string) []orderedRecord {
	if len(rows) == 0 {
		return nil
	}
	header := rows[0]
	out := make([]orderedRecord, 0, len(rows)-1)
	for _, row := range rows[1:] {
		rec := orderedRecord{keys: make([]string, 0, len(header)), values: map[string]any{}}
		for i, h := range header {
			if h == "" {
				h = "col" + strconv.Itoa(i+1)
			}
			v := ""
			if i < len(row) {
				v = row[i]
			}
			rec.keys = append(rec.keys, h)
			rec.values[h] = v
		}
		out = append(out, rec)
	}
	return out
}

// orderedRecord keeps column order through JSON/YAML encoding.
type orderedRecord struct {
	keys   []string
	values map[string]any
}

func (r orderedRecord) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range r.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(r.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (r orderedRecord) yamlNode() *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range r.keys {
		kn := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
		vn := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: cellString(r.values[k])}
		if looksLikeNonString(vn.Value) {
			vn.Style = yaml.DoubleQuotedStyle
		}
		n.Content = append(n.Content, kn, vn)
	}
	return n
}

// looksLikeNonString says whether a bare YAML scalar would be read back as
// something other than a string; such cells are quoted so a round trip
// keeps "007" and "true" as text.
func looksLikeNonString(s string) bool {
	if s == "" {
		return true
	}
	var v any
	if err := yaml.Unmarshal([]byte(s), &v); err != nil {
		return true
	}
	_, isStr := v.(string)
	return !isStr
}

// recordsToRows lays generic decoded data out as header+rows.
func recordsToRows(v any) ([][]string, error) {
	arr, ok := v.([]any)
	if !ok {
		// a single object becomes one row
		if obj, isObj := v.(map[string]any); isObj {
			arr = []any{obj}
		} else {
			return nil, ErrNotTabular
		}
	}
	if len(arr) == 0 {
		return nil, nil
	}
	if _, isArr := arr[0].([]any); isArr {
		rows := make([][]string, 0, len(arr))
		for _, row := range arr {
			cells, ok := row.([]any)
			if !ok {
				return nil, ErrNotTabular
			}
			out := make([]string, len(cells))
			for i, c := range cells {
				out[i] = cellString(c)
			}
			rows = append(rows, out)
		}
		return rows, nil
	}
	var header []string
	seen := map[string]bool{}
	objs := make([]map[string]any, 0, len(arr))
	for _, row := range arr {
		obj, ok := row.(map[string]any)
		if !ok {
			return nil, ErrNotTabular
		}
		objs = append(objs, obj)
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !seen[k] {
				seen[k] = true
				header = append(header, k)
			}
		}
	}
	rows := [][]string{header}
	for _, obj := range objs {
		row := make([]string, len(header))
		for i, k := range header {
			if v, ok := obj[k]; ok {
				row[i] = cellString(v)
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func cellString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(b)
	}
}

// decodeJSON decodes JSON into generic values; object key order is lost
// (Go maps), so columns are sorted by key when written as rows.
func decodeJSON(in []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(in))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	return normalise(v), nil
}

// normalise turns json.Number into float64 or keeps the literal as string
// when it does not fit, and yaml maps into map[string]any.
func normalise(v any) any {
	switch x := v.(type) {
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return f
		}
		return x.String()
	case []any:
		for i := range x {
			x[i] = normalise(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = normalise(x[k])
		}
		return x
	case map[any]any:
		out := map[string]any{}
		for k, val := range x {
			out[fmt.Sprint(k)] = normalise(val)
		}
		return out
	}
	return v
}

func decodeYAML(in []byte) (any, error) {
	var v any
	if err := yaml.Unmarshal(in, &v); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	return normalise(v), nil
}

func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func delimitedToRecords(comma rune) func([]byte) ([]orderedRecord, error) {
	return func(in []byte) ([]orderedRecord, error) {
		rows, err := readDelimited(in, comma)
		if err != nil {
			return nil, err
		}
		return rowsToRecords(rows), nil
	}
}

func init() {
	csvRecords := delimitedToRecords(',')
	tsvRecords := delimitedToRecords('\t')

	// csv/tsv → json / yaml
	toJSON := func(read func([]byte) ([]orderedRecord, error)) Func {
		return func(in []byte, _ options.Options) ([]byte, error) {
			recs, err := read(in)
			if err != nil {
				return nil, err
			}
			if recs == nil {
				recs = []orderedRecord{}
			}
			return encodeJSON(recs)
		}
	}
	toYAML := func(read func([]byte) ([]orderedRecord, error)) Func {
		return func(in []byte, _ options.Options) ([]byte, error) {
			recs, err := read(in)
			if err != nil {
				return nil, err
			}
			seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for _, r := range recs {
				seq.Content = append(seq.Content, r.yamlNode())
			}
			return encodeYAML(seq)
		}
	}
	register("csv", "json", toJSON(csvRecords))
	register("tsv", "json", toJSON(tsvRecords))
	register("csv", "yaml", toYAML(csvRecords))

	// json / yaml → csv / tsv
	fromTree := func(decode func([]byte) (any, error), comma rune) Func {
		return func(in []byte, _ options.Options) ([]byte, error) {
			v, err := decode(in)
			if err != nil {
				return nil, err
			}
			rows, err := recordsToRows(v)
			if err != nil {
				return nil, err
			}
			return writeDelimited(rows, comma)
		}
	}
	register("json", "csv", fromTree(decodeJSON, ','))
	register("json", "tsv", fromTree(decodeJSON, '\t'))
	register("yaml", "csv", fromTree(decodeYAML, ','))

	// json ↔ yaml
	register("json", "yaml", func(in []byte, _ options.Options) ([]byte, error) {
		v, err := decodeJSON(in)
		if err != nil {
			return nil, err
		}
		return encodeYAML(v)
	})
	register("yaml", "json", func(in []byte, _ options.Options) ([]byte, error) {
		v, err := decodeYAML(in)
		if err != nil {
			return nil, err
		}
		return encodeJSON(v)
	})

	// csv ↔ tsv
	register("csv", "tsv", func(in []byte, _ options.Options) ([]byte, error) {
		rows, err := readDelimited(in, ',')
		if err != nil {
			return nil, err
		}
		return writeDelimited(rows, '\t')
	})
	register("tsv", "csv", func(in []byte, _ options.Options) ([]byte, error) {
		rows, err := readDelimited(in, '\t')
		if err != nil {
			return nil, err
		}
		return writeDelimited(rows, ',')
	})
}
