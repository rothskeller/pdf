package pdfform

import (
	"errors"

	"github.com/rothskeller/pdf/pdfstruct"
)

/*
Checkboxes are encoded in the PDF as follows:
    /Root/AcroForm/Fields/3 = (#184,0) -> Dict<<
        /V = /Yes 			[current value, will be either /Yes or /Off or absent (meaning /Off)]
        /DR = Dict<<			[font resource that the "X" comes from]
            /Font = (#328,0)
        >>
        /Rect = Array[...]		[rectangle for the field]
        /Type = /Annot
        /FT = /Btn 			[note absence of /Ff, meaning /Ff=0, meaning checkbox]
        /MK = Dict<<
            /CA = "8"
        >>
        /AP = Dict<<...>>		[appearance states for /Yes and /Off]
        /DA = "0 0 0 rg /F8 0 Tf"	[default appearance for "X" in box]
        /F = 4 				[field should print]
        /AS = /Yes 			[current state, will be either /Yes or /Off]
        /P = (#18,0)			[reference to containing page]
        /DV = /Off			[default value]
        /Subtype = /Widget
        /T = "Planning"			[field name]
    >>
*/

// setCheckbox sets the state of a set of radio buttons.  This involves
// setting V on the parent field and /AS on each of the individual buttons.
func setCheckbox(pdf *pdfstruct.PDF, fieldref pdfstruct.Reference, field pdfstruct.Dict, value string) (err error) {
	switch value {
	case "Off":
		switch v := field["V"].(type) {
		case nil:
			return nil
		case pdfstruct.Name:
			if v == "Off" {
				return nil
			}
		}
		delete(field, "V")
		field["AS"] = pdfstruct.Name("Off")
		pdf.UpdateObject(fieldref, field)
	case "Yes":
		if v, ok := field["V"].(pdfstruct.Name); ok && v == "Yes" {
			return nil
		}
		field["V"] = pdfstruct.Name("Yes")
		field["AS"] = pdfstruct.Name("Yes")
		pdf.UpdateObject(fieldref, field)
	default:
		return errors.New("value is not valid for field")
	}
	return nil
}
