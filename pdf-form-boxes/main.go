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
		srcFH  *os.File
		src    *pdf.PDF
		outFH  *os.File
		out    *pdf.PDF
		c      *pdf.Cursor
		fields pdf.Array
		err    error
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
	c = src.Cursor().Key("Root").Key("AcroForm").Key("Fields")
	if fields, err = c.Array(); err != nil {
		goto ERROR
	}
	for i := range fields {
		cc := c.Clone().Index(i)
		if err = markField(cc, out); err != nil {
			goto ERROR
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

func markField(c *pdf.Cursor, out *pdf.PDF) (err error) {
	var (
		field pdf.Dict
	)
	if field, err = c.Dict(); err != nil {
		return err
	}
	if r, ok := field["Rect"]; ok {
		if err = markRect(r.(pdf.Array), out); err != nil {
			return err
		}
	}
	if _, ok := field["Kids"]; ok {
		if kids, err := c.Key("Kids").Array(); err != nil {
			return err
		} else {
			for i := range kids {
				cc := c.Clone().Index(i)
				if err = markField(cc, out); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func markRect(rect pdf.Array, out *pdf.PDF) (err error) {
	var x, y, r, t float64

	x = rect[0].(float64)
	y = rect[1].(float64)
	r = rect[2].(float64)
	t = rect[3].(float64)
	boxno++
	fmt.Printf("Box %2d: L %6.2f R %6.2f B %6.2f T %6.2f  //  X %6.2f Y %6.2f R %4.2f\n",
		boxno, x, r, y, t, (x+r)/2, (y+t)/2, (r-x)/2)
	if err = (pdf.Box{
		Rectangle: pdf.RectangleRT(x, y, r, t),
		Fill:      []byte{255, 0, 0, 128},
	}).Draw(out); err != nil {
		return err
	}
	if err = (pdf.Text{
		String:    strconv.Itoa(boxno),
		Rectangle: pdf.RectangleRT(x+1, y, r, t-1),
		FontSize:  8,
		VAlign:    "top",
	}).Draw(out); err != nil && err != pdf.ErrDoesntFit {
		return err
	}
	return nil
}
