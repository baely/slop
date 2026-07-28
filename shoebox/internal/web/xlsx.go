package web

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
)

// Minimal hand-rolled XLSX (it's a zip of XML parts) so the FY export needs
// no spreadsheet dependency. Inline strings only; hyperlinks are relative
// paths into the surrounding export zip, resolved once it's extracted.

type xlsxRow struct {
	Date, From, Subject, Notes string
	AmountCents                int64  // -1 = unknown
	ReceiptLink, ReceiptName   string // zip-relative target + display, "" if none
	EmailLink, EmailName       string
}

func xmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

func buildXLSX(sheet string, rows []xlsxRow, totalCents int64) []byte {
	var sb, links, rels strings.Builder
	cell := func(ref, val string) string {
		return fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, xmlEsc(val))
	}
	num := func(ref string, cents int64) string {
		if cents < 0 {
			return ""
		}
		return fmt.Sprintf(`<c r="%s"><v>%d.%02d</v></c>`, ref, cents/100, cents%100)
	}

	sb.WriteString(`<row r="1">`)
	for i, h := range []string{"Date", "From", "Subject", "Amount", "Notes", "Receipt", "Email"} {
		sb.WriteString(cell(fmt.Sprintf("%c1", 'A'+i), h))
	}
	sb.WriteString(`</row>`)

	relID := 0
	link := func(ref, target string) {
		relID++
		links.WriteString(fmt.Sprintf(`<hyperlink ref="%s" r:id="rIdL%d"/>`, ref, relID))
		rels.WriteString(fmt.Sprintf(`<Relationship Id="rIdL%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="%s" TargetMode="External"/>`, relID, xmlEsc(target)))
	}

	for i, r := range rows {
		n := i + 2
		sb.WriteString(fmt.Sprintf(`<row r="%d">`, n))
		sb.WriteString(cell(fmt.Sprintf("A%d", n), r.Date))
		sb.WriteString(cell(fmt.Sprintf("B%d", n), r.From))
		sb.WriteString(cell(fmt.Sprintf("C%d", n), r.Subject))
		sb.WriteString(num(fmt.Sprintf("D%d", n), r.AmountCents))
		sb.WriteString(cell(fmt.Sprintf("E%d", n), r.Notes))
		if r.ReceiptLink != "" {
			sb.WriteString(cell(fmt.Sprintf("F%d", n), r.ReceiptName))
			link(fmt.Sprintf("F%d", n), r.ReceiptLink)
		}
		if r.EmailLink != "" {
			sb.WriteString(cell(fmt.Sprintf("G%d", n), r.EmailName))
			link(fmt.Sprintf("G%d", n), r.EmailLink)
		}
		sb.WriteString(`</row>`)
	}
	tn := len(rows) + 2
	unit := "receipts"
	if len(rows) == 1 {
		unit = "receipt"
	}
	sb.WriteString(fmt.Sprintf(`<row r="%d">%s%s</row>`,
		tn, cell(fmt.Sprintf("C%d", tn), fmt.Sprintf("Total (%d %s)", len(rows), unit)), num(fmt.Sprintf("D%d", tn), totalCents)))

	sheetXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<cols>` +
		`<col min="1" max="1" width="12" customWidth="1"/>` +
		`<col min="2" max="2" width="26" customWidth="1"/>` +
		`<col min="3" max="3" width="50" customWidth="1"/>` +
		`<col min="4" max="4" width="12" customWidth="1"/>` +
		`<col min="5" max="5" width="28" customWidth="1"/>` +
		`<col min="6" max="6" width="34" customWidth="1"/>` +
		`<col min="7" max="7" width="14" customWidth="1"/>` +
		`</cols>` +
		`<sheetData>` + sb.String() + `</sheetData>`
	if links.Len() > 0 {
		sheetXML += `<hyperlinks>` + links.String() + `</hyperlinks>`
	}
	sheetXML += `</worksheet>`

	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
			`</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
			`</Relationships>`,
		"xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<sheets><sheet name="` + xmlEsc(sheet) + `" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
			`</Relationships>`,
		"xl/worksheets/sheet1.xml": sheetXML,
		"xl/worksheets/_rels/sheet1.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			rels.String() + `</Relationships>`,
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/worksheets/sheet1.xml", "xl/worksheets/_rels/sheet1.xml.rels"} {
		f, _ := zw.Create(name)
		f.Write([]byte(parts[name]))
	}
	zw.Close()
	return buf.Bytes()
}
