package pdfform

import (
	"errors"

	"github.com/rothskeller/pdf/pdfstruct"
)

// ClonePage clones the specified page of the PDF document, giving it new copies
// of fillable fields on that page.  The clone is inserted immediately after the
// original in the page order.  The result is not applied until p.Write is
// called.
//
// pagenum is the zero-based index of the page to clone.
//
// prefix is the prefix to add to the field names on the new page.  It must not
// contain a dot.  The field names on the new page will be
// "prefix.oldfieldname".
//
// Limitations: only works for PDF files with a flat page structure.  Does not
// preserve annotations other than fillable fields on the cloned page.
func ClonePage(p *pdfstruct.PDF, pagenum int, prefix string) (err error) {
	var (
		newPage    pdfstruct.Dict
		newPageRef pdfstruct.Reference
		oldPageRef pdfstruct.Reference
		fields     pdfstruct.Array
		newList    pdfstruct.Dict
		newListRef pdfstruct.Reference
	)
	if newPage, newPageRef, oldPageRef, err = clonePage(p, pagenum); err != nil {
		return err
	}
	if fields, newList, newListRef, err = newFieldList(p, prefix); err != nil {
		return err
	}
	if newList["Kids"], err = cloneFields(p, fields, oldPageRef, newPageRef, newListRef); err != nil {
		return err
	}
	newPage["Annots"] = newList["Kids"]
	p.UpdateObject(newListRef, newList)
	return nil
}

// clonePage creates a clone of the specified page, with everything except the
// annotations.
func clonePage(p *pdfstruct.PDF, pagenum int) (newPage pdfstruct.Dict, newPageRef, oldPageRef pdfstruct.Reference, err error) {
	// Get the Pages dictionary.
	pagesRef, ok := p.Catalog["Pages"].(pdfstruct.Reference)
	if !ok {
		err = errors.New("/Pages is not a reference")
		return
	}
	var pages pdfstruct.Dict
	if pages, err = p.GetDict(pagesRef); err != nil {
		return
	}
	// Increment the page count.
	if pcount, ok := pages["Count"].(int); ok {
		pages["Count"] = pcount + 1
	} else {
		err = errors.New("Pages/Count is not an int")
		return
	}
	// Make a new page and add it to the Pages/Kids array.
	newPage = make(pdfstruct.Dict)
	newPageRef = p.CreateObject(newPage)
	var kids pdfstruct.Array
	switch k := pages["Kids"].(type) {
	case pdfstruct.Array:
		kids = append(k, nil)
		pages["Kids"] = kids
	case pdfstruct.Reference:
		if kids, err = p.GetArray(k); err != nil {
			return
		}
		kids = append(kids, nil)
		p.UpdateObject(k, kids)
	default:
		err = errors.New("Pages/Kids is not an Array")
		return
	}
	copy(kids[pagenum+2:], kids[pagenum+1:])
	kids[pagenum+1] = newPageRef
	p.UpdateObject(pagesRef, pages)
	// Get the page we're trying to copy.
	if pagenum > len(kids)-2 {
		err = errors.New("not that many pages")
		return
	}
	if oldPageRef, ok = kids[pagenum].(pdfstruct.Reference); !ok {
		err = errors.New("Pages/Kids/# is not a reference")
		return
	}
	var oldPage pdfstruct.Dict
	if oldPage, err = p.GetDict(oldPageRef); err != nil {
		return
	}
	// Clone the page, with everything except the top Annots key.
	var clones = make(map[pdfstruct.Reference]pdfstruct.Reference)
	clones[oldPageRef] = newPageRef
	clones[pagesRef] = pagesRef
	for key, ov := range oldPage {
		if key == "Annots" {
			continue
		}
		if newPage[key], err = cloneObject(p, ov, clones); err != nil {
			return
		}
	}
	return
}

// newFieldList creates a new field list as a child of the top-level field list,
// with the specified name.
func newFieldList(p *pdfstruct.PDF, name string) (
	fields pdfstruct.Array, newField pdfstruct.Dict, newFieldRef pdfstruct.Reference, err error,
) {
	// Get the AcroForm dictionary.
	formref, ok := p.Catalog["AcroForm"].(pdfstruct.Reference)
	if !ok {
		err = errors.New("AcroForm is not a reference")
		return
	}
	form, err := p.GetDict(formref)
	if err != nil {
		return
	}
	// Make the new field.
	newField = make(pdfstruct.Dict)
	newField["T"] = name
	newFieldRef = p.CreateObject(newField)
	// Add it to the AcroForm/Fields list.
	switch f := form["Fields"].(type) {
	case pdfstruct.Array:
		fields = append(f, newFieldRef)
		form["Fields"] = fields
		p.UpdateObject(formref, form)
	case pdfstruct.Reference:
		if fields, err = p.GetArray(f); err != nil {
			return
		}
		fields = append(fields, newFieldRef)
		p.UpdateObject(f, fields)
	default:
		err = errors.New("AcroForm/Fields is not an array")
		return
	}
	return fields, newField, newFieldRef, nil
}

// cloneFields makes copies of all of the fields in fieldsKids that are on the
// page indicated by oldPageRef.  The copies have their page set to newPageRef
// and parent set to newTreeRef.  It adds those copies to newKids and to
// newPageAnnots.
func cloneFields(
	p *pdfstruct.PDF, fieldsKids pdfstruct.Array, oldPageRef, newPageRef, newTreeRef pdfstruct.Reference,
) (list pdfstruct.Array, err error) {
	var clones = make(map[pdfstruct.Reference]pdfstruct.Reference)
	clones[oldPageRef] = newPageRef
	for _, oldfieldrefobj := range fieldsKids {
		// Get the field dictionary.
		oldfieldref, ok := oldfieldrefobj.(pdfstruct.Reference)
		if !ok {
			return nil, errors.New("AcroForm/Fields/# is not a reference")
		}
		oldfield, err := p.GetDict(oldfieldref)
		if err != nil {
			return nil, err
		}
		// If it's not on the page we're cloning, ignore it.
		if oldfieldpage, ok := oldfield["P"].(pdfstruct.Reference); !ok || oldfieldpage.Number != oldPageRef.Number || oldfieldpage.Generation != oldPageRef.Generation {
			continue
		}
		// Clone it and add it to the list.
		newfieldref, err := cloneField(p, oldfield, oldfieldref, newPageRef, newTreeRef, clones)
		if err != nil {
			return nil, err
		}
		list = append(list, newfieldref)
	}
	return list, nil
}

// cloneField clones a single field and returns a reference to it.
func cloneField(
	p *pdfstruct.PDF, oldField pdfstruct.Dict, oldFieldRef, newPageRef, newParentRef pdfstruct.Reference,
	clones map[pdfstruct.Reference]pdfstruct.Reference,
) (newFieldRef pdfstruct.Reference, err error) {
	var newField pdfstruct.Dict
	if newField, err = cloneDict(p, oldField, clones); err != nil {
		return
	}
	newField["Page"] = newPageRef
	newField["Parent"] = newParentRef
	newFieldRef = p.CreateObject(newField)
	clones[oldFieldRef] = newFieldRef
	return newFieldRef, nil
}

func cloneObject(
	p *pdfstruct.PDF, old pdfstruct.Object, clones map[pdfstruct.Reference]pdfstruct.Reference,
) (no pdfstruct.Object, err error) {
	switch old := old.(type) {
	case nil, bool, int, float64, string, []byte, pdfstruct.Name:
		return old, nil
	case pdfstruct.Array:
		return cloneArray(p, old, clones)
	case pdfstruct.Dict:
		return cloneDict(p, old, clones)
	case pdfstruct.Stream:
		return cloneStream(p, old, clones)
	case pdfstruct.Reference:
		return cloneReference(p, old, clones)
	default:
		panic("unexpected object type")
	}
}

func cloneArray(
	p *pdfstruct.PDF, old pdfstruct.Array, clones map[pdfstruct.Reference]pdfstruct.Reference,
) (na pdfstruct.Array, err error) {
	for _, ov := range old {
		nv, err := cloneObject(p, ov, clones)
		if err != nil {
			return nil, err
		}
		na = append(na, nv)
	}
	return na, nil
}

func cloneDict(
	p *pdfstruct.PDF, old pdfstruct.Dict, clones map[pdfstruct.Reference]pdfstruct.Reference,
) (nd pdfstruct.Dict, err error) {
	nd = make(pdfstruct.Dict)
	for key, ov := range old {
		if nd[key], err = cloneObject(p, ov, clones); err != nil {
			return nil, err
		}
	}
	return nd, nil
}

func cloneStream(
	p *pdfstruct.PDF, old pdfstruct.Stream, clones map[pdfstruct.Reference]pdfstruct.Reference,
) (ns pdfstruct.Stream, err error) {
	ns.Data = old.Data
	ns.Dict, err = cloneDict(p, old.Dict, clones)
	return
}

func cloneReference(
	p *pdfstruct.PDF, old pdfstruct.Reference, clones map[pdfstruct.Reference]pdfstruct.Reference,
) (nr pdfstruct.Reference, err error) {
	if nr, ok := clones[old]; ok {
		return nr, nil
	}
	oo, err := p.Get(old)
	if err != nil {
		return
	}
	nr = p.CreateObject(nil)
	clones[old] = nr
	no, err := cloneObject(p, oo, clones)
	if err != nil {
		return
	}
	p.UpdateObject(nr, no)
	return nr, nil
}
