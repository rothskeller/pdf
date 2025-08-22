// Package pdfform reads and writes the fillable form fields in a PDF.
package pdfform

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rothskeller/pdf"
)

// GetFields returns a map from field name to field value for all fields in the
// PDF.
func GetFields(p *pdf.PDF) (fields map[string]string, err error) {
	var (
		form  pdf.Dict
		flist pdf.Array
	)
	fields = make(map[string]string)
	switch ref := p.Catalog["AcroForm"].(type) {
	case nil:
		return fields, nil
	case pdf.Dict:
		form = ref
	case pdf.Reference:
		if form, err = p.GetDict(ref); err != nil {
			return nil, fmt.Errorf("reading form: %s", err)
		}
	default:
		return nil, errors.New("AcroForm entry in catalog is not a Dict")
	}
	switch a := form["Fields"].(type) {
	case nil:
		return fields, nil
	case pdf.Array:
		flist = a
	default:
		return nil, errors.New("AcroForm/Fields is not an Array")
	}
	for i, f := range flist {
		if err = getField(p, fields, f, nil); err != nil {
			return nil, fmt.Errorf("AcroForm/Fields[%d]: %s", i, err)
		}
	}
	return fields, nil
}

func getField(p *pdf.PDF, fields map[string]string, obj pdf.Object, path []pdf.Dict) (err error) {
	var field pdf.Dict
	switch obj := obj.(type) {
	case pdf.Reference:
		if field, err = p.GetDict(obj); err != nil {
			return err
		}
	case pdf.Dict:
		field = obj
	default:
		return errors.New("not a Dict")
	}
	path = append(path, field)
	var kids pdf.Array
	switch k := field["Kids"].(type) {
	case nil:
		break
	case pdf.Reference:
		if kids, err = p.GetArray(k); err != nil {
			return fmt.Errorf("Kids: %s", err)
		}
	case pdf.Array:
		kids = k
	default:
		return errors.New("Kids: not an Array")
	}
	if len(kids) != 0 {
		for i, k := range kids {
			if err = getField(p, fields, k, path); err != nil {
				return fmt.Errorf("Kids[%d]: %s", i, err)
			}
		}
		return nil
	}
	var name, value string
	for i, f := range path {
		switch n := f["T"].(type) {
		case nil:
			break
		case string:
			name += "." + n
		default:
			return fmt.Errorf("path[%d]/T is not a string", i)
		}
		switch v := f["V"].(type) {
		case nil:
			break
		case string:
			value = v
		case pdf.Name:
			value = string(v)
		default:
			return fmt.Errorf("path[%d]/V is not a string or Name", i)
		}
	}
	if name != "" {
		fields[name[1:]] = value
	}
	return nil
}

// SetField sets the value of a field in the PDF.  The change does not take
// effect until the caller calls Write on the underlying PDF.
func SetField(p *pdf.PDF, name, value string, fontSize float64) (err error) {
	var form pdf.Dict
	switch f := p.Catalog["AcroForm"].(type) {
	case nil:
		return errors.New("PDF does not have any form fields")
	case pdf.Reference:
		if form, err = p.GetDict(f); err != nil {
			return fmt.Errorf("AcroForm: %s", err)
		}
	case pdf.Dict:
		form = f
	default:
		return errors.New("AcroForm is not a Dict")
	}
	var fields pdf.Array
	switch a := form["Fields"].(type) {
	case nil:
		return errors.New("PDF does not have any form fields")
	case pdf.Reference:
		if fields, err = p.GetArray(a); err != nil {
			return fmt.Errorf("AcroForm[Fields]: %s", err)
		}
	case pdf.Array:
		fields = a
	default:
		return errors.New("AcroForm[Fields] is not an Array")
	}
LOOP:
	for i, f := range fields {
		var (
			fieldref pdf.Reference
			field    pdf.Dict
			want     string
			fname    string
			ftype    pdf.Name
			ok       bool
		)
		if fieldref, ok = f.(pdf.Reference); !ok {
			return errors.New("AcroForm[Fields] element is not a Reference")
		}
		if field, err = p.GetDict(fieldref); err != nil {
			return fmt.Errorf("AcroForm[Fields][%d]: %s", i, err)
		}
		if fname, ok = field["T"].(string); !ok {
			return fmt.Errorf("AcroForm[Fields][%d][T] is not a string", i)
		}
		want = name
		idx := strings.IndexByte(want, '.')
		if idx >= 0 {
			want = want[:idx]
		}
		if fname != want {
			continue
		}
		if idx >= 0 {
			name = name[idx+1:]
			switch k := field["Kids"].(type) {
			case pdf.Array:
				fields = k
			case pdf.Reference:
				if fields, err = p.GetArray(k); err != nil {
					return err
				}
			default:
				return errors.New("expected hierarchical parent but Kids is not an Array")
			}
			goto LOOP
		}
		if ftype, ok = field["FT"].(pdf.Name); !ok {
			return fmt.Errorf("AcroForm[Fields][%d][FT] is not a Name", i)
		}
		switch ftype {
		case "Btn":
			return setButton(p, fieldref, field, value)
		case "Tx":
			return setText(p, form, field, fieldref, value, fontSize)
		case "Ch":
			return setChoice(p, fieldref, field, value)
		default:
			return fmt.Errorf("field type %q is not supported", ftype)
		}
	}
	return errors.New("no such field in form")
}

func setButton(p *pdf.PDF, fieldref pdf.Reference, field pdf.Dict, value string) (err error) {
	var flags int
	switch f := field["Ff"].(type) {
	case nil:
		flags = 0
	case int:
		flags = f
	default:
		return errors.New("field[Ff] is not an int")
	}
	if flags&(1<<16) != 0 {
		return errors.New("field is a push button and doesn't have a value")
	}
	if flags&(1<<15) != 0 {
		return setRadioButton(p, fieldref, field, value)
	}
	if field["Kids"] != nil {
		// I've seen it happen where the kids have the Ff value that
		// marks it as a radio button.
		return setRadioButton(p, fieldref, field, value)
	}
	return setCheckbox(p, fieldref, field, value)
}
