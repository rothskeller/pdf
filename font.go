package pdf

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// A fontHandler is a type that can handle rendering a font.
type fontHandler interface {
	// replaceInvalidChars ensures that all characters in the supplied
	// string are valid for the font.  It returns a possibly-adjusted
	// string, and a flag indicating whether the original string was OK.
	replaceInvalidChars(s string) (out string, ok bool)
	// measure returns the measurement of the string in the font, which must
	// be a single line.  The string must contain only characters valid for
	// the font (e.g., replaceInvalidChars returned it).  The return values
	// are in font units (i.e., 1/1000pt).  The returns are the width of the
	// string, its height above the baseline, and its descent below the
	// baseline.
	measure(s string) (width, habove, hbelow int)
	// metrics returns the height above and descent below the baseline for
	// the font in general, in font units (i.e., 1/1000pt).
	metrics() (habove, hbelow int)
	// addFontToPageResources ensures that the font is in the supplied page
	// resource fonts dictionary at the supplied path, and returns the name
	// by which it's known there.
	addFontToPageResources(pdf *PDF, path Path, fonts Dict) (name Name, err error)
	// encodeString renders the string in the proper form for a Tj operator.
	encodeString(s string) string
}

// fontHandlers contains the font handlers for all known fonts.
var fontHandlers = map[string]fontHandler{
	"Courier":           standardFontHandler{"Courier", metrics["Courier"]},
	"Courier-Bold":      standardFontHandler{"Courier-Bold", metrics["Courier-Bold"]},
	"Courier-Oblique":   standardFontHandler{"Courier-Oblique", metrics["Courier-Oblique"]},
	"Helvetica":         standardFontHandler{"Helvetica", metrics["Helvetica"]},
	"Helvetica-Bold":    standardFontHandler{"Helvetica-Bold", metrics["Helvetica-Bold"]},
	"Helvetica-Oblique": standardFontHandler{"Helvetica-Oblique", metrics["Helvetica-Oblique"]},
	"Times-Bold":        standardFontHandler{"Times-Bold", metrics["Times-Bold"]},
	"Times-Italic":      standardFontHandler{"Times-Italic", metrics["Times-Italic"]},
	"Times-Roman":       standardFontHandler{"Times-Roman", metrics["Times-Roman"]},
}

// FontMetrics returns the maximum height above the baseline and below the
// baseline at the specified font size.  A 0,0 return indicates an unknown font.
func FontMetrics(font string, fontSize float64) (habove, hbelow float64) {
	if fh := fontHandlers[font]; fh == nil {
		return 0, 0
	} else {
		ha, hb := fh.metrics()
		return float64(ha) * fontSize / 1000, float64(hb) * fontSize / 1000
	}
}

// ----------------------------------------------------------------------------

type standardFontHandler struct {
	name string
	fm   *standardFontMetrics
}

type standardFontMetrics struct {
	chars     [224]charMetrics // index is ASCII-32, max 223
	ligatures map[[2]byte]charMetrics
	kernpairs map[[2]byte]int16
	habove    int16
	hbelow    int16
}
type charMetrics [3]int16

func (sf standardFontHandler) replaceInvalidChars(s string) (out string, ok bool) {
	var sb strings.Builder

	ok = true
	for _, r := range s {
		if r < 32 {
			ok = false
		} else if _, ok := charmap.Windows1252.EncodeRune(r); ok {
			sb.WriteRune(r)
		} else {
			ok = false
		}
	}
	return sb.String(), ok
}

func (sf standardFontHandler) measure(s string) (width, habove, hbelow int) {
	s, _ = charmap.Windows1252.NewEncoder().String(s)
	for s != "" {
		var cm [3]int16
		if len(s) > 1 {
			var key = [2]byte{s[0], s[1]}
			if cm = sf.fm.ligatures[key]; cm[0] != 0 {
				s = s[2:]
			}
			if cm[0] == 0 {
				width += int(sf.fm.kernpairs[key])
			}
		}
		if cm[0] == 0 && s[0] >= 32 {
			cm = sf.fm.chars[s[0]-32]
			s = s[1:]
		} else if cm[0] == 0 {
			cm = sf.fm.chars['x'-32]
			s = s[1:]
		}
		width += int(cm[0])
		hbelow = min(hbelow, int(cm[1]))
		habove = max(habove, int(cm[2]))
	}
	hbelow = -hbelow
	return
}

func (sf standardFontHandler) metrics() (habove, hbelow int) {
	return int(sf.fm.habove), int(sf.fm.hbelow)
}

// addFontToPageResources ensures that the font is in the supplied page resource
// dictionary, and returns the name by which it's known there.
func (sf standardFontHandler) addFontToPageResources(pdf *PDF, path Path, fonts Dict) (name Name, err error) {
	var maxnum int

	for n := range fonts {
		if font, err := pdf.GetDict(path.K(n)); err != nil {
			return "", err
		} else if font["Type"] == Name("Font") &&
			font["Subtype"] == Name("Type1") &&
			font["BaseFont"] == Name(sf.name) &&
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
		"BaseFont": Name(sf.name),
		"Encoding": Name("WinAnsiEncoding"),
	}
	return name, nil
}

func (sf standardFontHandler) encodeString(s string) string {
	s, _ = charmap.Windows1252.NewEncoder().String(s)
	return EncodeString(s)
}

// ----------------------------------------------------------------------------

type trueTypeFontHandler struct {
}

func AddTrueTypeFont(name string, ttf []byte) (err error) {
	panic("not implemented")
}

func (tf *trueTypeFontHandler) replaceInvalidChars(s string) (out string, ok bool) {
	panic("not implemented") // TODO: Implement
}

func (tf *trueTypeFontHandler) measure(s string) (width int, habove int, hbelow int) {
	panic("not implemented") // TODO: Implement
}

func (tf *trueTypeFontHandler) metrics() (habove int, hbelow int) {
	panic("not implemented") // TODO: Implement
}

func (sf *trueTypeFontHandler) addFontToPageResources(pdf *PDF, fonts Dict) (name Name, err error) {
	panic("not implemented")
}

func (sf *trueTypeFontHandler) encodeString(s string) string {
	panic("not implemented")
}
