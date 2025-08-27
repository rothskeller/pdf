package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/rothskeller/pdf"
)

var boxno int

func main() {
	var (
		srcFH    *os.File
		src      *pdf.PDF
		outFH    *os.File
		out      *pdf.PDF
		numPages int
		err      error
	)
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: pdf-form-boxes pdf-file")
		os.Exit(2)
	}
	if srcFH, err = os.Open(os.Args[1]); err != nil {
		goto ERROR
	}
	if src, err = pdf.Open(srcFH); err != nil {
		goto ERROR
	}
	if outFH, err = os.Create(strings.TrimSuffix(os.Args[1], ".pdf") + ".boxes.pdf"); err != nil {
		goto ERROR
	}
	out = pdf.New(outFH)
	if err = out.ImportPDF(src); err != nil {
		goto ERROR
	}
	if numPages, err = src.NumPages(); err != nil {
		goto ERROR
	}
	for pagenum := 1; pagenum <= numPages; pagenum++ {
		var (
			path   pdf.Path
			annots pdf.Array
		)
		if path, err = src.PagePath(pagenum); err != nil {
			goto ERROR
		}
		path = path.K("Annots")
		switch obj := src.Get(path).(type) {
		case error:
			err = obj
			goto ERROR
		case nil:
			continue // no page annotations
		case pdf.Array:
			annots = obj
		default:
			err = fmt.Errorf("%s is %T, not Array or nil", path, obj)
			goto ERROR
		}
		for i := range annots {
			var (
				apath pdf.Path
				annot pdf.Dict
				rect  pdf.Rectangle
			)
			apath = path.I(i)
			if annot, err = src.GetDict(apath); err != nil {
				goto ERROR
			}
			if annot["Type"] != pdf.Name("Annot") || annot["Subtype"] != pdf.Name("Widget") {
				continue
			}
			if rect, err = src.GetRectangle(apath.K("Rect")); err != nil {
				continue // ill-formed, but we'll just ignore it
			}
			// TODO walk up the field dictionary chain to find the
			// field type (handle radio buttons differently) and the
			// field name (include in output)
			if err = markRect(pagenum, rect, out); err != nil {
				goto ERROR
			}
		}
	}
	if err = out.Write(); err != nil {
		goto ERROR
	}
	os.Exit(0)
ERROR:
	fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
	os.Exit(1)
}

func markRect(pagenum int, rect pdf.Rectangle, out *pdf.PDF) (err error) {
	boxno++
	fmt.Printf("Box %2d: P %d L %6.2f R %6.2f B %6.2f T %6.2f  //  P %d X %6.2f Y %6.2f R %4.2f\n",
		boxno, pagenum, rect.LLX, rect.URX, rect.LLY, rect.URY, pagenum, (rect.LLX+rect.URX)/2, (rect.LLY+rect.URY)/2, (rect.URX-rect.LLX)/2)
	if err = (pdf.Box{
		Page:      pagenum,
		Rectangle: pdf.RectangleRT(rect.LLX, rect.LLY, rect.URX, rect.URY),
		Fill:      []byte{255, 0, 0, 128},
	}).Draw(out); err != nil {
		return err
	}
	if err = (pdf.Text{
		Page:      pagenum,
		String:    strconv.Itoa(boxno),
		Rectangle: pdf.RectangleRT(rect.LLX+1, rect.LLY, rect.URX, rect.URY-1),
		FontSize:  8,
		VAlign:    "top",
	}).Draw(out); err != nil && err != pdf.ErrDoesntFit {
		return err
	}
	return nil
}
