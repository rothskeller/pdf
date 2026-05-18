package pdf

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

// Text is a structure containing all of the parameters for drawing a text
// string.  To draw a text string, create a Text structure and call its Draw
// method.
type Text struct {
	// String is the string to be drawn.  Required.  The string should be
	// encoded in UTF-8 as normal; however, only characters in the Windows
	// 1252 character set (a superset of ISO 8859-1) will actually be
	// rendered.
	String string
	// Rectangle is the page area into which to draw the string.  Required.
	Rectangle Rectangle
	// Page is the page number onto which to draw the string.  Default 1.
	Page int
	// Baseline, if nonzero, is the Y-coordinate of the baseline for the
	// first line of text.  This overrides any alignment specified in Align
	// or VAlign.
	Baseline float64
	// Font is the name of font in which the string should be drawn.  It
	// must be "Courier", "Helvetica", "Times-Roman", "Go", or "Go Mono".
	// Default is "Helvetica".
	Font string
	// FontSize is the starting and maximum size of the font with which the
	// string should be drawn.  Default 12.0.
	FontSize float64
	// MinFontSize is the minimum size of the font with which the string
	// should be drawn.  Defaults to FontSize.  If less than FontSize, text
	// will be shrunk to fit within the Rectangle, but no smaller than
	// MinFontSize.
	MinFontSize float64
	// LineHeight is the distance between lines in a multi-line text block
	// (either String contains newlines or Wrap is enabled and String gets
	// wrapped).  Specified as a multiple of the font size, default 1.0.
	LineHeight float64
	// Color is the color in which the text should be drawn, as a slice of
	// three byte values, one each for red, green, and blue.  Default is
	// black (i.e., 0, 0, 0).
	Color []byte
	// Align specifies the alignment of the text within the rectangle.  It
	// contains up to two letters, one specifying the vertical alignment and
	// the other specifying the horizontal alignment.  The order of the two
	// does not matter.
	//
	// The vertical alignment characters are T, M, F, B, t, m, f, and b.
	// These stand for top, middle, first-middle, and bottom respectively.
	// The lowercase versions align based on the actual text (i.e., what
	// ascenders and descenders are actually used); the uppercase versions
	// align based on the font bounding box (i.e., assuming full ascenders
	// and descenders even if the text being rendered doesn't use them).
	// M/m differs from F/f only for multi-line text:  M/m centers the whole
	// block and F/f centers the first line.  If no vertical alignment
	// character is specified, the default is F.  Note that an nonzero value
	// of Baseline overrides any vertical alignment character.
	//
	// The horizontal alignment characters are l, c, and r, for left,
	// center, and right.  If no horizontal alignment character is
	// specified, the default is l.
	Align string
	// HAlign indicates how the text should be aligned horizontally.
	// Allowed values are "left" (the default), "center", and "right".
	// Deprecated:  use Align instead.
	HAlign string
	// VAlign indicates how the text hsould be aligned vertically.  Allowed
	// values are "top", "center", "bottom", and "baseline" (the default).
	// "baseline" means to arrange for the baseline of the first line of
	// text to be at Baseline, shifting that up as needed to make the text
	// fit in Rectangle.  Deprecated: use Align instead.
	VAlign string
	// Wrap indicates that text should be wrapped to fit in the Rectangle.
	Wrap bool
	// Clip indicates that text should be clipped to the Rectangle.  If
	// false, text that doesn't fit in the Rectangle is allowed to extend
	// past its boundaries.
	Clip bool

	// fh is the font handler.
	fh fontHandler
	// lines is the String broken into lines, after any wrapping.
	lines []string
	// height is the total vertical height used
	height float64
}

// ErrTextRendering returns one or more problems that prevent the text from
// being rendered faithfully.
type ErrTextRendering struct {
	DoesntFitX  bool
	DoesntFitY  bool
	IllegalChar bool
}

func (e ErrTextRendering) Error() string {
	var s []string

	if e.DoesntFitX {
		s = append(s, "text is wider than bounding box")
	}
	if e.DoesntFitY {
		s = append(s, "text is taller than bounding box")
	}
	if e.IllegalChar {
		s = append(s, "text contains illegal characters")
	}
	return strings.Join(s, "; ")
}

func (e ErrTextRendering) AsError() error {
	if e.DoesntFitX || e.DoesntFitY || e.IllegalChar {
		return e
	}
	return nil
}

func (e *ErrTextRendering) Merge(o ErrTextRendering) {
	if o.DoesntFitX {
		e.DoesntFitX = true
	}
	if o.DoesntFitY {
		e.DoesntFitY = true
	}
	if o.IllegalChar {
		e.IllegalChar = true
	}
}

// WrapText word-wraps the text as needed to fit into the width of the supplied
// rectangle.  It makes no changes to the Text object.  It returns the String
// in word-wrapped form, broken into two strings:  the part that fits
// vertically in the text rectangle and the part that doesn't.  It returns the
// resolved font size and any text rendering issues.
func (t Text) WrapText() (wrappedText, overflowText string, fontSize float64, err error) {
	var (
		s             string
		warnings      ErrTextRendering
		wrappedLines  []string
		overflowLines []string
		fitsX         bool
		ok            bool
	)
	if err = t.checkParameters(); err != nil {
		return "", "", t.FontSize, err
	}
	if s, ok = t.fh.replaceInvalidChars(t.String, true); !ok {
		warnings.IllegalChar = true
	}
	if s == "" {
		return "", "", t.FontSize, warnings.AsError()
	}
	wrappedLines, overflowLines, fontSize, fitsX = t.wrapText(s)
	wrappedText = strings.Join(wrappedLines, "\n")
	overflowText = strings.Join(overflowLines, "\n")
	if !fitsX {
		warnings.DoesntFitX = true
	}
	if overflowText != "" {
		warnings.DoesntFitY = true
	}
	return wrappedText, overflowText, fontSize, warnings.AsError()
}

// wrapText wraps the supplied string into lines using the Text's bounding box,
// font, and font size range.  It returns the set of lines that fit vertically
// in the bounding box, the set that don't, the resolved font size, and a flag
// indicating whether all of the lines fit horizontally.
func (t Text) wrapText(s string) (lines, overflow []string, fontSize float64, fitsX bool) {
	fontSize = t.FontSize
	for {
		lines, overflow, fitsX = t.wrapTextAt(s, fontSize)
		if fitsX && len(overflow) == 0 {
			break
		}
		if fontSize-0.5 < t.MinFontSize {
			break
		}
		fontSize -= 0.5
	}
	return lines, overflow, fontSize, fitsX
}

// wrapTextAt wraps the supplied string into lines using the Text's bounding box
// and font and the supplied font size.  It returns the set of lines that fit
// vertically in the bounding box, the set that don't, and a flag indicating
// whether all of the lines fit horizontally.
func (t Text) wrapTextAt(s string, fontSize float64) (lines, overflow []string, fitsX bool) {
	// Start by assuming it will fit, until we find out otherwise.
	var width = t.Rectangle.URX - t.Rectangle.LLX
	var height = t.Rectangle.URY - t.Rectangle.LLY
	var lh = t.LineHeight * fontSize
	fitsX = true
	// Break the string up into lines and handle each one separately.
	lines = strings.Split(s, "\n")
	for i := 0; i < len(lines); i++ {
		var stop = len(lines[i])
		for {
			// Measure the line to see if it fits.
			if w, _, _ := measureText(lines[i][:stop], t.fh, fontSize); w > width {
				// It doesn't fit.  Is there a non-initial run
				// of spaces in it, such that we can word-wrap?
				if idx := strings.LastIndexByte(lines[i][:stop], ' '); idx > 0 && t.Wrap {
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
				fitsX = false
			}
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
	// How many lines can fit in the box?
	linesThatFit := int(math.Floor((height + max(0, lh-fontSize)) / lh))
	// If we have more than that, move some into overflow.
	if len(lines) > linesThatFit {
		lines, overflow = lines[:linesThatFit], lines[linesThatFit:]
	}
	return lines, overflow, fitsX
}

// Draw draws the text into the specified PDF.
func (t Text) Draw(pdf *PDF) (err error) {
	var (
		s        string
		lines    []string
		overflow []string
		fontSize float64
		align    string
		fitsX    bool
		top      float64
		font     Name
		warnings ErrTextRendering
		content  strings.Builder
		ok       bool
	)
	if err = t.checkParameters(); err != nil {
		return err
	}
	if strings.TrimSpace(t.String) == "" {
		return nil // Streamline special case of empty string.
	}
	if s, ok = t.fh.replaceInvalidChars(t.String, true); !ok {
		warnings.IllegalChar = true
	}
	if strings.TrimSpace(s) == "" {
		return warnings.AsError() // nothing survived the conversion
	}
	lines, overflow, fontSize, fitsX = t.wrapText(s)
	align = t.Align
	if !fitsX {
		align = align[:1] + "l"
		warnings.DoesntFitX = true
	}
	if len(overflow) != 0 {
		align = "t" + align[1:]
		warnings.DoesntFitY = true
		if !t.Clip {
			lines = append(lines, overflow...)
		}
	}
	top = t.top(lines, fontSize, align)
	if font, err = t.addFont(pdf); err != nil {
		return err
	}
	t.emitSetup(&content, font, fontSize)
	t.emitLines(pdf, &content, lines, fontSize, top, align)
	emitCleanup(&content)
	if err = pdf.AddPageContent(t.Page, content.String()); err != nil {
		return err
	}
	return warnings.AsError()
}

func (t *Text) checkParameters() error {
	// Check parameters and apply defaults.
	if t.Rectangle.LLX >= t.Rectangle.URX || t.Rectangle.LLY >= t.Rectangle.URY {
		return errors.New("invalid Rectangle")
	}
	if t.Page == 0 {
		t.Page = 1
	} else if t.Page < 0 {
		return errors.New("invalid Page")
	}
	if t.Baseline != 0 && (t.Baseline < t.Rectangle.LLY || t.Baseline >= t.Rectangle.URY) {
		return errors.New("invalid Baseline")
	}
	if t.Font == "" {
		t.Font = "Helvetica"
	}
	t.Font = strings.ReplaceAll(t.Font, " ", "")
	if t.fh = fontHandlers[t.Font]; t.fh == nil {
		return errors.New("invalid Font")
	}
	if t.FontSize == 0 {
		t.FontSize = 12.0
	} else if t.FontSize < 0 {
		return errors.New("invalid FontSize")
	}
	if t.MinFontSize == 0 {
		t.MinFontSize = t.FontSize
	} else if t.MinFontSize > t.FontSize || t.MinFontSize < 0 {
		return errors.New("invalid MinFontSize")
	}
	if t.LineHeight == 0 {
		t.LineHeight = 1.0
	} else if t.LineHeight < 0 {
		return errors.New("invalid LineHeight")
	}
	if t.Color == nil {
		t.Color = []byte{0, 0, 0}
	} else if len(t.Color) != 3 {
		return errors.New("invalid Color")
	}
	if t.Align != "" {
		var vert, horiz rune
		for _, r := range t.Align {
			switch r {
			case 'T', 't', 'M', 'm', 'F', 'f', 'B', 'b':
				if vert != 0 {
					return errors.New("invalid Align")
				} else {
					vert = r
				}
			case 'l', 'c', 'r':
				if horiz != 0 {
					return errors.New("invalid Align")
				} else {
					horiz = r
				}
			default:
				return errors.New("invalid Align")
			}
		}
		if vert == 0 {
			vert = 'F'
		}
		if horiz == 0 {
			horiz = 'l'
		}
		t.Align = string(vert) + string(horiz)
		if t.HAlign != "" || t.VAlign != "" {
			return errors.New("can't mix Align and HAlign/VAlign")
		}
	} else {
		var vert, horiz rune

		if t.HAlign == "" {
			horiz = 'l'
		} else if t.HAlign != "left" && t.HAlign != "center" && t.HAlign != "right" {
			return errors.New("invalid HAlign")
		} else {
			horiz = rune(t.HAlign[0])
		}
		switch t.VAlign {
		case "", "baseline":
			vert = 'F'
		case "top":
			vert = 't'
		case "center":
			vert = 'm'
		case "bottom":
			vert = 'b'
		default:
			return errors.New("invalid VAlign")
		}
		t.Align = string(vert) + string(horiz)
	}
	return nil
}

// "top" aligns the top of the actual text content (i.e., the tallest ascender
// of the top line) to the top of the rectangle.  "center" aligns the center of
// the actual text content to the center of the rectangle, i.e., the top of the
// tallest ascender of the first line and the bottom of the lowest descender of
// the last line are equidistant from the center.  "bottom" aligns the bottom of
// the actual text (i.e., the lowest descender of the last line) content to the
// bottom of the rectangle.
//
// "baseline" behaves differently depending on whether Baseline is specified.
// If it is, the baseline of the first line of text is aligned there.  If it's
// not, the actual text is ignored, and the font bounding box of the first line
// is centered around the center of the rectangle.  Multiple Text entries with
// the same T and B and no BL will have the same baseline and their first lines
// will be roughly centered.

// top figures out where to start vertically.  It returns the Y-coordinate of
// the baseline of the first line of text.
func (t *Text) top(lines []string, fontSize float64, align string) (bl1 float64) {
	var habove, hbelow, height float64

	// For uppercase, use the font metrics for top line ascender and bottom
	// line descender.  For lowercase, use the actual line contents.
	if align[0] < 'a' {
		habove, hbelow = FontMetrics(t.Font, fontSize)
	} else {
		_, habove, _ = measureText(lines[0], t.fh, fontSize)
		_, _, hbelow = measureText(lines[len(lines)-1], t.fh, fontSize)
	}
	// In either case, use the line height for all intervening lines.
	height = float64(len(lines)-1)*t.LineHeight*fontSize + habove + hbelow
	// Compute the baseline of the first line.
	if t.Baseline != 0 {
		bl1 = t.Baseline
	} else {
		switch align[0] {
		case 'T', 't':
			bl1 = t.Rectangle.URY - habove
		case 'M', 'm':
			bl1 = (t.Rectangle.LLY+t.Rectangle.URY)/2 + height/2 - habove
		case 'F', 'f':
			bl1 = (t.Rectangle.LLY+t.Rectangle.URY)/2 + (habove+hbelow)/2 - habove
		case 'B', 'b':
			bl1 = t.Rectangle.LLY + height - habove
		}
	}
	// For verifying that it fits within the rectangle, we always want to
	// use the actual line contents, even if we weren't before.
	if align[0] < 'a' {
		_, habove, _ = measureText(lines[0], t.fh, fontSize)
		_, _, hbelow = measureText(lines[len(lines)-1], t.fh, fontSize)
		height = float64(len(lines)-1)*t.LineHeight*fontSize + habove + hbelow
	}
	// If the result extends below the bottom of the rectangle, shift it up.
	if bl1+habove-height < t.Rectangle.LLY {
		bl1 = t.Rectangle.LLY + height - habove
	}
	// If the result extends above the top of the rectangle, shift it down.
	if bl1+habove > t.Rectangle.URY {
		bl1 = t.Rectangle.URY - habove
	}
	return bl1
}

// addFont ensures that the font is in the page's resource dictionary, and
// returns the name by which it's known there.
func (t *Text) addFont(pdf *PDF) (name Name, err error) {
	var (
		fonts Dict
		path  Path
	)
	if path, err = pdf.PagePath(t.Page); err != nil {
		return "", err
	}
	path = path.K("Resources").K("Font")
	switch obj := pdf.Get(path).(type) {
	case error:
		return "", err
	case nil:
		fonts = make(Dict)
	case Dict:
		fonts = obj
	default:
		return "", fmt.Errorf("%s is %T, not Dict or nil", path, obj)
	}
	if name, err = t.fh.addFontToPageResources(pdf, path, fonts); err != nil {
		return "", err
	}
	if err = pdf.Set(path, fonts); err != nil {
		return "", err
	}
	return name, nil
}

// emitSetup emits all of the preliminary content instructions before writing
// the lines.
func (t *Text) emitSetup(sb *strings.Builder, font Name, fontSize float64) {
	sb.WriteString("q")
	if t.Clip {
		fmt.Fprintf(sb, " %.2f %.2f %.2f %.2f re W n",
			t.Rectangle.LLX, t.Rectangle.LLY,
			t.Rectangle.URX-t.Rectangle.LLX, t.Rectangle.URY-t.Rectangle.LLY)
	}
	if t.Color[0] != 0 || t.Color[1] != 0 || t.Color[2] != 0 {
		fmt.Fprintf(sb, " %.2f %.2f %.2f rg", float64(t.Color[0])/255, float64(t.Color[1])/255, float64(t.Color[2])/255)
	}
	fmt.Fprintf(sb, " BT %s %.2f Tf", EncodeName(font), fontSize)
}

// emitLines emits all of the lines of text.
func (t *Text) emitLines(pdf *PDF, sb *strings.Builder, lines []string, fontSize, top float64, align string) {
	var (
		prevLeft float64
		yOffset  = top
	)
	for _, line := range lines {
		var left float64
		width, _, hbelow := measureText(line, t.fh, fontSize)
		if top-hbelow < t.Rectangle.LLY-0.1 && t.Clip {
			return
		}
		switch align[1] {
		case 'c':
			left = (t.Rectangle.LLX+t.Rectangle.URX)/2 - width/2
		case 'r':
			left = t.Rectangle.URX - width
		default: // 'l'
			left = t.Rectangle.LLX
		}
		fmt.Fprintf(sb, " %.2f %.2f Td %s Tj", left-prevLeft, yOffset, t.fh.encodeString(pdf, line))
		prevLeft = left
		yOffset = -fontSize * t.LineHeight
		top += yOffset
	}
}

// emitCleanup emits all of the cleanup instructions after writing the lines.
func emitCleanup(sb *strings.Builder) { sb.WriteString(" ET Q") }

// MeasureText returns the metrics of the specified string in the specified font
// at the specified size: specifically, the width, the height above the
// baseline, and the height below the baseline.  The string must not contain
// control characters, and must be in UTF-8 encoding.  Characters that are not
// recognized (i.e., not in Windows-1252 encoding) are ignored.  The function
// returns zeros if the font is not known.
func MeasureText(s, font string, size float64) (width, habove, hbelow float64) {
	fh := fontHandlers[font]
	if fh == nil {
		return 0, 0, 0
	}
	s, _ = fh.replaceInvalidChars(s, false)
	w, ha, hb := fh.measure(s)
	return float64(w) * size / 1000.0, float64(ha) * size / 1000.0, float64(hb) * size / 1000.0
}

// measureText returns the metrics of the specified string in the specified
// font at the specified size: specifically, the width, the height above the
// baseline, and the height below the baseline.
func measureText(s string, fh fontHandler, size float64) (width, habove, hbelow float64) {
	w, ha, hb := fh.measure(s)
	return float64(w) * size / 1000.0, float64(ha) * size / 1000.0, float64(hb) * size / 1000.0
}
