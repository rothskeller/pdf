package pdfform

import (
	"errors"
	"fmt"

	"github.com/rothskeller/pdf"
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
func setRadioButton(p *pdf.PDF, fieldref pdf.Reference, field pdf.Dict, value string) (err error) {
	var found bool

	// Update the V in the parent field.
	if v, ok := field["V"].(pdf.Name); ok && string(v) == value {
		return nil // no change needed
	}
	if value == "Off" {
		delete(field, "V")
		found = true
	} else {
		field["V"] = pdf.Name(value)
	}
	p.UpdateObject(fieldref, field)
	// Update the /AS of each of the Kids.  While doing so, make sure the
	// chosen value is valid.
	var kids pdf.Array
	switch k := field["Kids"].(type) {
	case nil:
		return errors.New("field[Kids] doesn't exist")
	case pdf.Reference:
		if kids, err = p.GetArray(k); err != nil {
			return fmt.Errorf("field[Kids]: %s", err)
		}
	case pdf.Array:
		kids = k
	default:
		return errors.New("field[Kids] is not an Array")
	}
	for i, k := range kids {
		// Get the kid Dict.
		var kid pdf.Dict
		var kidref pdf.Reference
		switch k := k.(type) {
		case pdf.Reference:
			if kid, err = p.GetDict(k); err != nil {
				return fmt.Errorf("field[Kids][%d]: %s", i, err)
			}
			kidref = k
		case pdf.Dict:
			kid = k
			kidref = fieldref
		default:
			return fmt.Errorf("field[Kids][%d] is not a Dict", i)
		}
		// Get the kid's AP dict.
		var ap pdf.Dict
		switch a := kid["AP"].(type) {
		case pdf.Reference:
			if ap, err = p.GetDict(a); err != nil {
				return fmt.Errorf("field[Kids][%d][AP]: %s", i, err)
			}
		case pdf.Dict:
			ap = a
		default:
			return fmt.Errorf("field[Kids][%d][AP] is not a Dict", i)
		}
		// Get the kid's AP/N dict.
		var apn pdf.Dict
		switch n := ap["N"].(type) {
		case pdf.Reference:
			if apn, err = p.GetDict(n); err != nil {
				return fmt.Errorf("field[Kids][%d][AP][N]: %s", i, err)
			}
		case pdf.Dict:
			apn = n
		default:
			return fmt.Errorf("field[Kids][%d][AP][N] is not a Dict", i)
		}
		// Does it have an entry that matches the requested value?
		if _, ok := apn[pdf.Name(value)]; ok {
			// Yes, so set the /AS for this kid to that value.
			found = true
			kid["AS"] = pdf.Name(value)
			if kidref != fieldref {
				p.UpdateObject(kidref, kid)
			}
		} else {
			// No, so set the /AS for this kid to /Off, if it isn't
			// already.
			if kid["AS"] != pdf.Name("Off") {
				kid["AS"] = pdf.Name("Off")
				if kidref != fieldref {
					p.UpdateObject(kidref, kid)
				}
			}
		}
	}
	if !found {
		return fmt.Errorf("value %q is not valid for field %q", value, field["T"])
	}
	return nil
}
