package pdfform

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/rothskeller/pdf/pdfstruct"
)

/*
Text fields are encoded in the PDF generally as follows:
    /Root/AcroForm/Fields/10 = (#198,0) -> Dict<<
        /T = "Origin Msg #"			[field name]
        /DA = "/TiRo 12 Tf 0 g"			[default appearance: font, font size, color]
        /P = (#18,0)				[reference to containing page]
        /Rect = Array[...]			[rectangle on page]
        /Subtype = /Widget
        /Type = /Annot
        /DV = (#336,0)				[reference to default value (usually empty)]
        /F = 4					[flags: field should print]
        /FT = /Tx				[field type is text]
        /MK = Dict<<>>				[not sure what this is for]
        /AP = Dict<<				[appearance dictionary, sometimes absent if field is empty]
            /N = (#368,0) -> Stream<<		["N" is normal appearance, generally the only one defined; must be separate object]
                /Type = /XObject
                /Subtype = /Form
                /BBox = Array[...]		[bounding box for the field, relative to bottom left of field /Rect]
                /Resources = Dict<<		[resources used by content stream]
                    /ProcSet = Array[
                        [0] = /PDF		[it uses PDF operators]
                        [1] = /Text		[it uses text operators]
                    ]
                    /Font = Dict<<
                        /TiRo = (#332,0)	[it uses the font named in /DA above]
                    >>
                >>
                /Length = 124			[length of content stream]
            >>
	    "/Tx BMC				[begin marked content for field text]
	     q 					[save graphics state]
	     1 1 65.256000 12.867000 re 	[define rectangular path, inset from bounding box]
	     W 					[set path as clipping path]
	     n 					[don't need path anymore]
	     BT 				[begin text object]
	     /TiRo 12.000000 Tf 		[set font and size]
	     0 0 0.6 rg 			[set color]
	     14.400000 TL 			[set leading for multi-line fields]
	     2 2.633500 Td 			[set initial baseline position]
	     (RSC-103P) Tj 			[write first line of text]
	     T* (second) Tj 			[write subsequent lines of text]
	     ET 				[end text object]
	     Q 					[restore saved graphics state]
	     EMC\n" 				[end of marked content]
        >>
    >>

In at least one case, where the same field value is displayed in multiple
places, the field dictionary contains only /T, /TU, /DA, /FT, and /Ff, plus a
Kids array mapping to multiple annotation dictionaries containing the rest of
the fields.
*/

// setText sets the value of a text field in a form.  fontsize is the font size
// to use; it is required for fields that do not have a font size supplied in
// the PDF, and ignored otherwise.
func setText(
	pdf *pdfstruct.PDF, form, field pdfstruct.Dict, fieldref pdfstruct.Reference, value string, fontSize float64,
) (err error) {
	// If the field value isn't changing, we don't need to do anything.
	if curr, ok := field["V"].(string); ok && curr == value {
		return nil
	}
	// Update the field value and save it.
	field["V"] = value
	pdf.UpdateObject(fieldref, field)
	// Look up the font name and size from the default field appearance.
	var fontName string
	if fontName, fontSize, err = textFontNameSize(pdf, field, fontSize); err != nil {
		return err
	}
	// Find the font dictionary.
	var fontRef pdfstruct.Reference
	if fontRef, err = textResourcesFont(pdf, form, fontName); err != nil {
		return err
	}
	// Get the list of the annotation widgets for the field.  (Usually there
	// is only one, but sometimes there are more.)
	var kids pdfstruct.Array
	switch a := field["Kids"].(type) {
	case nil:
		kids = append(kids, fieldref)
	case pdfstruct.Reference:
		if kids, err = pdf.GetArray(a); err != nil {
			return fmt.Errorf("field[Kids]: %s", err)
		}
	case pdfstruct.Array:
		kids = a
	default:
		return errors.New("field[Kids] is not an Array")
	}
	// Loop over the list and update each of them.
	for i, k := range kids {
		// Get the widget dictionary and its reference.
		var kid pdfstruct.Dict
		var kidref pdfstruct.Reference
		if k == fieldref {
			kid, kidref = field, fieldref
		} else {
			switch k := k.(type) {
			case pdfstruct.Reference:
				if kid, err = pdf.GetDict(k); err != nil {
					return fmt.Errorf("field[Kids][%d]: %s", i, err)
				}
				kidref = k
			default:
				return fmt.Errorf("field[Kids][%d] is not a Reference", i)
			}
		}
		// Compute the bounding box for the widget.
		var bbox []float64
		var bboxa pdfstruct.Array
		if bbox, bboxa, err = textBBox(pdf, kidref, kid); err != nil {
			return fmt.Errorf("field[Kids][%d]: %s", i, err)
		}
		// Compute the content stream for the widget.
		var cstream = textCStream(bbox, value, fontName, fontSize)
		// Compute the appearance for the field and save it.
		if err = textAPN(pdf, kidref, kid, bboxa, value, fontName, fontRef, cstream); err != nil {
			return fmt.Errorf("field[Kids][%d]: %s", i, err)
		}
	}
	return nil
}

// textBBox computes the bounding box for the field appearance XObject.
func textBBox(
	pdf *pdfstruct.PDF, widgetref pdfstruct.Reference, widget pdfstruct.Dict,
) (bbox []float64, bboxa pdfstruct.Array, err error) {
	// We need to get the widget rectangle.
	var recta pdfstruct.Array
	switch a := widget["Rect"].(type) {
	case nil:
		return nil, nil, errors.New("widget[Rect] is not set")
	case pdfstruct.Reference:
		if recta, err = pdf.GetArray(a); err != nil {
			return nil, nil, fmt.Errorf("widget[Rect]: %s", err)
		}
	case pdfstruct.Array:
		recta = a
	default:
		return nil, nil, errors.New("widget[Rect] is not an Array")
	}
	if len(recta) != 4 {
		return nil, nil, errors.New("widget[Rect] is not an Array of length 4")
	}
	var rect = make([]float64, 4)
	for i, v := range recta {
		switch v := v.(type) {
		case int:
			rect[i] = float64(v)
		case float64:
			rect[i] = v
		default:
			return nil, nil, errors.New("widget[Rect] is not an Array of 4 numbers")
		}
	}
	// Compute the bounding box.
	bbox = make([]float64, 4)
	bbox[0], bbox[1], bbox[2], bbox[3] = 0, 0, rect[2]-rect[0], rect[3]-rect[1]
	// Convert it into an Array.
	bboxa = make(pdfstruct.Array, 4)
	for i, v := range bbox {
		bboxa[i] = v
	}
	return bbox, bboxa, nil
}

var textDAFontRE = regexp.MustCompile(`/(\S+)\s*([0-9]+(?:\.[0-9]*)?)\s*Tf\b`)

// textFontNameSize returns the font name and size from the default appearance of the
// field.  If the font size is not specified there, it returns the supplied font
// size.
func textFontNameSize(pdf *pdfstruct.PDF, field pdfstruct.Dict, defaultSize float64) (name string, size float64, err error) {
	var da string
	switch a := field["DA"].(type) {
	case nil:
		return "", 0, errors.New("field[DA] is not set")
	case pdfstruct.Reference: // hardly seems likely, but it's allowed
		if da, err = pdf.GetString(a); err != nil {
			return "", 0, fmt.Errorf("field[DA]: %s", err)
		}
	case string:
		da = a
	default:
		return "", 0, errors.New("field[DA] is not a string")
	}
	var match []string
	if match = textDAFontRE.FindStringSubmatch(da); match == nil {
		return "", 0, errors.New("field[DA] does not contain a font setting")
	}
	name = match[1]
	size, _ = strconv.ParseFloat(match[2], 64)
	if size == 0 {
		size = defaultSize
	}
	return name, size, nil
}

// textResourcesFont returns the font dictionary for the named font.
func textResourcesFont(pdf *pdfstruct.PDF, form pdfstruct.Dict, fontName string) (ref pdfstruct.Reference, err error) {
	var dr pdfstruct.Dict
	switch a := form["DR"].(type) {
	case nil:
		return ref, errors.New("AcroForm[DR] is not present")
	case pdfstruct.Reference:
		if dr, err = pdf.GetDict(a); err != nil {
			return ref, fmt.Errorf("AcroForm[DR]: %s", err)
		}
	case pdfstruct.Dict:
		dr = a
	default:
		return ref, errors.New("AcroForm[DR] is not a Dict")
	}
	var font pdfstruct.Dict
	switch a := dr["Font"].(type) {
	case nil:
		return ref, errors.New("AcroForm[DR][Font] is not present")
	case pdfstruct.Reference:
		if font, err = pdf.GetDict(a); err != nil {
			return ref, fmt.Errorf("AcroForm[DR][Font]: %s", err)
		}
	case pdfstruct.Dict:
		font = a
	default:
		return ref, errors.New("AcroForm[DR][Font] is not a Dict")
	}
	switch a := font[pdfstruct.Name(fontName)].(type) {
	case nil:
		return ref, fmt.Errorf("field[DA] references font %q which is not defined in AcroForm[DR][Font]", fontName)
	case pdfstruct.Reference:
		return a, nil
	default:
		return ref, fmt.Errorf("AcroForm[DR][Form][%s] is not a Reference", fontName)
	}
}

// Ideally we would compute line placement based on actual font metrics, but
// that's hard.  For now, we'll hard-code a font ratio and hope it's good
// enough for our usage.
const ascenderToTotalRatio = 0.8

func textCStream(bbox []float64, value, fontName string, fontSize float64) []byte {
	var buf bytes.Buffer
	var multiline = bbox[3] >= (2.35*fontSize + 4) // room for two lines or more
	var leading = fontSize * 1.2
	var topline float64
	if multiline {
		// The initial vertical position of the text is 2 units plus an
		// ascender down from the top of the bounding box.
		topline = bbox[3] - 2.0 - ascenderToTotalRatio*fontSize
	} else {
		// The initial vertical position has the text centered in the
		// bounding box.  (It will look a little above the center if it
		// does not start with something having a descender.)
		topline = bbox[3]/2 + fontSize/2 - ascenderToTotalRatio*fontSize
	}
	// Start the rendering instructions.  Translation: begin marked content
	// for /Tx; save graphics state; define a rectangular path inset one
	// unit from the bounding box; set it as the clipping path; drop the
	// path; begin text object; set the font; set a dark blue color; set the
	// initial text position.
	fmt.Fprintf(&buf, "/Tx BMC q 1 1 %f %f re W n BT /%s %f Tf 0 0 0.6 rg %f TL 2 %f Td ",
		bbox[2]-2.0, bbox[3]-2.0, fontName, fontSize, leading, topline)
	// Emit the text itself.
	if multiline {
		for _, line := range strings.Split(value, "\n") {
			fmt.Fprintf(&buf, "%s Tj T* ", encodeString(line))
		}
	} else {
		fmt.Fprintf(&buf, "%s Tj ", encodeString(value))
	}
	// Finish the rendering instructions.
	buf.WriteString("ET Q EMC\n")
	return buf.Bytes()
}

// encodeString encodes the string in PDF syntax.  CRs, backslashes, and
// parentheses are escaped; everything else is literal; the whole is surrounded
// in parentheses.
func encodeString(s string) string {
	var sb strings.Builder
	var by = []byte(s)
	sb.WriteByte('(')
	for _, b := range by {
		switch b {
		case '\r':
			sb.WriteByte('\\')
			sb.WriteByte('r')
		case '\\', '(', ')':
			sb.WriteByte('\\')
			sb.WriteByte(b)
		default:
			sb.WriteByte(b)
		}
	}
	sb.WriteByte(')')
	return sb.String()
}

// textAPN computes and saves the appearance of a text field.
func textAPN(
	pdf *pdfstruct.PDF, widgetref pdfstruct.Reference, widget pdfstruct.Dict, bbox pdfstruct.Array, value, fontName string,
	fontRef pdfstruct.Reference, cstream []byte,
) (err error) {
	// Generate the AP/N.
	var apn pdfstruct.Stream
	apn.Dict = make(pdfstruct.Dict)
	apn.Dict["Type"] = pdfstruct.Name("XObject")
	apn.Dict["Subtype"] = pdfstruct.Name("Form")
	apn.Dict["BBox"] = bbox
	var rd = pdfstruct.Dict{
		"Font": pdfstruct.Dict{
			pdfstruct.Name(fontName): fontRef,
		},
		"ProcSet": pdfstruct.Array{
			pdfstruct.Name("PDF"),
			pdfstruct.Name("Text"),
		},
	}
	apn.Dict[pdfstruct.Name("Resources")] = rd
	apn.Data = cstream
	// Now figure out where to put it.  Note that N must be a separate
	// object; the spec doesn't say that, but most readers won't work if it
	// isn't.
	var ap pdfstruct.Dict
	switch a := widget["AP"].(type) {
	case nil:
		ap = make(pdfstruct.Dict)
		ap["N"] = pdf.CreateObject(apn)
		widget["AP"] = ap
		pdf.UpdateObject(widgetref, widget)
	case pdfstruct.Reference:
		if ap, err = pdf.GetDict(a); err != nil {
			return fmt.Errorf("widget[AP]: %s", err)
		}
		switch b := ap["N"].(type) {
		case pdfstruct.Reference:
			pdf.UpdateObject(b, apn)
		default:
			ap["N"] = pdf.CreateObject(apn)
			pdf.UpdateObject(a, ap)
		}
	case pdfstruct.Dict:
		ap = a
		switch b := ap["N"].(type) {
		case pdfstruct.Reference:
			pdf.UpdateObject(b, apn)
		default:
			ap["N"] = pdf.CreateObject(apn)
		}
	default:
		return errors.New("widget[AP] is not a Dict")
	}
	return nil
}
