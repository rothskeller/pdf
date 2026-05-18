package pdf

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/encoding/charmap"
	unicode2 "golang.org/x/text/encoding/unicode"
)

// A fontHandler is a type that can handle rendering a font.
type fontHandler interface {
	// replaceInvalidChars ensures that all characters in the supplied
	// string are valid for the font.  It returns a possibly-adjusted
	// string, and a flag indicating whether the original string was OK.
	replaceInvalidChars(s string, allowNewline bool) (out string, ok bool)
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
	encodeString(pdf *PDF, s string) string
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
// POSTSCRIPT STANDARD FONTS
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

func (sf standardFontHandler) replaceInvalidChars(s string, allowNewline bool) (out string, ok bool) {
	var sb strings.Builder

	ok = true
	for _, r := range s {
		if r == 10 && allowNewline {
			sb.WriteRune(r)
		} else if r < 32 {
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

func (sf standardFontHandler) encodeString(pdf *PDF, s string) string {
	s, _ = charmap.Windows1252.NewEncoder().String(s)
	return EncodeString(s)
}

// ----------------------------------------------------------------------------
// TRUETYPE FONTS
// ----------------------------------------------------------------------------

type trueTypeFontHandler struct {
	ttf        []byte
	tables     map[string]*ttTable
	name       string
	ascent     int // scaled to 1000
	descent    int // scaled to 1000
	capHeight  int
	flags      int
	bbox       Array
	italic     float64
	stemV      int
	unitSize   int
	rune2glyph map[rune]int
	glyphs     []*glyphMetrics
}

type ttTable struct {
	name     string
	checksum uint32
	pos      uint32
	size     uint32
	data     []byte
}

type glyphMetrics struct {
	width   int // scaled to 1000
	ascent  int // scaled to 1000
	descent int // scaled to 1000
}

func AddTrueTypeFont(ttf []byte) (err error) {
	var (
		metricsCount int
		tf           = trueTypeFontHandler{ttf: ttf}
	)
	switch tf.uint32at(0) {
	case 0x00010000, 0x74727565:
		break
	case 0x4F54544F, 0x74746366:
		return errors.New("unsupported TrueType font encoding")
	default:
		return errors.New("not a TrueType font")
	}
	tf.findTables()
	if tf.name, err = tf.readNAME(); err != nil {
		return err
	} else if fontHandlers[tf.name] != nil {
		return nil // already read
	}
	if err = tf.readHEAD(); err != nil {
		return err
	}
	if metricsCount, err = tf.readHHEA(); err != nil {
		return err
	}
	if err = tf.readOS2(); err != nil {
		return err
	}
	if err = tf.readPOST(); err != nil {
		return err
	}
	if err = tf.readCMAP(); err != nil {
		return err
	}
	if err = tf.readMAXP(); err != nil {
		return err
	}
	if err = tf.readHMTX(metricsCount); err != nil {
		return err
	}
	if err = tf.readGLYF(); err != nil {
		return err
	}
	fontHandlers[tf.name] = &tf
	return nil
}

func (tf *trueTypeFontHandler) findTables() {
	numTables := tf.uint16at(4)
	tf.tables = make(map[string]*ttTable)
	for i := range numTables {
		record := ttTable{
			name:     tf.tableNameAt(12 + i*16),
			checksum: uint32(tf.uint16at(12+i*16+4)*0x10000 + tf.uint16at(12+i*16+6)),
			pos:      uint32(tf.uint32at(12 + i*16 + 8)),
			size:     uint32(tf.uint32at(12 + i*16 + 12)),
		}
		record.data = tf.ttf[record.pos : record.pos+record.size]
		tf.tables[record.name] = &record
	}
}

func (tf *trueTypeFontHandler) readNAME() (name string, err error) {
	t := tf.tables["name"]
	if t == nil {
		return "", errors.New("no NAME table")
	}
	if format := t.uint16at(0); format != 0 {
		return "", fmt.Errorf("unknown NAME format %d", format)
	}
	nameCount := t.uint16at(2)
	base := t.uint16at(4)
	for i := range nameCount {
		if t.uint16at(6+12*i+6) != 6 {
			continue // not the font name
		}
		system := t.uint16at(6 + 12*i)
		code := t.uint16at(6 + 12*i + 2)
		local := t.uint16at(6 + 12*i + 4)
		size := t.uint16at(6 + 12*i + 8)
		position := t.uint16at(6 + 12*i + 10)
		data := t.data[base+position : base+position+size]
		if system == 3 && code == 1 && local == 0x409 && size != 0 {
			// name encoded in UTF-16BE
			if size%2 != 0 {
				return "", errors.New("name is not UTF-16BE format")
			}
			if by, err := unicode2.UTF16(unicode2.BigEndian, unicode2.IgnoreBOM).NewDecoder().Bytes(data); err != nil {
				return "", errors.New("name is not valid UTF-16BE format")
			} else if len(by) != 0 {
				return string(by), nil
			}
		} else if system == 1 && code == 0 && local == 0 && size != 0 {
			return string(data), nil
		}
	}
	return "", errors.New("no font name found in NAME table")
}

func (tf *trueTypeFontHandler) readHEAD() (err error) {
	t := tf.tables["head"]
	if t == nil {
		return errors.New("no HEAD table")
	}
	tf.unitSize = t.uint16at(18)
	scale := 1000.0 / float64(tf.unitSize)
	tf.bbox = make(Array, 4)
	tf.bbox[0] = int(math.Round(float64(t.int16at(36)) * scale))
	tf.bbox[1] = int(math.Round(float64(t.int16at(38)) * scale))
	tf.bbox[2] = int(math.Round(float64(t.int16at(40)) * scale))
	tf.bbox[3] = int(math.Round(float64(t.int16at(42)) * scale))
	if symbolDataFormat := t.uint16at(52); symbolDataFormat != 0 {
		return fmt.Errorf("unknown symbol data format %d", symbolDataFormat)
	}
	return nil
}

func (tf *trueTypeFontHandler) readHHEA() (metricsCount int, err error) {
	t := tf.tables["hhea"]
	if t == nil {
		return 0, nil
	}
	scale := 1000.0 / float64(tf.unitSize)
	tf.ascent = int(float64(t.int16at(4)) * scale)
	tf.descent = int(float64(t.int16at(6)) * scale)
	if metricDataFormat := t.uint16at(32); metricDataFormat != 0 {
		return 0, fmt.Errorf("Unknown horizontal metric data format %d", metricDataFormat)
	}
	if metricsCount = t.uint16at(34); metricsCount == 0 {
		return 0, errors.New("number of horizontal metrics is 0")
	}
	return metricsCount, nil
}

func (tf *trueTypeFontHandler) readOS2() (err error) {
	scale := 1000.0 / float64(tf.unitSize)
	t := tf.tables["OS/2"]
	if t == nil {
		if tf.ascent == 0 {
			tf.ascent = int(float64(tf.bbox[3].(int)) * scale)
		}
		if tf.descent == 0 {
			tf.descent = int(float64(tf.bbox[1].(int)) * scale)
		}
		tf.capHeight = tf.ascent
		tf.stemV = 109 // assuming a weight of 500
		return
	}
	version := t.uint16at(0)
	weightType := t.uint16at(4)
	tf.stemV = 50 + int(math.Pow(float64(weightType)/65.0, 2))
	if weightType >= 600 {
		tf.flags |= 0x40000
	}
	if fsType := t.uint16at(8); fsType == 0x0002 || (fsType&0x0300) != 0 {
		return errors.New("font has copyright restrictions")
	}
	if tf.ascent == 0 {
		tf.ascent = int(float64(t.int16at(68)) * scale)
	}
	if tf.descent == 0 {
		tf.descent = int(float64(t.int16at(70)) * scale)
	}
	if version > 1 {
		tf.capHeight = int(float64(t.int16at(88)) * scale)
	} else {
		tf.capHeight = tf.ascent
	}
	return nil
}

func (tf *trueTypeFontHandler) readPOST() (err error) {
	t := tf.tables["post"]
	if t == nil {
		return errors.New("no POST table")
	}
	tf.flags |= 4
	tf.italic = float64(t.int16at(4)) + float64(t.uint16at(6))/65536.0
	if tf.italic != 0 {
		tf.flags |= 0x40
	}
	if t.uint32at(12) != 0 {
		tf.flags |= 0x1 // fixed width
	}
	return nil
}

func (tf *trueTypeFontHandler) readMAXP() (err error) {
	t := tf.tables["maxp"]
	if t == nil {
		return errors.New("no MAXP table")
	}
	tf.glyphs = make([]*glyphMetrics, t.uint16at(4))
	return nil
}

func (tf *trueTypeFontHandler) readCMAP() (err error) {
	var cmapPos int

	t := tf.tables["cmap"]
	if t == nil {
		return errors.New("no CMAP table")
	}
	tableCount := t.uint16at(2)
	for i := range tableCount {
		system := t.uint16at(4 + 6*i)
		coded := t.uint16at(4 + 6*i + 2)
		position := t.uint32at(4 + 6*i + 4)
		if (system == 3 && coded == 1) || system == 0 { // Microsoft, Unicode
			if format := t.uint16at(position); format == 4 {
				cmapPos = position
				break
			}
		}
	}
	if cmapPos == 0 {
		return errors.New("no CMAP for Unicode")
	}
	tf.rune2glyph = make(map[rune]int)
	maxRune := 0
	cmapEnd := cmapPos + t.uint16at(cmapPos+2)
	segmentSize := t.uint16at(cmapPos+6) / 2
	pos := cmapPos + 14
	endCodes := make([]int, segmentSize)
	for i := range endCodes {
		endCodes[i] = t.uint16at(pos + 2*i)
	}
	pos += 2*segmentSize + 2
	startCodes := make([]int, segmentSize)
	for i := range startCodes {
		startCodes[i] = t.uint16at(pos + 2*i)
	}
	pos += 2 * segmentSize
	idDeltas := make([]int, segmentSize)
	for i := range idDeltas {
		idDeltas[i] = t.int16at(pos + 2*i)
	}
	pos += 2 * segmentSize
	idRangeOffsetsPos := pos
	idRangeOffsets := make([]int, segmentSize)
	for i := range idRangeOffsets {
		idRangeOffsets[i] = t.uint16at(pos + 2*i)
	}
	pos += 2 * segmentSize
	var glyph int
	for seg := range segmentSize {
		for r := startCodes[seg]; r <= endCodes[seg]; r++ {
			if idRangeOffsets[seg] == 0 {
				glyph = (idDeltas[seg] + r) & 0xFFFF
			} else {
				position := idRangeOffsetsPos + 2*seg + (r-startCodes[seg])*2 + idRangeOffsets[seg]
				if position >= cmapEnd {
					glyph = 0
				} else {
					glyph = t.uint16at(position)
					if glyph != 0 {
						glyph = (glyph + idDeltas[seg]) & 0xFFFF
					}
				}
			}
			tf.rune2glyph[rune(r)] = glyph
			if r < unicode.MaxRune {
				maxRune = max(r, maxRune)
			}
		}
	}
	return nil
}

func (tf *trueTypeFontHandler) readHMTX(metricsCount int) (err error) {
	t := tf.tables["hmtx"]
	if t == nil {
		return errors.New("no HMTX table")
	}
	var width int
	for glyph := range tf.glyphs {
		if glyph < metricsCount {
			width = t.uint16at(4 * glyph)
			if width > 0x8000 {
				width = 0
			}
		}
		tf.glyphs[glyph] = &glyphMetrics{width: int(math.Round(float64(width) * 1000 / float64(tf.unitSize)))}
	}
	return nil
}

func (tf *trueTypeFontHandler) readGLYF() (err error) {
	for gid := range tf.glyphs {
		if glyph, err := tf.getGlyphData(gid); err != nil {
			return err
		} else if glyph != nil {
			yMin := int16(binary.BigEndian.Uint16(glyph[4:]))
			yMax := int16(binary.BigEndian.Uint16(glyph[8:]))
			tf.glyphs[gid].descent = int(math.Round(float64(yMin) * 1000 / float64(tf.unitSize)))
			tf.glyphs[gid].ascent = int(math.Round(float64(yMax) * 1000 / float64(tf.unitSize)))
		}
	}
	return nil
}

func (tf *trueTypeFontHandler) metrics() (habove int, hbelow int) {
	return tf.ascent, tf.descent
}

func (tf *trueTypeFontHandler) replaceInvalidChars(s string, allowNewline bool) (out string, ok bool) {
	ok = strings.IndexFunc(s, func(r rune) bool {
		return tf.rune2glyph[r] == 0 && (r != 10 || !allowNewline)
	}) < 0
	return s, ok
}

func (tf *trueTypeFontHandler) measure(s string) (width int, habove int, hbelow int) {
	for _, r := range s {
		g := tf.glyphs[tf.rune2glyph[r]]
		width += g.width
		habove = max(habove, g.ascent)
		hbelow = min(hbelow, g.descent)
	}
	return width, habove, hbelow
}

type ttfInPDF struct {
	tf          *trueTypeFontHandler
	charsUsed   map[rune]struct{}
	cidToGID    Stream
	cidFont     Dict
	cidFontRef  Reference
	fontDesc    Dict
	fontDescRef Reference
}

func (tf *trueTypeFontHandler) addFontToPageResources(pdf *PDF, path Path, fonts Dict) (name Name, err error) {
	var maxnum int

	for n := range fonts {
		if font, err := pdf.GetDict(path.K(n)); err != nil {
			return "", err
		} else if font["Type"] == Name("Font") &&
			font["Subtype"] == Name("Type0") &&
			font["BaseFont"] == Name(tf.name) &&
			font["Encoding"] == Name("Identity-H") {
			return n, nil
		}
		if strings.HasPrefix(string(n), "F") {
			if num, err := strconv.Atoi(string(n[1:])); err == nil {
				maxnum = max(maxnum, num)
			}
		}
	}
	// Not found, need to add it.  First, the TTF structure in the PDF.
	if pdf.ttfs == nil {
		pdf.ttfs = make(map[string]*ttfInPDF)
	}
	var ttf = pdf.ttfs[tf.name]
	if ttf == nil {
		ttf = &ttfInPDF{tf: tf, charsUsed: make(map[rune]struct{})}
		pdf.ttfs[tf.name] = ttf
	}
	// Next, the ToUnicode.
	if pdf.toUnicode.Number == 0 {
		pdf.toUnicode = pdf.CreateObject(Stream{
			Dict: Dict{"Length": len(toUnicode)},
			Data: toUnicode,
		})
	}
	// Next, the font descriptor.
	if ttf.fontDesc == nil {
		ttf.fontDesc = Dict{
			"Type":         Name("FontDescriptor"),
			"Ascent":       tf.ascent,
			"CapHeight":    tf.capHeight,
			"Descent":      tf.descent,
			"Flags":        tf.flags,
			"FontBBox":     tf.bbox,
			"FontName":     Name(tf.name),
			"ItalicAngle":  tf.italic,
			"MissingWidth": tf.glyphs[0].width,
			"StemV":        tf.stemV,
		}
		ttf.fontDescRef = pdf.CreateObject(ttf.fontDesc)
	}
	// Next, the CIDFont.
	if ttf.cidFont == nil {
		ttf.cidFont = Dict{
			"Type":     Name("Font"),
			"Subtype":  Name("CIDFontType2"),
			"BaseFont": Name(tf.name),
			"CIDSystemInfo": Dict{
				"Registry":   "Adobe",
				"Ordering":   "UCS",
				"Supplement": 0,
			},
			"DW":             tf.glyphs[0].width,
			"FontDescriptor": ttf.fontDescRef,
		}
		ttf.cidFontRef = pdf.CreateObject(ttf.cidFont)
	}
	name = Name(fmt.Sprintf("F%d", maxnum+1))
	fonts[name] = Dict{
		"Type":            Name("Font"),
		"Subtype":         Name("Type0"),
		"BaseFont":        Name(tf.name),
		"Encoding":        Name("Identity-H"),
		"ToUnicode":       pdf.toUnicode,
		"DescendantFonts": Array{ttf.cidFontRef},
	}
	return name, nil
}

func (tf *trueTypeFontHandler) encodeString(pdf *PDF, s string) string {
	for _, r := range s {
		pdf.ttfs[tf.name].charsUsed[r] = struct{}{}
	}
	s, _ = unicode2.UTF16(unicode2.BigEndian, unicode2.IgnoreBOM).NewEncoder().String(s)
	return EncodeString(s)
}

func (tf *trueTypeFontHandler) resolveBeforeWrite(pdf *PDF) {
	ttf := pdf.ttfs[tf.name]
	usedChars, usedGlyphs, fullToUsed := ttf.usedGlyphs(tf.rune2glyph)
	ttf.cidFont["W"] = tf.widthsArray(usedChars)
	ttf.cidFont["CIDToGIDMap"] = pdf.CreateObject(tf.cidToGIDMap(usedChars, fullToUsed))
	ttf.fontDesc["FontFile2"] = pdf.CreateObject(tf.fontFile(usedChars, usedGlyphs, fullToUsed))
}

func (ttf *ttfInPDF) usedGlyphs(rune2glyph map[rune]int) (usedChars []rune, usedGlyphs []int, fullToUsed map[int]int) {
	usedChars = slices.Collect(maps.Keys(ttf.charsUsed))
	slices.Sort(usedChars)
	usedMap := make(map[int]struct{}, len(ttf.charsUsed))
	for char := range ttf.charsUsed {
		usedMap[rune2glyph[char]] = struct{}{}
	}
	usedMap[0] = struct{}{}
	usedMap[1] = struct{}{}
	usedGlyphs = slices.Collect(maps.Keys(usedMap))
	slices.Sort(usedGlyphs)
	fullToUsed = make(map[int]int, len(usedGlyphs))
	for u, f := range usedGlyphs {
		fullToUsed[f] = u
	}
	return usedChars, usedGlyphs, fullToUsed
}

func (tf *trueTypeFontHandler) widthsArray(used []rune) (W Array) {
	widths := make([]int, len(used))
	for i, u := range used {
		widths[i] = tf.glyphs[tf.rune2glyph[u]].width
	}
	i := 0
	for i < len(used) {
		if i <= len(used)-3 && used[i+1] == used[i]+1 && used[i+2] == used[i]+2 && widths[i+1] == widths[i] && widths[i+2] == widths[i] {
			// We have a run of consecutive equal widths of 3 or
			// more adjacent characters.
			var j int
			for j = i + 1; j < len(used); j++ {
				if used[j] != used[j-1]+1 || widths[j] != widths[i] {
					break
				}
			}
			W = append(W, int(used[i]), int(used[j-1]), widths[i])
			i = j
			continue
		}
		if i <= len(used)-2 && used[i+1] == used[i]+1 {
			// We have a run of 2 or more adjacent characters.
			var j int
			var w2 = Array{widths[i]}
			for j = i + 1; j < len(used); j++ {
				if used[j] != used[j-1]+1 {
					break
				}
				if j >= i+3 && widths[j-2] == widths[j] && widths[j-1] == j {
					j -= 2
					w2 = w2[:len(w2)-2]
					break
				}
				w2 = append(w2, widths[j])
			}
			W = append(W, int(used[i]), w2)
			i = j
			continue
		}
		// We have a single character.
		W = append(W, int(used[i]), int(used[i]), widths[i])
		i++
	}
	return W
}

func (tf *trueTypeFontHandler) cidToGIDMap(usedChars []rune, fullToUsed map[int]int) (s Stream) {
	s.Dict = make(Dict)
	s.Data = make([]byte, 2*usedChars[len(usedChars)-1]+2)
	for _, r := range usedChars {
		binary.BigEndian.PutUint16(s.Data[2*r:], uint16(fullToUsed[tf.rune2glyph[r]]))
	}
	return s
}

func (tf *trueTypeFontHandler) fontFile(usedChars []rune, usedGlyphs []int, fullToUsed map[int]int) Stream {
	var tables = []*ttTable{
		// Copy some tables unchanged from the full font file.
		new(*tf.tables["name"]),
		tf.maybeTable("cvt "),
		tf.maybeTable("fpgm"),
		tf.maybeTable("prep"),
		tf.maybeTable("gasp"),
		tf.maybeTable("OS/2"),
		// Compute the rest.
		tf.makePOST(),
		tf.makeCMAP(usedChars, fullToUsed),
		tf.makeGLYF(usedGlyphs),
		tf.makeHMTX(usedGlyphs),
		tf.makeLOCA(usedGlyphs),
		tf.makeHEAD(),
		tf.makeHHEA(len(usedGlyphs)),
		tf.makeMAXP(len(usedGlyphs)),
	}
	tables = slices.DeleteFunc(tables, func(t *ttTable) bool { return t == nil })
	fontFile := makeFontFile(tables)
	return Stream{make(Dict), fontFile}
}

func (tf *trueTypeFontHandler) makePOST() (t *ttTable) {
	ot := tf.tables["post"]
	t = &ttTable{name: "post", data: make([]byte, 32)}
	t.data[1] = 3                   // format 3
	copy(t.data[4:], ot.data[4:16]) // italic angle and underline info
	return t
}

func (tf *trueTypeFontHandler) makeCMAP(usedChars []rune, fullToUsed map[int]int) (t *ttTable) {
	// Compute the segments in the CMAP table.
	type segment struct {
		endCode   rune
		startCode rune
		idDelta   int
	}
	var segments []segment
	for _, r := range usedChars {
		idDelta := fullToUsed[tf.rune2glyph[r]] - int(r)
		if len(segments) != 0 &&
			segments[len(segments)-1].endCode == r-1 &&
			segments[len(segments)-1].idDelta == idDelta {
			segments[len(segments)-1].endCode = r
		} else {
			segments = append(segments, segment{r, r, idDelta})
		}
	}
	segments = append(segments, segment{0xFFFF, 0xFFFF, 1})
	t = &ttTable{name: "cmap"}
	t.data = binary.BigEndian.AppendUint16(t.data, 0)  // version
	t.data = binary.BigEndian.AppendUint16(t.data, 1)  // one subtable
	t.data = binary.BigEndian.AppendUint16(t.data, 3)  // Microsoft encoding
	t.data = binary.BigEndian.AppendUint16(t.data, 1)  // UCS-2 encoding
	t.data = binary.BigEndian.AppendUint32(t.data, 12) // Offset to subtable
	t.data = binary.BigEndian.AppendUint16(t.data, 4)  // Subtable version
	segcount := uint16(len(segments))
	t.data = binary.BigEndian.AppendUint16(t.data, uint16(16+8*segcount)) // Length of subtable
	t.data = binary.BigEndian.AppendUint16(t.data, 0)                     // English
	t.data = binary.BigEndian.AppendUint16(t.data, uint16(2*segcount))    // 2*segcount
	searchRange := uint16(1)
	entrySelector := uint16(0)
	for searchRange*2 <= segcount {
		searchRange *= 2
		entrySelector++
	}
	searchRange = searchRange * 2
	t.data = binary.BigEndian.AppendUint16(t.data, searchRange)
	t.data = binary.BigEndian.AppendUint16(t.data, entrySelector)
	t.data = binary.BigEndian.AppendUint16(t.data, segcount*2-searchRange)
	for _, s := range segments {
		t.data = binary.BigEndian.AppendUint16(t.data, uint16(s.endCode))
	}
	t.data = binary.BigEndian.AppendUint16(t.data, 0) // pad
	for _, s := range segments {
		t.data = binary.BigEndian.AppendUint16(t.data, uint16(s.startCode))
	}
	for _, s := range segments {
		t.data = binary.BigEndian.AppendUint16(t.data, uint16(s.idDelta))
	}
	for range segments {
		t.data = binary.BigEndian.AppendUint16(t.data, 0) // idRangeOffset
	}
	return t
}

func (tf *trueTypeFontHandler) makeGLYF(usedGlyphs []int) (t *ttTable) {
	t = &ttTable{name: "glyf"}
	for _, fgid := range usedGlyphs {
		glyph, _ := tf.getGlyphData(fgid)
		t.data = append(t.data, glyph...)
	}
	return t
}

func (tf *trueTypeFontHandler) makeHMTX(usedGlyphs []int) (t *ttTable) {
	metricsCount := tf.tables["hhea"].uint16at(34)
	old := tf.tables["hmtx"]
	t = &ttTable{name: "hmtx"}
	for _, fgid := range usedGlyphs {
		var width, lsb uint16
		if fgid < metricsCount {
			width = uint16(old.uint16at(4 * fgid))
			lsb = uint16(old.uint16at(4*fgid + 2))
		} else {
			width = uint16(old.uint16at(4*metricsCount - 4))
			lsb = uint16(old.uint16at(4*metricsCount + 2*(fgid-metricsCount)))
		}
		t.data = binary.BigEndian.AppendUint16(t.data, width)
		t.data = binary.BigEndian.AppendUint16(t.data, lsb)
	}
	return t
}

func (tf *trueTypeFontHandler) makeLOCA(usedGlyphs []int) (t *ttTable) {
	t = &ttTable{name: "loca", data: []byte{0, 0, 0, 0}}
	var offset uint32
	for _, fgid := range usedGlyphs {
		glyph, _ := tf.getGlyphData(fgid)
		offset += uint32(len(glyph))
		t.data = binary.BigEndian.AppendUint32(t.data, offset)
	}
	return t
}

func (tf *trueTypeFontHandler) makeHEAD() (t *ttTable) {
	old := tf.tables["head"]
	t = &ttTable{name: "head"}
	t.data = make([]byte, len(old.data))
	copy(t.data, old.data)
	binary.BigEndian.PutUint32(t.data[8:], 0)
	binary.BigEndian.PutUint16(t.data[50:], 1)
	return t
}

func (tf *trueTypeFontHandler) makeHHEA(usedGlyphs int) (t *ttTable) {
	old := tf.tables["hhea"]
	t = &ttTable{name: "hhea"}
	t.data = make([]byte, len(old.data))
	copy(t.data, old.data)
	binary.BigEndian.PutUint16(t.data[34:], uint16(usedGlyphs))
	return t
}

func (tf *trueTypeFontHandler) makeMAXP(usedGlyphs int) (t *ttTable) {
	old := tf.tables["maxp"]
	t = &ttTable{name: "maxp"}
	t.data = make([]byte, len(old.data))
	copy(t.data, old.data)
	binary.BigEndian.PutUint16(t.data[4:], uint16(usedGlyphs))
	return t
}

func (tf *trueTypeFontHandler) maybeTable(name string) (t *ttTable) {
	if old := tf.tables[name]; old != nil {
		return new(*old)
	}
	return nil
}

func (tf *trueTypeFontHandler) getGlyphData(gid int) (glyph []byte, err error) {
	head := tf.tables["head"]
	if head == nil {
		return nil, errors.New("no HEAD table")
	}
	locaFormat := head.uint16at(50)
	loca := tf.tables["loca"]
	if loca == nil {
		return nil, errors.New("no LOCA table")
	}
	glyf := tf.tables["glyf"]
	if glyf == nil {
		return nil, errors.New("no GLYF table")
	}
	var glyphOffset, glyphSize int
	if locaFormat == 0 {
		glyphOffset = loca.uint16at(gid*2) * 2
		glyphSize = loca.uint16at(gid*2+2)*2 - glyphOffset
	} else {
		glyphOffset = loca.uint32at(gid * 4)
		glyphSize = loca.uint32at(gid*4+4) - glyphOffset
	}
	if glyphSize == 0 {
		return nil, nil
	}
	return glyf.data[glyphOffset : glyphOffset+glyphSize], nil
}

func (tf *trueTypeFontHandler) tableNameAt(pos int) (s string) {
	return string(tf.ttf[pos : pos+4])
}

func (tf *trueTypeFontHandler) uint16at(pos int) int {
	return int(binary.BigEndian.Uint16(tf.ttf[pos:]))
}

func (tf *trueTypeFontHandler) uint32at(pos int) int {
	return int(binary.BigEndian.Uint32(tf.ttf[pos:]))
}

func (tt *ttTable) int16at(pos int) (i int) {
	return int(int16(binary.BigEndian.Uint16(tt.data[pos:])))
}

func (tt *ttTable) uint16at(pos int) int {
	return int(binary.BigEndian.Uint16(tt.data[pos:]))
}

func (tt *ttTable) uint32at(pos int) int {
	return int(binary.BigEndian.Uint32(tt.data[pos:]))
}

func makeFontFile(tables []*ttTable) (file []byte) {
	var (
		buf          bytes.Buffer
		sfnt         []byte
		fileChecksum uint32
		head         *ttTable
	)
	sfnt = binary.BigEndian.AppendUint32(sfnt, 0x10000) // font type
	fileChecksum += 0x10000
	tableCount := uint16(len(tables))
	sfnt = binary.BigEndian.AppendUint16(sfnt, tableCount)
	searchRange := uint16(1)
	entrySelector := uint16(0)
	for searchRange*2 <= tableCount {
		searchRange *= 2
		entrySelector++
	}
	sfnt = binary.BigEndian.AppendUint16(sfnt, searchRange*16)
	fileChecksum += uint32(tableCount)*0x10000 + uint32(searchRange*16)
	sfnt = binary.BigEndian.AppendUint16(sfnt, entrySelector)
	sfnt = binary.BigEndian.AppendUint16(sfnt, (tableCount-searchRange)*16)
	fileChecksum += uint32(entrySelector)*0x10000 + uint32((tableCount-searchRange)*16)
	offset := uint32(len(sfnt) + 16*len(tables))
	slices.SortFunc(tables, func(a, b *ttTable) int { return cmp.Compare(a.name, b.name) })
	for _, t := range tables {
		if t.name == "head" {
			head = t
		}
		t.pos = offset
		t.size = uint32(len(t.data))
		if len(t.data)%4 != 0 {
			t.data = append(t.data, []byte{0, 0, 0}[:4-(len(t.data)%4)]...)
		}
		offset += uint32(len(t.data))
		if t.checksum == 0 {
			for i := 0; i < len(t.data); i += 4 {
				t.checksum += binary.BigEndian.Uint32(t.data[i:])
			}
		}
		fileChecksum += t.checksum
		sfnt = append(sfnt, []byte(t.name)...)
		sfnt = binary.BigEndian.AppendUint32(sfnt, uint32(t.checksum))
		sfnt = binary.BigEndian.AppendUint32(sfnt, uint32(t.pos))
		sfnt = binary.BigEndian.AppendUint32(sfnt, uint32(t.size))
	}
	buf.Write(sfnt)
	fileChecksum = 0xB1B0AFBA - fileChecksum
	binary.BigEndian.PutUint32(head.data[8:], fileChecksum)
	for _, t := range tables {
		buf.Write(t.data)
	}
	return buf.Bytes()
}

var toUnicode = []byte(`/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CIDSystemInfo
<</Registry (Adobe)
/Ordering (UCS)
/Supplement 0
>> def
/CMapName /Adobe-Identity-UCS def
/CMapType 2 def
1 begincodespacerange
<0000> <FFFF>
endcodespacerange
1 beginbfrange
<0000> <FFFF> <0000>
endbfrange
endcmap
CMapName currentdict /CMap defineresource pop
end
end`)
