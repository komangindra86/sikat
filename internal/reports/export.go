package reports

import (
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/go-pdf/fpdf"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/unicode/norm"

	"sikat/internal/clock"
)

// Meta adalah informasi kop ekspor.
type Meta struct {
	Hospital    string
	GeneratedAt time.Time
	GeneratedBy string
}

func (m Meta) stamp() string {
	s := "Dibuat " + m.GeneratedAt.In(clock.Location).Format("02/01/2006 15:04") + " WITA"
	if m.GeneratedBy != "" {
		s += " oleh " + m.GeneratedBy
	}
	return s
}

// Filename: "sikat-laporan-koordinator-2026-11.xlsx".
func Filename(slug string, f Filter, ext string) string {
	var p string
	switch f.Period {
	case PeriodYear:
		p = fmt.Sprint(f.Year)
	case PeriodRange:
		p = f.From + "_" + f.To
	default:
		p = f.Month
	}
	return "sikat-" + slug + "-" + p + "." + ext
}

// ---------------------------------------------------------------------------
// Excel
// ---------------------------------------------------------------------------

// WriteXLSX menulis tabel sebagai workbook Excel (angka tetap berupa angka).
func WriteXLSX(w io.Writer, t Table, m Meta) error {
	x := excelize.NewFile()
	defer x.Close()
	const sheet = "Laporan"
	if err := x.SetSheetName("Sheet1", sheet); err != nil {
		return err
	}
	cell := func(col, row int) string {
		n, _ := excelize.CoordinatesToCellName(col, row)
		return n
	}
	titleStyle, _ := x.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}})
	subStyle, _ := x.NewStyle(&excelize.Style{Font: &excelize.Font{Size: 10, Color: "475569"}})
	border := []excelize.Border{{Type: "bottom", Color: "CBD5E1", Style: 1}}
	headStyle, _ := x.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "0F172A"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"E8F1FC"}, Pattern: 1},
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "center"}, Border: border,
	})
	numFmt := map[string]string{KindInt: "#,##0", KindPct: `0.0"%"`, KindMoney: `"Rp" #,##0`}
	styles := map[string]int{}
	styleFor := func(kind string, bold bool) int {
		key := fmt.Sprint(kind, bold)
		if id, ok := styles[key]; ok {
			return id
		}
		st := &excelize.Style{Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}, Font: &excelize.Font{Bold: bold}}
		if f, ok := numFmt[kind]; ok {
			st.CustomNumFmt = &f
			st.Alignment.Horizontal = "right"
		}
		if kind == KindDuration {
			st.Alignment.Horizontal = "right"
		}
		if bold {
			st.Border = []excelize.Border{{Type: "top", Color: "94A3B8", Style: 1}}
		}
		id, _ := x.NewStyle(st)
		styles[key] = id
		return id
	}

	_ = x.SetCellValue(sheet, "A1", t.Title)
	_ = x.SetCellStyle(sheet, "A1", "A1", titleStyle)
	_ = x.SetCellValue(sheet, "A2", t.Subtitle)
	_ = x.SetCellValue(sheet, "A3", strings.TrimPrefix(m.Hospital+" · "+m.stamp(), " · "))
	_ = x.SetCellStyle(sheet, "A2", "A3", subStyle)
	row := 4
	if len(t.Metrics) > 0 {
		parts := make([]string, 0, len(t.Metrics))
		for _, mt := range t.Metrics {
			parts = append(parts, mt.Label+": "+mt.Value)
		}
		_ = x.SetCellValue(sheet, "A4", strings.Join(parts, "   ·   "))
		_ = x.SetCellStyle(sheet, "A4", "A4", subStyle)
		row = 5
	}
	row++
	header := row
	for i, c := range t.Columns {
		_ = x.SetCellValue(sheet, cell(i+1, row), c.Label)
		w := c.Width
		if w == 0 {
			w = 12
		}
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = x.SetColWidth(sheet, col, col, w*1.25)
	}
	_ = x.SetCellStyle(sheet, cell(1, row), cell(len(t.Columns), row), headStyle)
	_ = x.SetRowHeight(sheet, row, 30)
	write := func(cells []Cell, bold bool) {
		row++
		for i, c := range cells {
			if i >= len(t.Columns) {
				break
			}
			kind := t.Columns[i].Kind
			ref := cell(i+1, row)
			switch {
			case !c.Valid:
				_ = x.SetCellValue(sheet, ref, "")
			case kind == KindInt || kind == KindPct || kind == KindMoney:
				_ = x.SetCellValue(sheet, ref, c.Num)
			default:
				_ = x.SetCellValue(sheet, ref, c.Text)
			}
			_ = x.SetCellStyle(sheet, ref, ref, styleFor(kind, bold))
		}
	}
	for _, r := range t.Rows {
		write(r, false)
	}
	if len(t.Totals) > 0 {
		write(t.Totals, true)
	}
	if len(t.Rows) == 0 {
		row++
		_ = x.SetCellValue(sheet, cell(1, row), "Tidak ada data pada periode ini.")
	}
	if len(t.Columns) > 0 && len(t.Rows) > 0 {
		last := header + len(t.Rows)
		_ = x.AutoFilter(sheet, cell(1, header)+":"+cell(len(t.Columns), last), nil)
	}
	_ = x.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: header, TopLeftCell: cell(1, header+1), ActivePane: "bottomLeft"})
	row++
	for _, n := range t.Notes {
		row++
		_ = x.SetCellValue(sheet, cell(1, row), n)
		_ = x.SetCellStyle(sheet, cell(1, row), cell(1, row), subStyle)
	}
	_ = x.SetDocProps(&excelize.DocProperties{Title: t.Title, Creator: "SIKAT", Description: t.Subtitle})
	return x.Write(w)
}

// ---------------------------------------------------------------------------
// PDF
// ---------------------------------------------------------------------------

// pdfReplacer: font standar PDF (Helvetica) hanya aman untuk ASCII di fpdf
// (pengukuran lebar teks per rune). Tanda baca umum diganti padanannya.
var pdfReplacer = strings.NewReplacer("≤", "<=", "≥", ">=", "→", "->", "←", "<-", "✓", "v", " ", " ",
	"–", "-", "—", "-", "·", "|", "…", "...", "‘", "'", "’", "'", "“", "\"", "”", "\"", "×", "x", "•", "-")

// pdfText mengubah teks menjadi ASCII: aksen dilepas (é → e), karakter lain → "?".
func pdfText(s string) string {
	s = norm.NFD.String(pdfReplacer.Replace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 128:
			b.WriteRune(r)
		case unicode.Is(unicode.Mn, r):
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

// WritePDF menulis tabel sebagai PDF A4 landscape dengan header tabel berulang
// di setiap halaman.
func WritePDF(w io.Writer, t Table, m Meta) error {
	pdf := fpdf.New("L", "mm", "A4", "")
	enc := pdfText
	const margin, lineH, pad = 10.0, 3.6, 1.2
	pdf.SetMargins(margin, 12, margin)
	pdf.SetAutoPageBreak(false, 14)
	pdf.SetTitle(t.Title, true)
	pdf.SetCreator("SIKAT", true)
	pdf.AliasNbPages("{nb}")
	footer := enc(strings.TrimPrefix(m.Hospital+" · ", " · ") + "SIKAT · " + m.stamp())
	pdf.SetFooterFunc(func() {
		pdf.SetY(-10)
		pdf.SetFont("Helvetica", "", 7)
		pdf.SetTextColor(100, 116, 139)
		pdf.CellFormat(0, 4, footer, "", 0, "L", false, 0, "")
		pdf.SetX(margin)
		pdf.CellFormat(0, 4, fmt.Sprintf("Halaman %d/{nb}", pdf.PageNo()), "", 0, "R", false, 0, "")
	})
	pdf.AddPage()
	pageW, pageH := pdf.GetPageSize()
	usable := pageW - 2*margin
	bottom := pageH - 14

	pdf.SetFont("Helvetica", "B", 14)
	pdf.SetTextColor(15, 23, 42)
	pdf.CellFormat(0, 7, enc(t.Title), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(71, 85, 105)
	pdf.MultiCell(0, 4.5, enc(t.Subtitle), "", "L", false)
	if len(t.Metrics) > 0 {
		parts := make([]string, 0, len(t.Metrics))
		for _, mt := range t.Metrics {
			parts = append(parts, mt.Label+": "+mt.Value)
		}
		pdf.MultiCell(0, 4.5, enc(strings.Join(parts, "   ·   ")), "", "L", false)
	}
	pdf.Ln(2)

	// Lebar kolom proporsional.
	total := 0.0
	for _, c := range t.Columns {
		if c.Width == 0 {
			total += 10
		} else {
			total += c.Width
		}
	}
	widths := make([]float64, len(t.Columns))
	for i, c := range t.Columns {
		cw := c.Width
		if cw == 0 {
			cw = 10
		}
		widths[i] = usable * cw / total
	}
	align := func(i int) string {
		if t.Columns[i].Numeric() {
			return "R"
		}
		return "L"
	}
	rowHeight := func(cells []string) float64 {
		maxLines := 1
		for i, s := range cells {
			if n := len(pdf.SplitText(s, widths[i]-2*pad)); n > maxLines {
				maxLines = n
			}
		}
		return float64(maxLines)*lineH + 2*pad
	}
	drawRow := func(cells []string, fill bool, topLine bool) {
		h := rowHeight(cells)
		x, y := pdf.GetXY()
		if fill {
			pdf.SetFillColor(232, 241, 252)
			pdf.Rect(x, y, usable, h, "F")
		}
		for i, s := range cells {
			pdf.SetXY(x+pad, y+pad)
			pdf.MultiCell(widths[i]-2*pad, lineH, s, "", align(i), false)
			x += widths[i]
		}
		pdf.SetDrawColor(203, 213, 225)
		if topLine {
			pdf.SetDrawColor(148, 163, 184)
			pdf.Line(margin, y, margin+usable, y)
		}
		pdf.Line(margin, y+h, margin+usable, y+h)
		pdf.SetXY(margin, y+h)
	}
	head := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		head[i] = enc(c.Label)
	}
	drawHeader := func() {
		pdf.SetFont("Helvetica", "B", 7.5)
		pdf.SetTextColor(15, 23, 42)
		drawRow(head, true, false)
		pdf.SetFont("Helvetica", "", 7.5)
		pdf.SetTextColor(30, 41, 59)
	}
	drawHeader()
	put := func(r []Cell, bold bool) {
		cells := make([]string, len(t.Columns))
		for i := range t.Columns {
			if i < len(r) {
				cells[i] = enc(r[i].Text)
			}
		}
		if bold {
			pdf.SetFont("Helvetica", "B", 7.5)
		}
		if pdf.GetY()+rowHeight(cells) > bottom {
			pdf.AddPage()
			drawHeader()
			if bold {
				pdf.SetFont("Helvetica", "B", 7.5)
			}
		}
		drawRow(cells, false, bold)
		if bold {
			pdf.SetFont("Helvetica", "", 7.5)
		}
	}
	for _, r := range t.Rows {
		put(r, false)
	}
	if len(t.Totals) > 0 {
		put(t.Totals, true)
	}
	if len(t.Rows) == 0 {
		pdf.Ln(2)
		pdf.CellFormat(0, 6, enc("Tidak ada data pada periode ini."), "", 1, "L", false, 0, "")
	}
	if len(t.Notes) > 0 {
		pdf.Ln(3)
		pdf.SetFont("Helvetica", "I", 7)
		pdf.SetTextColor(100, 116, 139)
		for _, n := range t.Notes {
			if pdf.GetY()+8 > bottom {
				pdf.AddPage()
			}
			pdf.MultiCell(0, 3.6, enc(n), "", "L", false)
		}
	}
	if err := pdf.Error(); err != nil {
		return err
	}
	return pdf.Output(w)
}
