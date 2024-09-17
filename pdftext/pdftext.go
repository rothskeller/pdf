// Package pdftext provides methods for adding text to PDF files opened with
// gofpdf, with styling, alignment, and wrapping.  It relies on font metrics,
// and has embedded font metrics for Courier, Helvetica, and Times-Roman (ASCII
// characters only).
package pdftext

import (
	"fmt"
	"slices"
	"strings"

	"github.com/phpdave11/gofpdf"
)

// Style is the style for drawing a text string.
type Style struct {
	Font        string  // default = "Helvetica"
	FontSize    float64 // default = 12.0
	MinFontSize float64 // default = no shrink to fit
	LineHeight  float64 // as a multiple of font size, default = 1.0
	Color       []byte  // default = black
	HAlign      string  // "left" (default), "center", "right"
	VAlign      string  // "center" (default), "baseline", "top"
	Wrap        int8    // >0 = true, <=0 = false
	Clip        int8    // >0 = true, <=0 = false
}

func (s Style) Merge(o Style) Style {
	if o.Font != "" {
		s.Font = o.Font
	}
	if o.FontSize != 0 {
		s.FontSize = o.FontSize
	}
	if o.MinFontSize != 0 {
		s.MinFontSize = o.MinFontSize
	}
	if o.LineHeight != 0 {
		s.LineHeight = o.LineHeight
	}
	if o.Color != nil {
		s.Color = o.Color
	}
	if o.HAlign != "" {
		s.HAlign = o.HAlign
	}
	if o.VAlign != "" {
		s.VAlign = o.VAlign
	}
	if o.Wrap != 0 {
		s.Wrap = o.Wrap
	}
	if o.Clip != 0 {
		s.Clip = o.Clip
	}
	return s
}

// Draw draws the string into specified box on the current page of the PDF with
// the specified style.  It returns whether the string fit in its box.
func Draw(pdf *gofpdf.Fpdf, s string, x, y, w, h float64, style Style) (fits bool) {
	var (
		lh    float64
		lines []string
		top   float64
	)
	// Streamline special case of empty string.
	if strings.TrimSpace(s) == "" {
		return true
	}
	// We need font metrics.
	if style.Font == "" {
		style.Font = "Helvetica"
	}
	if _, ok := metrics[style.Font]; !ok {
		panic(fmt.Sprintf("no font metrics for %q", style.Font))
	}
	if style.FontSize == 0 {
		style.FontSize = 12
	}
	// Wrap the text and shrink to fit, if either is requested.
	for {
		lh = style.LineHeight * style.FontSize
		if lh == 0 {
			lh = style.FontSize
		}
		if lines, fits = fitText(s, style.Font, w, h, style.FontSize, lh, style.Wrap > 0); fits {
			break
		}
		if style.MinFontSize == 0 || style.FontSize-0.5 < style.MinFontSize {
			break
		}
		style.FontSize -= 0.5
	}
	if !fits {
		style.VAlign = "top"
	}
	// Figure out where to start vertically.
	switch style.VAlign {
	case "top":
		_, habove, _ := Measure(lines[0], style.Font, style.FontSize)
		top = y + habove
	case "baseline":
		habove, hbelow := FontMetrics(style.Font, style.FontSize)
		height := float64(len(lines)-1)*lh + habove + hbelow
		top = y + (h-height)/2 + habove
	default: // "center"
		_, habove, _ := Measure(lines[0], style.Font, style.FontSize)
		_, _, hbelow := Measure(lines[len(lines)-1], style.Font, style.FontSize)
		height := float64(len(lines)-1)*lh + habove + hbelow
		top = y + (h-height)/2 + habove
	}
	// Set up for drawing.
	pdf.SetFont(style.Font, "", style.FontSize)
	if style.Color != nil {
		pdf.SetTextColor(int(style.Color[0]), int(style.Color[1]), int(style.Color[2]))
	} else {
		pdf.SetTextColor(0, 0, 0)
	}
	if style.Clip > 0 {
		pdf.ClipRect(x, y, w, h, false)
	}
	// Draw the lines.
	for _, line := range lines {
		var left float64

		switch style.HAlign {
		case "center":
			width, _, _ := Measure(line, style.Font, style.FontSize)
			left = x + (w-width)/2
		case "right":
			width, _, _ := Measure(line, style.Font, style.FontSize)
			left = x + w - width
		default: // "left"
			left = x
		}
		pdf.Text(left, top, line)
		top += lh
	}
	if style.Clip > 0 {
		pdf.ClipEnd()
	}
	return fits
}

// fitText determines whether the string fits in the box at the specified font
// and size, and how it got word-wrapped in order to fit.
func fitText(s, font string, w, h, sz, lh float64, wrap bool) (lines []string, fits bool) {
	// Streamline special case of empty string.
	if s == "" {
		return nil, true
	}
	// Start by assuming it will fit, until we find out otherwise.
	var height = h
	fits = true
	// Break the string up into lines and handle each one separately.
	lines = strings.Split(s, "\n")
	for i := 0; i < len(lines); i++ {
		var stop = len(lines[i])
		for {
			// Measure the line to see if it fits.
			if width, _, _ := Measure(lines[i][:stop], font, sz); width > w {
				// It doesn't fit.  Is there a non-initial run
				// of spaces in it, such that we can word-wrap?
				if idx := strings.LastIndexByte(lines[i][:stop], ' '); idx > 0 && wrap {
					for ; idx > 0 && lines[i][idx-1] == ' '; idx-- {
					}
					if idx > 0 {
						// Yes.  Stop the line at that
						// point and try again.
						stop = idx
						continue
					}
				}
				// Can't word wrap (any further).  The whole
				// value will not fit.  We'll accept truncating
				// this line, but we'll still continue laying
				// out the rest of the lines to do the best we
				// can.
				fits = false
			}
			// Remove the line's vertical size from bbox.
			height -= lh
			// If we had to take a tail off the line to word wrap,
			// put that into the slice as the next line, and remove
			// it from the current line.
			var rest int
			for rest = stop; rest < len(lines[i]) && lines[i][rest] == ' '; rest++ {
			}
			if rest < len(lines[i]) {
				lines = slices.Insert(lines, i+1, lines[i][rest:])
			}
			if stop < len(lines[i]) {
				lines[i] = lines[i][:stop]
			}
			// Move on to the next line.
			break
		}
	}
	// For the last line, use the minimum of the font height and the line
	// height.
	height = height + lh - min(lh, sz)
	// Did the value fit vertically?
	if height < 0 {
		fits = false
	}
	// Return the result.
	return lines, fits
}
