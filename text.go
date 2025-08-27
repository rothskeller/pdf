package pdf

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Text is a structure containing all of the parameters for drawing a text
// string.  To draw a text string, create a Text structure and call its Draw
// method.
type Text struct {
	// String is the string to be drawn.  Required.
	String string
	// Rectangle is the page area into which to draw the string.  Required.
	Rectangle Rectangle
	// Page is the page number onto which to draw the string.  Default 1.
	Page int
	// Baseline is the Y-coordinate of the baseline for the first line of
	// text, used when VAlign is "baseline".  The default is such that the
	// first line is vertically centered in the Rectangle, taking into
	// account its ascenders and descenders.
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
	// HAlign indicates how the text should be aligned horizontally.
	// Allowed values are "left" (the default), "center", and "right".
	HAlign string
	// VAlign indicates how the text hsould be aligned vertically.  Allowed
	// values are "top", "center", "bottom", and "baseline" (the default).
	// "baseline" means to arrange for the baseline of the first line of
	// text to be at Baseline, shifting that up as needed to make the text
	// fit in Rectangle.
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
// Rectangle.  The text was still drawn, either overfloawing or clipped to the
// Rectangle depending on the Clip setting.
var ErrDoesntFit = errors.New("text does not fit in bounding box")

// Draw draws the text into the specified PDF.
func (t Text) Draw(pdf *PDF) (err error) {
	var (
		top     float64
		font    Name
		fitErr  error
		content strings.Builder
	)
	if strings.TrimSpace(t.String) == "" {
		return nil // Streamline special case of empty string.
	}
	if err = t.checkParameters(); err != nil {
		return err
	}
	fitErr = t.wrapAndShrink()
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
	return fitErr
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
	if t.HAlign == "" {
		t.HAlign = "left"
	} else if t.HAlign != "left" && t.HAlign != "center" && t.HAlign != "right" {
		return errors.New("invalid HAlign")
	}
	if t.VAlign == "" {
		t.VAlign = "baseline"
	} else if t.VAlign != "top" && t.VAlign != "center" && t.VAlign != "bottom" && t.VAlign != "baseline" {
		return errors.New("invalid VAlign")
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
		t.HAlign = "left"
	}
	if !fitsY {
		t.VAlign = "top"
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
			if w, _, _ := MeasureText(t.lines[i][:stop], t.Font, t.FontSize); w > width {
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

// top figures out where to start vertically.  It returns the Y-coordinate of
// the baseline of the first line of text.
func (t *Text) top() float64 {
	_, habove, hbelow1 := MeasureText(t.lines[0], t.Font, t.FontSize)
	_, _, hbelow := MeasureText(t.lines[len(t.lines)-1], t.Font, t.FontSize)
	height := float64(len(t.lines)-1)*t.LineHeight + habove + hbelow
	switch t.VAlign {
	case "top":
		return t.Rectangle.URY - habove
	case "center":
		return (t.Rectangle.LLY+t.Rectangle.URY)/2 + height/2 - habove
	case "baseline":
		if t.Baseline == 0 {
			t.Baseline = (t.Rectangle.LLY+t.Rectangle.URY)/2 + (hbelow1-habove)/2
		}
		if t.Baseline+habove-height >= t.Rectangle.LLY {
			return t.Baseline
		}
		fallthrough
	case "bottom":
		return t.Rectangle.LLY + height - habove
	}
	panic("not reachable")
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
			font["BaseFont"] == Name(t.Font) {
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
		fmt.Fprintf(sb, " %d %d %d rg", t.Color[0], t.Color[1], t.Color[2])
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

		switch t.HAlign {
		case "center":
			width, _, _ := MeasureText(line, t.Font, t.FontSize)
			left = (t.Rectangle.LLX+t.Rectangle.URX)/2 - width/2
		case "right":
			width, _, _ := MeasureText(line, t.Font, t.FontSize)
			left = t.Rectangle.URX - width
		default: // "left"
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
// newlines.  Characters that are not recognized (i.e., any non-ASCII
// characters) are given the size of an 'x', so the result could be imprecise if
// such characters are included.  The function returns zeros if the font is not
// known.
func MeasureText(s, font string, size float64) (width, habove, hbelow float64) {
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
		if cm[0] == 0 && s[0] >= 32 && s[0] <= 126 {
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
