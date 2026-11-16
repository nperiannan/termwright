// Package xlsx writes a small Excel workbook (Office Open XML): sheets of text and numbers with a header row, column
// widths, a few styles and line charts over a sheet's columns. Nothing else: enough for reports.
package xlsx

import (
	"archive/zip"
	"fmt"
	"io"
	"math"
	"strings"
)

// Styles a cell can have.
const (
	Plain  = 0
	Header = 1 // bold on grey: a table's header row
	Pass   = 2 // bold green
	Fail   = 3 // bold red
	Mono   = 4 // a fixed-width font (hex)
	Bold   = 5
	Title  = 6 // large and bold
	Change = 7 // on light yellow: something that changed
	Wrap   = 8 // a fixed-width font that wraps at line breaks (a hex dump)
)

// Cell is a value with a style; a plain string, int or float64 in a row is a cell of style Plain.
type Cell struct {
	V any
	S int
}

// Chart is a line chart (scatter with lines) of columns of a sheet over its first column.
type Chart struct {
	Title    string
	YTitle   string
	XTitle   string
	Sheet    string   // the sheet with the data
	Series   []int    // its columns (0-based) to draw, each a line; row 1 has their names
	Colors   []string // RRGGBB per series
	Rows     int      // data rows (from row 2)
	Col, Row int      // where its top left corner is (0-based)
	W, H     int      // its size in columns and rows
}

// Sheet is a worksheet.
type Sheet struct {
	Name   string
	Rows   [][]any
	Widths []float64 // column widths in characters (0: the default)
	Freeze bool      // the first row stays when scrolling
	Charts []Chart
}

func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r < 0x20 && r != '\t' && r != '\n' && r != '\r':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Col is a column's letters: 0 → A, 26 → AA.
func Col(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

func sheetXML(s Sheet, drawing bool) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	if s.Freeze {
		b.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	}
	if len(s.Widths) > 0 {
		b.WriteString("<cols>")
		for i, w := range s.Widths {
			if w > 0 {
				fmt.Fprintf(&b, `<col min="%d" max="%d" width="%.1f" customWidth="1"/>`, i+1, i+1, w)
			}
		}
		b.WriteString("</cols>")
	}
	b.WriteString("<sheetData>")
	for i, row := range s.Rows {
		fmt.Fprintf(&b, `<row r="%d">`, i+1)
		for j, v := range row {
			st := 0
			if c, ok := v.(Cell); ok {
				v, st = c.V, c.S
			}
			ref := fmt.Sprintf("%s%d", Col(j), i+1)
			sa := ""
			if st != 0 {
				sa = fmt.Sprintf(` s="%d"`, st)
			}
			switch x := v.(type) {
			case nil:
				if st != 0 {
					fmt.Fprintf(&b, `<c r="%s"%s/>`, ref, sa)
				}
			case int:
				fmt.Fprintf(&b, `<c r="%s"%s><v>%d</v></c>`, ref, sa, x)
			case float64:
				if math.IsNaN(x) || math.IsInf(x, 0) {
					continue
				}
				fmt.Fprintf(&b, `<c r="%s"%s><v>%s</v></c>`, ref, sa, fmt.Sprint(x))
			default:
				t := fmt.Sprint(x)
				fmt.Fprintf(&b, `<c r="%s"%s t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, sa, esc(t))
			}
		}
		b.WriteString("</row>")
	}
	b.WriteString("</sheetData>")
	if drawing {
		b.WriteString(`<drawing r:id="rId1"/>`)
	}
	b.WriteString("</worksheet>")
	return b.String()
}

func chartXML(c Chart) string {
	var b strings.Builder
	q := "'" + strings.ReplaceAll(c.Sheet, "'", "''") + "'"
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><c:roundedCorners val="0"/><c:chart>`)
	fmt.Fprintf(&b, `<c:title><c:tx><c:rich><a:bodyPr/><a:p><a:pPr><a:defRPr sz="1200" b="1"/></a:pPr><a:r><a:rPr lang="en-US" sz="1200" b="1"/><a:t>%s</a:t></a:r></a:p></c:rich></c:tx><c:overlay val="0"/></c:title>`, esc(c.Title))
	b.WriteString(`<c:autoTitleDeleted val="0"/><c:plotArea><c:layout/><c:scatterChart><c:scatterStyle val="lineMarker"/><c:varyColors val="0"/>`)
	last := c.Rows + 1
	for i, col := range c.Series {
		color := "2A78D6"
		if i < len(c.Colors) {
			color = strings.TrimPrefix(c.Colors[i], "#")
		}
		fmt.Fprintf(&b, `<c:ser><c:idx val="%d"/><c:order val="%d"/><c:tx><c:strRef><c:f>%s!$%s$1</c:f></c:strRef></c:tx>`, i, i, q, Col(col))
		fmt.Fprintf(&b, `<c:spPr><a:ln w="22225" cap="rnd"><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:round/></a:ln></c:spPr><c:marker><c:symbol val="none"/></c:marker>`, color)
		fmt.Fprintf(&b, `<c:xVal><c:numRef><c:f>%s!$A$2:$A$%d</c:f></c:numRef></c:xVal><c:yVal><c:numRef><c:f>%s!$%s$2:$%s$%d</c:f></c:numRef></c:yVal><c:smooth val="0"/></c:ser>`, q, last, q, Col(col), Col(col), last)
	}
	axis := func(id, cross int, pos, title string, grid bool) {
		zero := map[bool]string{true: `<c:min val="0"/>`}[grid] // the values' axis starts at 0
		fmt.Fprintf(&b, `<c:valAx><c:axId val="%d"/><c:scaling><c:orientation val="minMax"/>%s</c:scaling><c:delete val="0"/><c:axPos val="%s"/>`, id, zero, pos)
		if grid {
			b.WriteString(`<c:majorGridlines><c:spPr><a:ln w="6350"><a:solidFill><a:srgbClr val="E3E7EB"/></a:solidFill></a:ln></c:spPr></c:majorGridlines>`)
		}
		if title != "" {
			fmt.Fprintf(&b, `<c:title><c:tx><c:rich><a:bodyPr/><a:p><a:pPr><a:defRPr sz="900" b="0"/></a:pPr><a:r><a:rPr lang="en-US" sz="900" b="0"/><a:t>%s</a:t></a:r></a:p></c:rich></c:tx><c:overlay val="0"/></c:title>`, esc(title))
		}
		fmt.Fprintf(&b, `<c:numFmt formatCode="General" sourceLinked="1"/><c:majorTickMark val="out"/><c:minorTickMark val="none"/><c:tickLblPos val="nextTo"/><c:crossAx val="%d"/><c:crosses val="autoZero"/><c:crossBetween val="midCat"/></c:valAx>`, cross)
	}
	b.WriteString(`<c:axId val="1001"/><c:axId val="1002"/></c:scatterChart>`)
	axis(1001, 1002, "b", c.XTitle, false)
	axis(1002, 1001, "l", c.YTitle, true)
	b.WriteString(`</c:plotArea><c:legend><c:legendPos val="b"/><c:overlay val="0"/></c:legend><c:plotVisOnly val="1"/><c:dispBlanksAs val="span"/></c:chart></c:chartSpace>`)
	return b.String()
}

func drawingXML(cs []Chart, first int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart">`)
	for i, c := range cs {
		fmt.Fprintf(&b, `<xdr:twoCellAnchor><xdr:from><xdr:col>%d</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>%d</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from><xdr:to><xdr:col>%d</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>%d</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:to>`, c.Col, c.Row, c.Col+c.W, c.Row+c.H)
		fmt.Fprintf(&b, `<xdr:graphicFrame macro=""><xdr:nvGraphicFramePr><xdr:cNvPr id="%d" name="Chart %d"/><xdr:cNvGraphicFramePr/></xdr:nvGraphicFramePr><xdr:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/></xdr:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart"><c:chart r:id="rId%d"/></a:graphicData></a:graphic></xdr:graphicFrame><xdr:clientData/></xdr:twoCellAnchor>`, first+i+1, first+i+1, i+1)
	}
	b.WriteString(`</xdr:wsDr>`)
	return b.String()
}

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<fonts count="6"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><color rgb="FF1A7F37"/><name val="Calibri"/></font><font><b/><sz val="11"/><color rgb="FFCF222E"/><name val="Calibri"/></font><font><sz val="10"/><name val="Consolas"/></font><font><b/><sz val="16"/><name val="Calibri"/></font></fonts>` +
	`<fills count="4"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FFF0F2F5"/><bgColor indexed="64"/></patternFill></fill><fill><patternFill patternType="solid"><fgColor rgb="FFFFF4D6"/><bgColor indexed="64"/></patternFill></fill></fills>` +
	`<borders count="2"><border><left/><right/><top/><bottom/><diagonal/></border><border><left/><right/><top/><bottom style="thin"><color rgb="FFC9CFD6"/></bottom><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="9"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="0" fontId="1" fillId="2" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"/>` +
	`<xf numFmtId="0" fontId="2" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="0" fontId="3" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="0" fontId="4" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="0" fontId="5" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="0" fontId="0" fillId="3" borderId="0" xfId="0" applyFill="1"/>` +
	`<xf numFmtId="0" fontId="4" fillId="0" borderId="0" xfId="0" applyFont="1" applyAlignment="1"><alignment vertical="top" wrapText="1"/></xf></cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`

// Write writes the workbook.
func Write(w io.Writer, sheets []Sheet) error {
	z := zip.NewWriter(w)
	put := func(name, body string) error {
		f, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, body)
		return err
	}
	var ct, wb, wbr strings.Builder
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/><Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>`)
	wb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><bookViews><workbookView/></bookViews><sheets>`)
	wbr.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	chartN, drawN := 0, 0
	for i, s := range sheets {
		n := i + 1
		fmt.Fprintf(&ct, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		name := s.Name
		if len([]rune(name)) > 31 {
			name = string([]rune(name)[:31])
		}
		fmt.Fprintf(&wb, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, esc(name), n, n)
		fmt.Fprintf(&wbr, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
		if err := put(fmt.Sprintf("xl/worksheets/sheet%d.xml", n), sheetXML(s, len(s.Charts) > 0)); err != nil {
			return err
		}
		if len(s.Charts) == 0 {
			continue
		}
		drawN++
		fmt.Fprintf(&ct, `<Override PartName="/xl/drawings/drawing%d.xml" ContentType="application/vnd.openxmlformats-officedocument.drawing+xml"/>`, drawN)
		if err := put(fmt.Sprintf("xl/worksheets/_rels/sheet%d.xml.rels", n), fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/drawing%d.xml"/></Relationships>`, drawN)); err != nil {
			return err
		}
		var dr strings.Builder
		dr.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
		for j, c := range s.Charts {
			chartN++
			fmt.Fprintf(&ct, `<Override PartName="/xl/charts/chart%d.xml" ContentType="application/vnd.openxmlformats-officedocument.drawingml.chart+xml"/>`, chartN)
			fmt.Fprintf(&dr, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart" Target="../charts/chart%d.xml"/>`, j+1, chartN)
			if err := put(fmt.Sprintf("xl/charts/chart%d.xml", chartN), chartXML(c)); err != nil {
				return err
			}
		}
		dr.WriteString(`</Relationships>`)
		if err := put(fmt.Sprintf("xl/drawings/_rels/drawing%d.xml.rels", drawN), dr.String()); err != nil {
			return err
		}
		if err := put(fmt.Sprintf("xl/drawings/drawing%d.xml", drawN), drawingXML(s.Charts, chartN-len(s.Charts))); err != nil {
			return err
		}
	}
	ct.WriteString(`</Types>`)
	wb.WriteString(`</sheets></workbook>`)
	fmt.Fprintf(&wbr, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`, len(sheets)+1)
	for _, p := range [][2]string{
		{"[Content_Types].xml", ct.String()},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>`},
		{"docProps/app.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>Termwright</Application></Properties>`},
		{"xl/workbook.xml", wb.String()},
		{"xl/_rels/workbook.xml.rels", wbr.String()},
		{"xl/styles.xml", stylesXML},
	} {
		if err := put(p[0], p[1]); err != nil {
			return err
		}
	}
	return z.Close()
}
