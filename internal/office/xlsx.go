package office

import (
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// ── XLSX writer ──

const xlsxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
</Types>`

const xlsxRootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

const xlsxWorkbook = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets>
</workbook>`

const xlsxWorkbookRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

const xlsxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>
<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>
<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>
<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
<cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs>
</styleSheet>`

// WriteXLSX writes rows into one sheet. The first row is bold when header
// is set. A cell that parses as a number without a leading zero is stored
// as a number; everything else stays text (inline strings, no shared table).
func WriteXLSX(rows [][]string, header bool) ([]byte, error) {
	var sh strings.Builder
	sh.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for ri, row := range rows {
		sh.WriteString(fmt.Sprintf(`<row r="%d">`, ri+1))
		for ci, v := range row {
			ref := colName(ci) + strconv.Itoa(ri+1)
			style := ""
			if header && ri == 0 {
				style = ` s="1"`
			}
			if n, ok := numeric(v); ok {
				sh.WriteString(fmt.Sprintf(`<c r="%s"%s><v>%s</v></c>`, ref, style, n))
				continue
			}
			if v == "" {
				continue
			}
			sh.WriteString(fmt.Sprintf(`<c r="%s"%s t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, esc(v)))
		}
		sh.WriteString("</row>")
	}
	sh.WriteString("</sheetData></worksheet>")
	return writePkg([]part{
		{name: "[Content_Types].xml", data: []byte(xlsxContentTypes)},
		{name: "_rels/.rels", data: []byte(xlsxRootRels)},
		{name: "xl/workbook.xml", data: []byte(xlsxWorkbook)},
		{name: "xl/_rels/workbook.xml.rels", data: []byte(xlsxWorkbookRels)},
		{name: "xl/styles.xml", data: []byte(xlsxStyles)},
		{name: "xl/worksheets/sheet1.xml", data: []byte(sh.String())},
	})
}

// numeric says whether a cell should be stored as a number, and answers
// its canonical spelling. "007", "1e5" and " 12" stay text.
func numeric(s string) (string, bool) {
	if s == "" || len(s) > 20 {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c == '-' || c == '.') {
			return "", false
		}
	}
	if strings.HasPrefix(s, "0") && len(s) > 1 && s[1] != '.' || strings.HasPrefix(s, "-0") && len(s) > 2 && s[2] != '.' {
		return "", false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || strings.HasSuffix(s, ".") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "-.") {
		return "", false
	}
	return strconv.FormatFloat(f, 'f', -1, 64), true
}

// colName turns 0 → A, 25 → Z, 26 → AA.
func colName(i int) string {
	s := ""
	i++
	for i > 0 {
		i--
		s = string(rune('A'+i%26)) + s
		i /= 26
	}
	return s
}

var cellRefRe = regexp.MustCompile(`^([A-Z]+)(\d+)$`)

// colIndex turns "A" → 0, "AA" → 26.
func colIndex(ref string) int {
	m := cellRefRe.FindStringSubmatch(ref)
	if m == nil {
		return -1
	}
	n := 0
	for _, c := range m[1] {
		n = n*26 + int(c-'A') + 1
	}
	return n - 1
}

// ── XLSX reader ──

// ReadXLSX answers the first worksheet as rows of strings. Shared and
// inline strings, numbers, booleans and formula results are read; dates
// come out as their serial numbers (the cell style is not interpreted).
func ReadXLSX(data []byte) ([][]string, error) {
	p, err := openPkg(data)
	if err != nil {
		return nil, err
	}
	sheet := firstSheet(p)
	body, err := p.read(sheet)
	if err != nil {
		return nil, err
	}
	shared := sharedStrings(p)
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	var rows [][]string
	var row []string
	inRow := false
	cellType := ""
	cellCol := -1
	var text strings.Builder
	inV, inT := false, false
	inIS := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("xlsx: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				inRow = true
				row = nil
			case "c":
				if !inRow {
					continue
				}
				cellType = attrOf(t, "t")
				cellCol = colIndex(attrOf(t, "r"))
				if cellCol < 0 {
					cellCol = len(row)
				}
				text.Reset()
			case "v":
				inV = true
			case "is":
				inIS = true
			case "t":
				if inIS {
					inT = true
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "t":
				inT = false
			case "is":
				inIS = false
			case "c":
				if !inRow {
					continue
				}
				v := text.String()
				switch cellType {
				case "s":
					if i, err := strconv.Atoi(v); err == nil && i >= 0 && i < len(shared) {
						v = shared[i]
					}
				case "b":
					if v == "1" {
						v = "TRUE"
					} else if v == "0" {
						v = "FALSE"
					}
				case "", "n":
					if f, err := strconv.ParseFloat(v, 64); err == nil {
						v = strconv.FormatFloat(f, 'f', -1, 64)
					}
				}
				for len(row) < cellCol {
					row = append(row, "")
				}
				if cellCol < len(row) {
					row[cellCol] = v
				} else {
					row = append(row, v)
				}
				if len(row) > 16384 {
					return nil, fmt.Errorf("xlsx: too many columns")
				}
			case "row":
				inRow = false
				rows = append(rows, row)
				if len(rows) > 1<<20 {
					return nil, fmt.Errorf("xlsx: too many rows")
				}
			}
		case xml.CharData:
			if inV || inT {
				text.Write(t)
			}
		}
	}
	return trimEmptyRows(rows), nil
}

// firstSheet resolves the workbook's first sheet part.
func firstSheet(p *pkg) string {
	wb, err := p.read("xl/workbook.xml")
	if err != nil {
		return "xl/worksheets/sheet1.xml"
	}
	rels := readRels(p, "xl/_rels/workbook.xml.rels")
	dec := xml.NewDecoder(strings.NewReader(string(wb)))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "sheet" {
			if target, ok := rels[attrOf(se, "id")]; ok {
				return resolve("xl/workbook.xml", target)
			}
			break
		}
	}
	return "xl/worksheets/sheet1.xml"
}

// sharedStrings reads xl/sharedStrings.xml (rich runs concatenated).
func sharedStrings(p *pkg) []string {
	data, err := p.read("xl/sharedStrings.xml")
	if err != nil {
		return nil
	}
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	var out []string
	var cur strings.Builder
	inSI, inT := false, false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI = true
				cur.Reset()
			case "t":
				inT = inSI
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				inSI = false
				out = append(out, cur.String())
			case "t":
				inT = false
			}
		case xml.CharData:
			if inT {
				cur.Write(t)
			}
		}
	}
	return out
}
