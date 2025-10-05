package pdf

import (
	"slices"
	"strings"
)

// Table is a structure containing the parameters for drawing a table.  To draw
// a table, create a Table structure, call its Cell method repeatedly for each
// cell in it, and then call its Draw method.
type Table struct {
	// Page is the page number onto which to draw the table.  Default 1.
	Page int
	// Rectangle is the page area into which to draw the table.  The table
	// is vertically aligned to the top of the rectangle and horizontally
	// centered in it.  After a call to Draw, Rectangle reflects the actual
	// rectangle used.
	Rectangle Rectangle
	// TableBorderWidth is the width of the border drawn around the table.
	TableBorderWidth float64
	// TableBorderStroke is the color of the border drawn around the table.
	TableBorderStroke []byte
	// CellPadX is the padding on the left and right side of each cell.
	CellPadX float64
	// CellPadY is the padding on the top and bottom of each cell.
	CellPadY float64
	// CellBorderWidth is the width of the border drawn around each cell of
	// the table.
	CellBorderWidth float64
	// CellBorderStroke is the color of the border drawn around each cell of
	// the table.
	CellBorderStroke []byte
	// Align specifies the alignment of the table within the Rectangle.  It
	// contains up to two letters:  "t", "m", or "b" to specify top, middle,
	// or bottom vertical alignment, and "l", "c", or "r" to specify left,
	// center, or right horizontal alignment.  The default is "tc".
	Align string

	widths  []float64
	heights []float64
	cells   []*Cell
}

// Cell is a structure defining the parameters for a single cell in a Table.
type Cell struct {
	// Row is the row number of the cell.  For cells that span rows,
	// it is the topmost row that the cell spans.  Rows are numbered from 0
	// starting at the top of the table.
	Row int
	// Col is the column number of the cell.  For cells that span columns,
	// it is the leftmost column that the cell spans.  Cells are numbered
	// from 0 starting at the left of the table.
	Col int
	// RowSpan is the number of rows the cell spans (default 1).
	RowSpan int
	// ColSpan is the number of columns the cell spans (default 1).
	ColSpan int
	// Fill is the background color of the cell, if any.
	Fill []byte
	// Text is the text to place in the cell.  Note that the Page,
	// Rectangle, MinFontSize, and Wrap will be overridden by Table.Draw.
	Text *Text
}

// ColumnWidths returns the column widths.  These can be passed to
// SetColumnWidths of a different table to align layouts.  Call this before
// calling Draw.
func (t *Table) ColumnWidths() []float64 {
	return slices.Clone(t.widths)
}

// SetColumnWidths sets the column widths.  Call this before calling Cell.
func (t *Table) SetColumnWidths(widths []float64) {
	t.widths = widths
}

// Cell adds a cell to the table.  Cells may be added in any order.  Note that
// cells with rowSpan != 1 are ignored in row height calculations and cells with
// colSpan != 1 are ignored in column width calculations.
func (t *Table) Cell(cell *Cell) {
	t.cells = append(t.cells, cell)
	if cell.RowSpan <= 1 {
		lines := strings.Count(cell.Text.String, "\n") + 1
		if cell.Text.FontSize == 0 {
			cell.Text.FontSize = 12.0
		}
		if cell.Text.LineHeight == 0 {
			cell.Text.LineHeight = 1.2
		}
		height := float64(lines) * cell.Text.LineHeight * cell.Text.FontSize
		for len(t.heights) <= cell.Row {
			t.heights = append(t.heights, 0)
		}
		t.heights[cell.Row] = max(t.heights[cell.Row], height)
	}
	if cell.ColSpan <= 1 {
		lines := strings.Split(cell.Text.String, "\n")
		if cell.Text.Font == "" {
			cell.Text.Font = "Helvetica"
		}
		if cell.Text.FontSize == 0 {
			cell.Text.FontSize = 12.0
		}
		for len(t.widths) <= cell.Col {
			t.widths = append(t.widths, 0)
		}
		for _, line := range lines {
			w, _, _ := MeasureText(line, cell.Text.Font, cell.Text.FontSize)
			t.widths[cell.Col] = max(t.widths[cell.Col], w)
		}
	}
}

// InsertRow inserts an empty row at the specified row number.  In other words,
// any existing Cells with a row number >= the specified number have their row
// number incremented.
func (t *Table) InsertRow(row int) {
	for _, cell := range t.cells {
		if cell.Row >= row {
			cell.Row++
		} else if cell.Row+cell.RowSpan > row {
			cell.RowSpan++
		}
	}
	t.heights = slices.Insert(t.heights, row, 0)
}

// Size returns the computed size of the table.  It must be called before Draw.
// (To get the saze of the table after Draw, look at its Rectangle.)
func (t *Table) Size() (width, height float64) {
	for _, w := range t.widths {
		width += w + 2*t.CellPadX + t.CellBorderWidth
	}
	for _, h := range t.heights {
		height += h + 2*t.CellPadY + t.CellBorderWidth
	}
	width += 2*t.TableBorderWidth - t.CellBorderWidth
	height += 2*t.TableBorderWidth - t.CellBorderWidth
	return width, height
}

// Draw draws a table.  The table Rectangle is left encapsulating the space used
// by the table.  If the table doesn't fit on the page, nothing is drawn and
// ErrDoesntFit is returned.
func (t *Table) Draw(pdf *PDF) (err error) {
	var (
		twidth  float64
		theight float64
	)
	// Add padding to the cells.
	for i := range t.widths {
		t.widths[i] += 2 * t.CellPadX
		twidth += t.widths[i]
	}
	for i := range t.heights {
		t.heights[i] += 2 * t.CellPadY
		theight += t.heights[i]
	}
	// Compute the table dimensions.
	twidth += 2*t.TableBorderWidth + float64(len(t.widths)-1)*t.CellBorderWidth
	theight += 2*t.TableBorderWidth + float64(len(t.heights)-1)*t.CellBorderWidth
	if theight > t.Rectangle.URY-t.Rectangle.LLY || twidth > t.Rectangle.URX-t.Rectangle.LLX {
		return ErrDoesntFit
	}
	// Compute the table position.
	switch {
	case strings.ContainsRune(t.Align, 'm'):
		t.Rectangle.URY = (t.Rectangle.LLY + t.Rectangle.URY + theight) / 2
	case strings.ContainsRune(t.Align, 'b'):
		t.Rectangle.URY = t.Rectangle.LLY + theight
	}
	t.Rectangle.LLY = t.Rectangle.URY - theight
	switch {
	case strings.ContainsRune(t.Align, 'l'):
		// no change to t.Rectangle.LLX
	case strings.ContainsRune(t.Align, 'r'):
		t.Rectangle.LLX = t.Rectangle.URX - twidth
	default:
		t.Rectangle.LLX = (t.Rectangle.LLX + t.Rectangle.URX - twidth) / 2
	}
	t.Rectangle.URX = t.Rectangle.LLX + twidth
	// Draw each cell.
	for _, cell := range t.cells {
		t.drawCell(pdf, cell)
	}
	// Draw the border around the table, if requested.  We do this after
	// drawing the cells so that the table border overrides the cell borders
	// of the outer cells.
	if t.TableBorderWidth != 0 {
		if t.TableBorderStroke == nil {
			t.TableBorderStroke = []byte{0, 0, 0}
		}
		// Indent the rectangle by half of the stroke width.
		border := t.Rectangle
		border.LLX, border.LLY, border.URX, border.URY =
			border.LLX+t.TableBorderWidth/2, border.LLY+t.TableBorderWidth/2,
			border.URX-t.TableBorderWidth/2, border.URY-t.TableBorderWidth/2
		// Draw the rectangle.
		Box{Rectangle: border, Page: t.Page, Stroke: t.TableBorderStroke, StrokeWidth: t.TableBorderWidth}.Draw(pdf)
	}
	return nil
}

func (t *Table) drawCell(pdf *PDF, cell *Cell) {
	var crect Rectangle

	// Compute the top left corner of the cell.
	crect.LLX = t.Rectangle.LLX + t.TableBorderWidth
	crect.URY = t.Rectangle.URY - t.TableBorderWidth
	for i := 0; i < cell.Row; i++ {
		crect.URY -= t.heights[i]
	}
	if cell.Row > 0 {
		crect.URY -= t.CellBorderWidth * float64(cell.Row-1)
	}
	for i := 0; i < cell.Col; i++ {
		crect.LLX += t.widths[i]
	}
	if cell.Col > 0 {
		crect.LLX += t.CellBorderWidth * float64(cell.Col-1)
	}
	// Compute the bottom right corner of the cell.
	if cell.RowSpan < 1 {
		cell.RowSpan = 1
	}
	crect.LLY = crect.URY
	for i := cell.Row; i < cell.Row+cell.RowSpan; i++ {
		crect.LLY -= t.heights[i]
	}
	if cell.RowSpan > 1 {
		crect.LLY -= t.CellBorderWidth * float64(cell.RowSpan-1)
	}
	if cell.ColSpan < 1 {
		cell.ColSpan = 1
	}
	crect.URX = crect.LLX
	for i := cell.Col; i < cell.Col+cell.ColSpan; i++ {
		crect.URX += t.widths[i]
	}
	if cell.ColSpan > 1 {
		crect.URX += t.CellBorderWidth * float64(cell.ColSpan-1)
	}
	// If the cell has a border or a fill, draw that box.
	if cell.Fill != nil || t.CellBorderWidth != 0 {
		box := crect
		box.LLX, box.LLY, box.URX, box.URY =
			box.LLX-t.CellBorderWidth/2, box.LLY-t.CellBorderWidth/2,
			box.URX+t.CellBorderWidth/2, box.URY+t.CellBorderWidth/2
		if t.CellBorderWidth == 0 {
			t.CellBorderStroke = nil
		} else if t.CellBorderStroke == nil {
			t.CellBorderStroke = []byte{0, 0, 0}
		}
		Box{Page: t.Page, Rectangle: box, Fill: cell.Fill, Stroke: t.CellBorderStroke, StrokeWidth: t.CellBorderWidth}.Draw(pdf)
	}
	// Reduce the cell rectangle by the padding.
	crect.LLX, crect.LLY, crect.URX, crect.URY =
		crect.LLX+t.CellPadX, crect.LLY+t.CellPadY,
		crect.URX-t.CellPadX, crect.URY-t.CellPadY
	// Draw the text in the cell.
	cell.Text.Rectangle = crect
	cell.Text.Page = t.Page
	cell.Text.Draw(pdf)
}
