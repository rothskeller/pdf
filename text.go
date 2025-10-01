package pdf

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"
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
	// must be either "Courier", "Helvetica", or "Times-Roman".  Default is
	// "Helvetica".
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

	// lines is the String broken into lines, after any wrapping.
	lines []string
}

// ErrDoesntFit is the error returned by Draw if the text doesn't fit in its
// Rectangle.  The text was still drawn, either overflowing or clipped to the
// Rectangle depending on the Clip setting.
var ErrDoesntFit = errors.New("text does not fit in bounding box")

// ErrIllegalChar is the error returned by Draw if the text contains a character
// that cannot be rendered.  The text was still drawn but the offending
// character(s) were omitted.
var ErrIllegalChar = errors.New("text contains invalid character")

// Draw draws the text into the specified PDF.
func (t Text) Draw(pdf *PDF) (err error) {
	var (
		top      float64
		font     Name
		warnings error
		content  strings.Builder
	)
	if strings.TrimSpace(t.String) == "" {
		return nil // Streamline special case of empty string.
	}
	if t.String, warnings = utf8To1252(t.String); strings.TrimSpace(t.String) == "" {
		return warnings // nothing survived the conversion
	}
	if err = t.checkParameters(); err != nil {
		return err
	}
	warnings = errors.Join(warnings, t.wrapAndShrink())
	top = t.top()
	if font, err = t.addFont(pdf); err != nil {
		return err
	}
	t.emitSetup(&content, font)
	t.emitLines(&content, top)
	emitCleanup(&content)
	if err = pdf.AddPageContent(t.Page, content.String()); err != nil {
		return err
	}
	return warnings
}

func utf8To1252(ustr string) (cp1252 string, err error) {
	var sb strings.Builder
	for _, r := range ustr {
		if by, ok := charmap.Windows1252.EncodeRune(r); ok {
			sb.WriteByte(by)
		} else {
			err = ErrIllegalChar
		}
	}
	return sb.String(), err
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
	} else if _, ok := metrics[t.Font]; !ok {
		return errors.New("invalid Font (no metrics)")
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

// wrapAndShrink wraps the text and shrinks it to fit, if either is requested.
// The only error return is ErrDoesntFit, if the text cannot be made to fit the
// bounding box.
func (t *Text) wrapAndShrink() error {
	var (
		fitsX, fitsY bool
	)
	for {
		if fitsX, fitsY = t.fitText(); fitsX && fitsY {
			break
		}
		if t.FontSize-0.5 < t.MinFontSize {
			break
		}
		t.FontSize -= 0.5
	}
	if !fitsX {
		t.Align = t.Align[:1] + "l"
	}
	if !fitsY {
		t.Align = "t" + t.Align[1:]
	}
	if !fitsX || !fitsY {
		return ErrDoesntFit
	}
	return nil
}

// fitText determines whether the string fits in the box at the specified font
// and size, and how it got word-wrapped in order to fit.
func (t *Text) fitText() (fitsX, fitsY bool) {
	// Start by assuming it will fit, until we find out otherwise.
	var width = t.Rectangle.URX - t.Rectangle.LLX
	var height = t.Rectangle.URY - t.Rectangle.LLY
	var lh = t.LineHeight * t.FontSize
	fitsX, fitsY = true, true
	// Break the string up into lines and handle each one separately.
	t.lines = strings.Split(t.String, "\n")
	for i := 0; i < len(t.lines); i++ {
		var stop = len(t.lines[i])
		for {
			// Measure the line to see if it fits.
			if w, _, _ := measureText1252(t.lines[i][:stop], t.Font, t.FontSize); w > width {
				// It doesn't fit.  Is there a non-initial run
				// of spaces in it, such that we can word-wrap?
				if idx := strings.LastIndexByte(t.lines[i][:stop], ' '); idx > 0 && t.Wrap {
					for ; idx > 0 && t.lines[i][idx-1] == ' '; idx-- {
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
			// Remove the line's vertical size from bbox.
			height -= lh
			// If we had to take a tail off the line to word wrap,
			// put that into the slice as the next line, and remove
			// it from the current line.
			var rest int
			for rest = stop; rest < len(t.lines[i]) && t.lines[i][rest] == ' '; rest++ {
			}
			if rest < len(t.lines[i]) {
				t.lines = slices.Insert(t.lines, i+1, t.lines[i][rest:])
			}
			if stop < len(t.lines[i]) {
				t.lines[i] = t.lines[i][:stop]
			}
			// Move on to the next line.
			break
		}
	}
	// For the last line, use the minimum of the font height and the line
	// height.
	height = height + lh - min(lh, t.FontSize)
	// Did the value fit vertically?
	if height < 0 {
		fitsY = false
	}
	// Return the result.
	return fitsX, fitsY
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
func (t *Text) top() (bl1 float64) {
	var habove, hbelow, height float64

	// For uppercase, use the font metrics for top line ascender and bottom
	// line descender.  For lowercase, use the actual line contents.
	if t.Align[0] < 'a' {
		habove, hbelow = FontMetrics(t.Font, t.FontSize)
	} else {
		_, habove, _ = measureText1252(t.lines[0], t.Font, t.FontSize)
		_, _, hbelow = measureText1252(t.lines[len(t.lines)-1], t.Font, t.FontSize)
	}
	// In either case, use the line height for all intervening lines.
	height = float64(len(t.lines)-1)*t.LineHeight*t.FontSize + habove + hbelow
	// Compute the baseline of the first line.
	if t.Baseline != 0 {
		bl1 = t.Baseline
	} else {
		switch t.Align[0] {
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
	if t.Align[0] < 'a' {
		_, habove, _ = measureText1252(t.lines[0], t.Font, t.FontSize)
		_, _, hbelow = measureText1252(t.lines[len(t.lines)-1], t.Font, t.FontSize)
		height = float64(len(t.lines)-1)*t.LineHeight*t.FontSize + habove + hbelow
	}
	// If the result extends below the bottom of the rectangle, shift it up.
	if bl1+habove-height < t.Rectangle.LLY {
		bl1 = t.Rectangle.LLY + height - habove
		println("shifting ", t.lines[0], " up")
	}
	// If the result extends above the top of the rectangle, shift it down.
	if bl1+habove > t.Rectangle.URY {
		bl1 = t.Rectangle.URY - habove
		println("shifting ", t.lines[0], " down")
	}
	return bl1
}

// addFont ensures that the font is in the page's resource dictionary, and
// returns the name by which it's known there.
func (t *Text) addFont(pdf *PDF) (name Name, err error) {
	var (
		fonts  Dict
		maxnum int
		path   Path
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
	for n := range fonts {
		if font, err := pdf.GetDict(path.K(n)); err != nil {
			return "", err
		} else if font["Type"] == Name("Font") &&
			font["Subtype"] == Name("Type1") &&
			font["BaseFont"] == Name(t.Font) &&
			font["Encoding"] == Name("WinAnsiEncoding") {
			return n, nil
		}
		if strings.HasPrefix(string(n), "F") {
			if num, err := strconv.Atoi(string(n[1:])); err == nil {
				maxnum = max(maxnum, num)
			}
		}
	}
	// Not found, need to add it.
	name = Name(fmt.Sprintf("F%d", maxnum+1))
	fonts[name] = Dict{
		"Type":     Name("Font"),
		"Subtype":  Name("Type1"),
		"BaseFont": Name(t.Font),
		"Encoding": Name("WinAnsiEncoding"),
	}
	if err = pdf.Set(path, fonts); err != nil {
		return "", err
	}
	return name, nil
}

// emitSetup emits all of the preliminary content instructions before writing
// the lines.
func (t *Text) emitSetup(sb *strings.Builder, font Name) {
	sb.WriteString("q")
	if t.Clip {
		fmt.Fprintf(sb, " %.2f %.2f %.2f %.2f re W n",
			t.Rectangle.LLX, t.Rectangle.LLY,
			t.Rectangle.URX-t.Rectangle.LLX, t.Rectangle.URY-t.Rectangle.LLY)
	}
	if t.Color[0] != 0 || t.Color[1] != 0 || t.Color[2] != 0 {
		fmt.Fprintf(sb, " %.2f %.2f %.2f rg", float64(t.Color[0])/255, float64(t.Color[1])/255, float64(t.Color[2])/255)
	}
	fmt.Fprintf(sb, " BT %s %.2f Tf", EncodeName(font), t.FontSize)
}

// emitLines emits all of the lines of text.
func (t *Text) emitLines(sb *strings.Builder, top float64) {
	var (
		prevLeft float64
		yOffset  = top
	)
	for _, line := range t.lines {
		var left float64

		switch t.Align[1] {
		case 'c':
			width, _, _ := measureText1252(line, t.Font, t.FontSize)
			left = (t.Rectangle.LLX+t.Rectangle.URX)/2 - width/2
		case 'r':
			width, _, _ := measureText1252(line, t.Font, t.FontSize)
			left = t.Rectangle.URX - width
		default: // 'l'
			left = t.Rectangle.LLX
		}
		fmt.Fprintf(sb, " %.2f %.2f Td %s Tj", left-prevLeft, yOffset, EncodeString(line))
		prevLeft = left
		yOffset = -t.FontSize * t.LineHeight
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
	s, _ = utf8To1252(s)
	return measureText1252(s, font, size)
}

// measureText1252 returns the metrics of the specified string in the specified
// font at the specified size: specifically, the width, the height above the
// baseline, and the height below the baseline.  The string must not contain
// control characters, and must use Windows-1252 encoding.  The function returns
// zeros if the font is not known.
func measureText1252(s, font string, size float64) (width, habove, hbelow float64) {
	w, ha, hb := measure(s, font)
	return float64(w) * size / 1000.0, float64(ha) * size / 1000.0, float64(hb) * size / 1000.0
}

func measure(s, font string) (width, habove, hbelow int) {
	fm := metrics[font]
	if fm == nil {
		return 0, 0, 0
	}
	for s != "" {
		var cm [3]int16
		if len(s) > 1 {
			var key = [2]byte{s[0], s[1]}
			if cm = fm.ligatures[key]; cm[0] != 0 {
				s = s[2:]
			}
			if cm[0] == 0 {
				width += int(fm.kernpairs[key])
			}
		}
		if cm[0] == 0 && s[0] >= 32 {
			cm = fm.chars[s[0]-32]
			s = s[1:]
		} else if cm[0] == 0 {
			cm = fm.chars['x'-32]
			s = s[1:]
		}
		width += int(cm[0])
		hbelow = min(hbelow, int(cm[1]))
		habove = max(habove, int(cm[2]))
	}
	hbelow = -hbelow
	return
}
