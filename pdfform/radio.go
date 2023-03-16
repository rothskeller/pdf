package pdfform

import (
	"errors"
	"fmt"

	"github.com/rothskeller/pdf/pdfstruct"
)

/*
Radio button sets are encoded in the PDF as follows:
    /Root/AcroForm/Fields/0 = (#9,0) -> Dict<<
        /Kids = Array[			[one kid for each button]
            [0] = (#177,0) -> Dict<<
                /F = 4			[flags: field should print]
                /P = (#18,0)		[reference to containing page]
                /Rect = Array[...]	[rectangle for field]
                /AP = Dict<<		[appearance dictionary]
                    /D = Dict<<...>>	[appearances for each state when mouse down]
                    /N = Dict<<		[appearences for each state normally]
                        /1 = (#19,0)	["1" here is the value when this button is selected]
                    >>
                >>
                /MK = Dict<<		[not sure what this is for, doesn't seem to matter]
                    /CA = "l"
                >>
                /Parent = (#9,0)	[reference to containing radio button set]
                /Subtype = /Widget
                /Type = /Annot
                /AS = /1		[current state of this button, either /Off or the name in /AP/N above]
            >>
            [1] = (#178,0)
            [2] = (#179,0)
        ]
        /T = "Immediate"		[field name]
        /FT = /Btn			[field type button]
        /Ff = 49152			[flags: radio behavior]
	/V = /1				[current value of radio button set; will be /Off or the name in one button's AP/N]
    >>

Note, however, that Mac OS Preview incorrectly encodes radio button settings.
When a radio button is turned on, it doesn't change the parent set at all, and
it adds /V, /FT, /T, and /Ff on the selected child.  It doesn't remove those
from any child that was deselected.  And it can't read its own encoding; when
you re-open the PDF, it doesn't show any radio button selected.

(Chrome, and presumably other browsers, doesn't save fillable fields at all.
Its Save feature saves the unedited PDF, and its Print-to-PDF feature prints the
field data but leaves it uneditable.)
*/

// setRadioButton sets the state of a set of radio buttons.  This involves
// setting V on the parent field and /AS on each of the individual buttons.
func setRadioButton(pdf *pdfstruct.PDF, fieldref pdfstruct.Reference, field pdfstruct.Dict, value string) (err error) {
	var found bool

	// Update the V in the parent field.
	if v, ok := field["V"].(pdfstruct.Name); ok && string(v) == value {
		return nil // no change needed
	}
	if value == "Off" {
		delete(field, "V")
		found = true
	} else {
		field["V"] = pdfstruct.Name(value)
	}
	pdf.UpdateObject(fieldref, field)
	// Update the /AS of each of the Kids.  While doing so, make sure the
	// chosen value is valid.
	var kids pdfstruct.Array
	switch k := field["Kids"].(type) {
	case nil:
		return errors.New("field[Kids] doesn't exist")
	case pdfstruct.Reference:
		if kids, err = pdf.GetArray(k); err != nil {
			return fmt.Errorf("field[Kids]: %s", err)
		}
	case pdfstruct.Array:
		kids = k
	default:
		return errors.New("field[Kids] is not an Array")
	}
	for i, k := range kids {
		// Get the kid Dict.
		var kid pdfstruct.Dict
		var kidref pdfstruct.Reference
		switch k := k.(type) {
		case pdfstruct.Reference:
			if kid, err = pdf.GetDict(k); err != nil {
				return fmt.Errorf("field[Kids][%d]: %s", i, err)
			}
			kidref = k
		case pdfstruct.Dict:
			kid = k
			kidref = fieldref
		default:
			return fmt.Errorf("field[Kids][%d] is not a Dict", i)
		}
		// Get the kid's AP dict.
		var ap pdfstruct.Dict
		switch a := kid["AP"].(type) {
		case pdfstruct.Reference:
			if ap, err = pdf.GetDict(a); err != nil {
				return fmt.Errorf("field[Kids][%d][AP]: %s", i, err)
			}
		case pdfstruct.Dict:
			ap = a
		default:
			return fmt.Errorf("field[Kids][%d][AP] is not a Dict", i)
		}
		// Get the kid's AP/N dict.
		var apn pdfstruct.Dict
		switch n := ap["N"].(type) {
		case pdfstruct.Reference:
			if apn, err = pdf.GetDict(n); err != nil {
				return fmt.Errorf("field[Kids][%d][AP][N]: %s", i, err)
			}
		case pdfstruct.Dict:
			apn = n
		default:
			return fmt.Errorf("field[Kids][%d][AP][N] is not a Dict", i)
		}
		// Does it have an entry that matches the requested value?
		if _, ok := apn[pdfstruct.Name(value)]; ok {
			// Yes, so set the /AS for this kid to that value.
			found = true
			kid["AS"] = pdfstruct.Name(value)
			if kidref != fieldref {
				pdf.UpdateObject(kidref, kid)
			}
		} else {
			// No, so set the /AS for this kid to /Off, if it isn't
			// already.
			if kid["AS"] != pdfstruct.Name("Off") {
				kid["AS"] = pdfstruct.Name("Off")
				if kidref != fieldref {
					pdf.UpdateObject(kidref, kid)
				}
			}
		}
	}
	if !found {
		return fmt.Errorf("value %q is not valid for field %q", value, field["T"])
	}
	return nil
}
