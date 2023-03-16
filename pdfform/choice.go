package pdfform

import (
	"errors"
	"fmt"

	"github.com/rothskeller/pdf/pdfstruct"
)

/*
Choices are encoded in the PDF as follows:
    /Root/AcroForm/Fields/6 = (#24,0) -> Dict<<
        /AP = (#332,0)				[appearance]
        /Border = Array[...]
        /DA = "/TimesNewRomanPSMT 9 Tf 0 g"	[default appearance]
        /DV = "Operations Section"		[default value]
        /F = 4 					[include when printing]
        /FT = /Ch 				[field type choice]
        /Ff = 4587520 				[flags: combo box, editable]
        /MK = (#331,0)
        /Opt = Array[				[list of valid options]
            [0] = "RACES Chief Radio Officer"
            [1] = "RACES Unit"
            [2] = "Operations Section"
            [3] = ""
        ]
        /Rect = Array[...]
        /Subtype = /Widget
        /T = "ToICSPosition"			[field name]
        /Type = /Annot
        /V = "RACES Chief Radio Officer"	[current value]
    >>
*/

// setChoice sets the state of select or combo box.
func setChoice(pdf *pdfstruct.PDF, fieldref pdfstruct.Reference, field pdfstruct.Dict, value string) (err error) {
	// Update the V in the field.
	if v, ok := field["V"].(string); ok && v == value {
		return nil // no change needed
	}
	field["V"] = value
	pdf.UpdateObject(fieldref, field)
	// If editing is allowed — i.e., values not in the list are acceptable —
	// we're done.
	if field["Ff"].(int)&0x60000 != 0 {
		return nil
	}
	// Make sure the value is valid.
	var opts pdfstruct.Array
	switch o := field["Opts"].(type) {
	case nil:
		return errors.New("field[Opts] is not specified")
	case pdfstruct.Reference:
		if opts, err = pdf.GetArray(o); err != nil {
			return fmt.Errorf("field[Opts]: %s", err)
		}
	case pdfstruct.Array:
		opts = o
	default:
		return errors.New("field[Opts] is not an Array")
	}
	for _, o := range opts {
		if o, ok := o.(string); ok && o == value {
			return nil
		}
	}
	return fmt.Errorf("value %q is not valid for field %q", value, field["T"])
}
