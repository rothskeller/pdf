package pdf

// This file contains the code for making updates to the PDF structure.

import (
	"errors"
	"fmt"
	"slices"
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

// AddPageContent adds content to the specified page.
func (pdf *PDF) AddPageContent(pagenum int, content string) (err error) {
	var (
		path    Path
		cstream Stream
	)
	if content == "" {
		return nil
	}
	if path, err = pdf.PagePath(pagenum); err != nil {
		return err
	}
	path = path.K("Contents")
	switch contents := pdf.Get(path).(type) {
	case error:
		return contents
	case nil:
		cstream = Stream{Dict: Dict{}, Data: []byte(content)}
		return pdf.Set(path, pdf.CreateObject(cstream))
	case Stream:
		cstream = contents
		cstream.Decompress(0)
		cstream.Data = append(cstream.Data, '\n')
		cstream.Data = append(cstream.Data, []byte(content)...)
		return pdf.Set(path, cstream)
	case Array:
		path = path.I(len(contents) - 1)
		if cstream, err = pdf.GetStream(path); err != nil {
			return err
		}
		cstream.Decompress(0)
		cstream.Data = append(cstream.Data, '\n')
		cstream.Data = append(cstream.Data, []byte(content)...)
		return pdf.Set(path, cstream)
	default:
		return fmt.Errorf("%s is %T, not Stream or Array", path, contents)
	}
}

// AddPageResource adds a resource to a page.
func (pdf *PDF) AddPageResource(pagenum int, resType, resName Name, resource Object) (err error) {
	var (
		path  Path
		tdict Dict
	)
	if path, err = pdf.PagePath(pagenum); err != nil {
		return err
	}
	path = path.K("Resources").K(resType)
	switch tobj := pdf.Get(path).(type) {
	case error:
		return err
	case nil:
		tdict = Dict{}
	case Dict:
		tdict = tobj
	default:
		return fmt.Errorf("%s is %T, not Dict or nil", path, tobj)
	}
	if _, ok := tdict[resName]; ok {
		return fmt.Errorf("%s/%s already exists", path, resName)
	}
	tdict[resName] = resource
	return pdf.Set(path, tdict)
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
		srcPath   Path
		destPath  Path
	)
	if numPages, err = imp.src.NumPages(); err != nil {
		return err
	}
	if destPages, err = imp.dest.NumPages(); err != nil {
		return err
	}
	for pagenum := 1; pagenum <= numPages; pagenum++ {
		if srcPath, err = imp.src.PagePath(pagenum); err != nil {
			return err
		}
		if pagenum > destPages {
			var mediaBox Rectangle

			if mediaBox, err = imp.src.GetRectangle(srcPath.K("MediaBox")); err != nil {
				return err
			}
			if err = imp.dest.AddPage(mediaBox); err != nil {
				return err
			}
		}
		if destPath, err = imp.dest.PagePath(pagenum); err != nil {
			return err
		}
		if err = imp.importPage(pagenum, srcPath, destPath); err != nil {
			return err
		}
	}
	return nil
}

func (imp *importer) importPage(pagenum int, srcPath, destPath Path) (err error) {
	var (
		srcBBox   Rectangle
		destBBox  Rectangle
		resources Dict
		formName  Name
	)
	// Check arguments.
	if srcBBox, err = imp.src.GetRectangle(srcPath.K("MediaBox")); err != nil {
		return fmt.Errorf("imp.src: %w", err)
	}
	if destBBox, err = imp.dest.GetRectangle(destPath.K("MediaBox")); err != nil {
		return fmt.Errorf("imp.dest: %w", err)
	}
	if srcBBox.LLX != destBBox.LLX || srcBBox.LLY != destBBox.LLY ||
		srcBBox.URX != destBBox.URX || srcBBox.URY != destBBox.URY {
		return fmt.Errorf("ImportPage: source and destination pages are not the same size")
	}
	// Import the resources used by the source page.
	if resources, err = imp.importResources(srcPath); err != nil {
		return err
	}
	// Import the source page content stream(s).
	formName = Name(fmt.Sprintf("ImportedPage%d", pagenum))
	if err = imp.importContentStreams(formName, srcPath, resources, destPath, srcBBox); err != nil {
		return err
	}
	if err = imp.dest.AddPageContent(pagenum, fmt.Sprintf("q 0 J 1 w 0 j 0 G 0 g %s Do Q", EncodeName(formName))); err != nil {
		return err
	}
	return imp.importDefaultFields(pagenum, srcPath, destPath)
}

func (imp *importer) importResources(srcPath Path) (res Dict, err error) {
	var (
		pagePath     Path
		srcPage      Dict
		srcResources Dict
	)
	if srcPage, err = imp.src.GetDict(srcPath); err != nil {
		return nil, err
	}
	pagePath = srcPath
	for srcPage != nil {
		if srcPage["Resources"] != nil {
			if srcResources, err = imp.src.GetDict(pagePath.K("Resources")); err != nil {
				return nil, fmt.Errorf("imp.src: %w", err)
			}
			break
		} else if srcPage["Parent"] != nil {
			pagePath = pagePath.K("Parent")
			if srcPage, err = imp.src.GetDict(pagePath); err != nil {
				return nil, fmt.Errorf("imp.src: %w", err)
			}
		} else {
			return nil, fmt.Errorf("imp.src: %s: no Resources found", srcPath)
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
		if reffed, err = imp.src.Fetch(obj); err != nil {
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

func (imp *importer) importContentStreams(formName Name, srcPath Path, resources Dict, destPath Path, bbox Rectangle) (err error) {
	var (
		cstrPaths []Path
		outref    Reference
		outstr    = Stream{Dict: Dict{
			"Type":      Name("XObject"),
			"Subtype":   Name("Form"),
			"BBox":      bbox.toArray(),
			"Resources": resources,
		}}
	)
	srcPath = srcPath.K("Contents")
	switch obj := imp.src.Get(srcPath).(type) {
	case error:
		return err
	case Stream:
		cstrPaths = []Path{srcPath}
	case Array:
		cstrPaths = slices.Collect(ArrayPaths(srcPath, obj))
	default:
		return fmt.Errorf("%s is %T, not Stream or Array", srcPath, obj)
	}
	for _, cstrPath := range cstrPaths {
		if str, err := imp.src.GetStream(cstrPath); err != nil {
			return err
		} else if err = str.Decompress(0); err != nil {
			return err
		} else {
			outstr.Data = append(outstr.Data, str.Data...)
		}
	}
	outref = imp.dest.CreateObject(outstr)
	destPath = destPath.K("Resources").K("XObject")
	switch obj := imp.dest.Get(destPath).(type) {
	case error:
		return err
	case nil:
		return imp.dest.Set(destPath, Dict{formName: outref})
	case Dict:
		obj[formName] = outref
		return imp.dest.Set(destPath, obj)
	default:
		return fmt.Errorf("%s is %T, not Dict or nil", destPath, obj)
	}
}

// importDefaultFields locates any checkbox or radio button fields, finds the
// appearance dictionary for their "off" state, and adds that to the destination
// page.
func (imp *importer) importDefaultFields(pagenum int, srcPath, destPath Path) (err error) {
	srcPath = srcPath.K("Annots")
	switch arr := imp.src.Get(srcPath).(type) {
	case error:
		return arr
	case nil:
		return nil // no annotations to read
	case Array:
		for i := range arr {
			if err = imp.importAnnotation(srcPath.I(i), destPath, pagenum, i); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%s is %T, not Array or nil", srcPath, arr)
	}
}

// importAnnotation imports a single annotation into the destination, if it's
// one we want.
func (imp *importer) importAnnotation(annotPath, destPath Path, pagenum, annotIdx int) (err error) {
	var (
		annot   Dict
		appS    Stream
		appBBox Rectangle
		rect    Rectangle
		name    Name
		out     Object
		outref  Reference
		xm      Matrix
	)
	if annot, err = imp.src.GetDict(annotPath); err != nil {
		return err
	}
	if annot["Type"] != Name("Annot") || annot["Subtype"] != Name("Widget") {
		return nil // not one we're interested in
	}
	if appS, err = imp.src.GetStream(annotPath.K("AP").K("N").K("Off")); err != nil {
		return nil // not one we're interested in
	}
	if appBBox, err = imp.src.GetRectangle(annotPath.K("AP").K("N").K("Off").K("BBox")); err != nil {
		return nil // ill-formed, but we'll just ignore it
	}
	if m, err := imp.src.GetArray(annotPath.K("AP").K("N").K("Off").K("Matrix")); err == nil {
		if am, err := m.ToMatrix(); err == nil {
			if am.A != 1 || am.B != 0 || am.C != 0 || am.D != 1 || am.E != 0 || am.F != 0 {
				return fmt.Errorf("%s/AP/N/Off/Matrix: non-identity matrix not supported", annotPath)
			}
		}
	}
	if ft, ff := imp.src.findFieldDict(annotPath); ft != "Btn" || ff&0x10000 != 0 {
		return nil // not one we're interested in
	}
	if rect, err = imp.src.GetRectangle(annotPath.K("Rect")); err != nil {
		return nil // ill-formed, but we'll just ignore it
	}
	// This is an annotation that we want to add to the destination page.
	name = Name(fmt.Sprintf("A%d", annotIdx))
	if out, err = imp.importDeepObject(appS); err != nil {
		return err
	}
	outref = imp.dest.CreateObject(out)
	destPath = destPath.K("Resources").K("XObject")
	switch obj := imp.dest.Get(destPath).(type) {
	case error:
		return err
	case nil:
		if err = imp.dest.Set(destPath, Dict{name: outref}); err != nil {
			return err
		}
	case Dict:
		obj[name] = outref
		if err = imp.dest.Set(destPath, obj); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s is %T, not Dict or nil", destPath, obj)
	}
	// The lower left corner of the annotation should go at the lower left
	// corner of the widget rectangle.
	xm.E = rect.LLX - appBBox.LLX
	xm.F = rect.LLY - appBBox.LLY
	// The width and height of the annotation should be scaled to the size
	// of the widget rectangle.
	xm.A = (rect.URX - rect.LLX) / (appBBox.URX - appBBox.LLX)
	xm.D = (rect.URY - rect.LLY) / (appBBox.URY - appBBox.LLY)
	// Add content to the destination page to display the annotation.
	return imp.dest.AddPageContent(pagenum,
		fmt.Sprintf("q 0 J 1 w 0 j 0 G 0 g %.2f 0 0 %.2f %.2f %.2f cm %s Do Q",
			xm.A, xm.D, xm.E, xm.F, EncodeName(name)))
}

func (pdf *PDF) findFieldDict(annotPath Path) (ft Name, ff int) {
	var (
		d      Dict
		seenFf bool
		err    error
	)
	for {
		if d, err = pdf.GetDict(annotPath); err != nil {
			return "", 0
		}
		if _, ok := d["Ff"]; ok && !seenFf {
			ff, _ = d["Ff"].(int)
			seenFf = true
		}
		if ft, ok := d["FT"].(Name); ok {
			return ft, ff
		}
		annotPath = annotPath.K("Parent")
	}
}
