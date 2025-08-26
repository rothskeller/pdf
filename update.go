package pdf

// This file contains the code for making updates to the PDF structure.

import (
	"errors"
	"fmt"
)

// USLetterPortrait is the most common mediaBox parameter to AddPage.
var USLetterPortrait = Rectangle{0, 0, 612, 792}

// ErrNoSuchPage indicates a reference to a page number that does not exist in
// the PDF.
var ErrNoSuchPage = errors.New("no such page number")

// CreateObject creates a new object with the specified content, and returns a
// reference to it.  The new content will be written if Write is called.
func (p *PDF) CreateObject(obj Object) (ref Reference) {
	if p.updates == nil {
		p.updates = make(map[Reference]Object)
	}
	if p.xref == nil {
		p.xref = []any{nil}
	}
	ref.Number = len(p.xref)
	p.xref = append(p.xref, obj)
	p.updates[ref] = obj
	return ref
}

// UpdateObject registers new content for the object with the specified
// reference.  The new content will be written if Write is called.
func (p *PDF) UpdateObject(ref Reference, obj Object) {
	if p.updates == nil {
		p.updates = make(map[Reference]Object)
	}
	p.updates[ref] = obj
	p.xref[ref.Number] = obj
}

// AddPage adds a page to the PDF, with the specified dimensions.
func (pdf *PDF) AddPage(mediaBox Rectangle) (err error) {
	var (
		page    Dict
		pageref Reference
		c       *Cursor
		countC  *Cursor
	)
	page = Dict{
		"Type":   Name("Page"),
		"Parent": pdf.Catalog["Pages"],
		"Resources": Dict{"ProcSet": Array{
			Name("PDF"),
			Name("Text"),
			Name("ImageB"),
			Name("ImageC"),
			Name("ImageI"),
		}},
		"MediaBox": mediaBox.toArray(),
	}
	pageref = pdf.CreateObject(page)
	c = pdf.Cursor().Key("Root").Key("Pages")
	countC = c.Clone().Key("Count")
	count, _ := countC.Int()
	countC.SetObject(count + 1)
	c.Key("Kids").Append(pageref)
	return c.Error()
}

// AddPageContent adds content to the specified page.
func (pdf *PDF) AddPageContent(pagenum int, content string) (err error) {
	var (
		c        *Cursor
		contents Object
		cstream  Stream
	)
	if content == "" {
		return nil
	}
	if c, err = pdf.CursorForPage(pagenum); err != nil {
		return err
	}
	if page, err := c.Dict(); err != nil {
		return err
	} else if _, ok := page["Contents"]; !ok {
		cstream = Stream{Dict: Dict{}, Data: []byte(content)}
		cstreamRef := pdf.CreateObject(cstream)
		c.SetKey("Contents", cstreamRef)
		return c.Error()
	}
	if contents, err = c.Key("Contents").ObjectDeref(); err != nil {
		return err
	}
	switch contents := contents.(type) {
	case Stream:
		cstream = contents
	case Array:
		if cstream, err = c.Index(len(contents) - 1).Stream(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s is not a Stream or Array", c.Path())
	}
	// Add the new content to the identified stream, and mark the object
	// containing it for update.
	if err = cstream.Decompress(0); err != nil {
		return err
	}
	cstream.Data = append(cstream.Data, '\n')
	cstream.Data = append(cstream.Data, []byte(content)...)
	c.SetObject(cstream)
	return nil
}

// AddPageResource adds a resource to a page.
func (pdf *PDF) AddPageResource(pagenum int, resType, resName Name, resource Object) (err error) {
	var (
		c         *Cursor
		resources Dict
		tdict     Dict
	)
	if c, err = pdf.CursorForPage(pagenum); err != nil {
		return err
	}
	if resources, err = c.Key("Resources").Dict(); err != nil {
		return err
	}
	if _, ok := resources[resType]; !ok {
		resources[resType] = Dict{}
		c.SetObject(resources)
	}
	if tdict, err = c.Key(resType).Dict(); err != nil {
		return err
	}
	if _, ok := tdict[resName]; ok {
		return fmt.Errorf("%s/%s already exists", c.Path(), resName)
	}
	c.SetKey(resName, resource)
	return c.Error()
}

// ImportPDF imports all of the pages from the specified other PDF into the
// receiver PDF.  The imported page content is overlaid onto the existing
// content of each corresponding page; new pages are added as needed.
func (pdf *PDF) ImportPDF(otherPDF *PDF) (err error) {
	if pdf == otherPDF {
		panic("pdfstruct.ImportPDF cannot import from same PDF file")
	}
	var imp = importer{src: otherPDF, dest: pdf, objmap: make(map[Reference]Reference)}
	return imp.importPages()
}

type importer struct {
	src    *PDF
	dest   *PDF
	objmap map[Reference]Reference
}

func (imp *importer) importPages() (err error) {
	var (
		numPages  int
		destPages int
		srcC      *Cursor
		destC     *Cursor
	)
	if numPages, err = imp.src.NumPages(); err != nil {
		return err
	}
	if destPages, err = imp.dest.NumPages(); err != nil {
		return err
	}
	for pagenum := 1; pagenum <= numPages; pagenum++ {
		if srcC, err = imp.src.CursorForPage(pagenum); err != nil {
			return err
		}
		if pagenum > destPages {
			var mediaBox Rectangle

			if mediaBox, err = imp.src.getMediaBox(srcC); err != nil {
				return err
			}
			if err = imp.dest.AddPage(mediaBox); err != nil {
				return err
			}
		}
		if destC, err = imp.dest.CursorForPage(pagenum); err != nil {
			return err
		}
		if err = imp.importPage(pagenum, srcC, destC); err != nil {
			return err
		}
	}
	return nil
}

func (pdf *PDF) getMediaBox(pageC *Cursor) (box Rectangle, err error) {
	var mediaBox Array

	if mediaBox, err = pageC.Clone().Key("MediaBox").Array(); err != nil {
		return box, err
	}
	if len(mediaBox) != 4 {
		return box, fmt.Errorf("%s/MediaBox: ill-formed rectangle", pageC.Path())
	}
	if box.LLX, err = pdf.GetNumber(mediaBox[0]); err != nil {
		return box, fmt.Errorf("%s/MediaBox[0]: %w", pageC.Path(), err)
	}
	if box.LLY, err = pdf.GetNumber(mediaBox[1]); err != nil {
		return box, fmt.Errorf("%s/MediaBox[1]: %w", pageC.Path(), err)
	}
	if box.URX, err = pdf.GetNumber(mediaBox[2]); err != nil {
		return box, fmt.Errorf("%s/MediaBox[2]: %w", pageC.Path(), err)
	}
	if box.URY, err = pdf.GetNumber(mediaBox[3]); err != nil {
		return box, fmt.Errorf("%s/MediaBox[3]: %w", pageC.Path(), err)
	}
	return box, nil
}

func (imp *importer) importPage(pagenum int, srcPageC, destPageC *Cursor) (err error) {
	var (
		srcBBox   Rectangle
		destBBox  Rectangle
		resources Dict
		formName  Name
	)
	// Check arguments.
	if srcBBox, err = imp.src.getMediaBox(srcPageC); err != nil {
		return fmt.Errorf("imp.src: %s/MediaBox: %w", srcPageC.Path(), err)
	}
	if destBBox, err = imp.dest.getMediaBox(destPageC); err != nil {
		return fmt.Errorf("imp.dest: %s/MediaBox: %w", destPageC.Path(), err)
	}
	if srcBBox.LLX != destBBox.LLX || srcBBox.LLY != destBBox.LLY ||
		srcBBox.URX != destBBox.URX || srcBBox.URY != destBBox.URY {
		return fmt.Errorf("ImportPage: source and destination pages are not the same size")
	}
	// Import the resources used by the source page.
	if resources, err = imp.importResources(srcPageC); err != nil {
		return err
	}
	// Import the source page content stream(s).
	formName = Name(fmt.Sprintf("ImportedPage%d", pagenum))
	if err = imp.importContentStreams(formName, srcPageC, resources, destPageC, srcBBox); err != nil {
		return err
	}
	if err = imp.dest.AddPageContent(pagenum, fmt.Sprintf("q 0 J 1 w 0 j 0 G 0 g %s Do Q", EncodeName(formName))); err != nil {
		return err
	}
	return imp.importDefaultFields(pagenum, srcPageC, destPageC)
}

func (imp *importer) importResources(srcPageC *Cursor) (res Dict, err error) {
	var (
		pageC        *Cursor
		srcPage      Dict
		srcResources Dict
	)
	if srcPage, err = srcPageC.Dict(); err != nil {
		return nil, err
	}
	pageC = srcPageC.Clone()
	for srcPage != nil {
		if srcPage["Resources"] != nil {
			if srcResources, err = pageC.Key("Resources").Dict(); err != nil {
				return nil, fmt.Errorf("imp.src: %w", err)
			}
			break
		} else if srcPage["Parent"] != nil {
			if srcPage, err = pageC.Key("Parent").Dict(); err != nil {
				return nil, fmt.Errorf("imp.src: %w", err)
			}
		} else {
			return nil, fmt.Errorf("imp.src: %s: no Resources found", srcPageC.Path())
		}
	}
	if out, err := imp.importDeepObject(srcResources); err != nil {
		return nil, err
	} else {
		return out.(Dict), nil
	}
}

func (imp *importer) importDeepObject(obj Object) (out Object, err error) {
	switch obj := obj.(type) {
	case Reference:
		if outref := imp.objmap[obj]; outref.Number != 0 {
			return outref, nil
		}
		var reffed Object
		if reffed, err = imp.src.Get(obj); err != nil {
			return nil, err
		}
		if reffed, err = imp.importDeepObject(reffed); err != nil {
			return nil, err
		}
		outref := imp.dest.CreateObject(reffed)
		imp.objmap[obj] = outref
		return outref, nil
	case Dict:
		outdict := make(Dict)
		for k, v := range obj {
			if outdict[k], err = imp.importDeepObject(v); err != nil {
				return nil, err
			}
		}
		return outdict, nil
	case Array:
		outary := make(Array, len(obj))
		for i, v := range obj {
			if outary[i], err = imp.importDeepObject(v); err != nil {
				return nil, err
			}
		}
		return outary, nil
	case Stream:
		outstr := Stream{Data: obj.Data} // no need to clone the data
		if outdict, err := imp.importDeepObject(obj.Dict); err != nil {
			return nil, err
		} else {
			outstr.Dict = outdict.(Dict)
		}
		return outstr, nil
	default:
		return obj, nil
	}
}

func (imp *importer) importContentStreams(formName Name, srcPageC *Cursor, resources Dict, destPageC *Cursor, bbox Rectangle) (err error) {
	var (
		srcC        *Cursor
		destC       *Cursor
		contentsObj Object
		contents    Array
		outref      Reference
		destRes     Dict
		outstr      = Stream{Dict: Dict{
			"Type":      Name("XObject"),
			"Subtype":   Name("Form"),
			"BBox":      bbox.toArray(),
			"Resources": resources,
		}}
	)
	srcC = srcPageC.Clone()
	if contentsObj, err = srcC.Key("Contents").ObjectDeref(); err != nil {
		return err
	}
	if cstr, ok := contentsObj.(Stream); ok {
		contents = Array{cstr}
	} else if contents, err = srcC.Array(); err != nil {
		return err
	}
	for _, c := range contents {
		if str, err := imp.src.GetStream(c); err != nil {
			return err
		} else if err = str.Decompress(0); err != nil {
			return err
		} else {
			outstr.Data = append(outstr.Data, str.Data...)
		}
	}
	outref = imp.dest.CreateObject(outstr)
	destC = destPageC.Clone().Key("Resources")
	if destRes, err = destC.Dict(); err != nil {
		return err
	}
	if _, ok := destRes["XObject"]; !ok {
		destC.SetKey("XObject", Dict{})
	} else {
		destC.Key("XObject")
	}
	return destC.SetKey(formName, outref)
}

// importDefaultFields locates any checkbox or radio button fields, finds the
// appearance dictionary for their "off" state, and adds that to the destination
// page.
func (imp *importer) importDefaultFields(pagenum int, srcPageC, destPageC *Cursor) (err error) {
	var (
		srcC   *Cursor
		annots Array
	)
	srcC = srcPageC.Clone()
	if annots, err = srcC.Key("Annots").Array(); err != nil {
		return nil // probably means there are none
	}
	for i := range annots {
		if err = imp.importAnnotation(srcC.Clone().Index(i), destPageC, pagenum, i); err != nil {
			return err
		}
	}
	return nil
}

// importAnnotation imports a single annotation into the destination, if it's
// one we want.
func (imp *importer) importAnnotation(annotC, destPageC *Cursor, pagenum, annotIdx int) (err error) {
	var (
		annot   Dict
		appC    *Cursor
		appS    Stream
		rect    Array
		x, y    float64
		name    Name
		out     Object
		outref  Reference
		destC   *Cursor
		destRes Dict
	)
	if annot, err = annotC.Dict(); err != nil {
		return err
	}
	if annot["Type"] != Name("Annot") || annot["Subtype"] != Name("Widget") {
		return nil // not one we're interested in
	}
	appC = annotC.Clone()
	if appS, err = appC.Key("AP").Key("N").Key("Off").Stream(); err != nil {
		return nil // not one we're interested in
	}
	if ft, ff := findFieldDict(annotC); ft != "Btn" || ff&0x10000 != 0 {
		return nil // not one we're interested in
	}
	if rect, err = annotC.Key("Rect").Array(); err != nil || len(rect) != 4 {
		return nil // ill-formed, but we'll just ignore it
	}
	x, y = toFloat64(rect[0]), toFloat64(rect[1])
	// This is an annotation that we want to add to the destination page.
	name = Name(fmt.Sprintf("A%d", annotIdx))
	if out, err = imp.importDeepObject(appS); err != nil {
		return err
	}
	outref = imp.dest.CreateObject(out)
	destC = destPageC.Clone().Key("Resources")
	if destRes, err = destC.Dict(); err != nil {
		return err
	}
	if _, ok := destRes["XObject"]; !ok {
		destC.SetKey("XObject", Dict{})
	} else {
		destC.Key("XObject")
	}
	if err = destC.SetKey(name, outref); err != nil {
		return err
	}
	// Add content to the destination page to display the annotation.
	return imp.dest.AddPageContent(pagenum,
		fmt.Sprintf("q 0 J 1 w 0 j 0 G 0 g 1 0 0 1 %.2f %.2f cm %s Do Q",
			-x, -y, EncodeName(name)))
}

func findFieldDict(annotC *Cursor) (ft Name, ff int) {
	var (
		d      Dict
		seenFf bool
		err    error
		c      = annotC.Clone()
	)
	for {
		if d, err = c.Dict(); err != nil {
			return "", 0
		}
		if _, ok := d["Ff"]; ok && !seenFf {
			ff, _ = d["Ff"].(int)
			seenFf = true
		}
		if ft, ok := d["FT"].(Name); ok {
			return ft, ff
		}
		c = c.Key("Parent")
	}
}

func toFloat64(o Object) float64 {
	switch o := o.(type) {
	case float64:
		return o
	case int:
		return float64(o)
	default:
		return 0 // don't want to panic on invalid PDF
	}
}
