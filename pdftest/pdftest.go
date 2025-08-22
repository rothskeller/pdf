package main

import (
	"fmt"
	"os"

	"github.com/rothskeller/pdf"
)

func main() {
	var (
		p2  *pdf.PDF
		err error
	)
	os.Chdir("/Users/stever/src/pdf-v2")
	fh, _ := os.Create("test.pdf")
	p := pdf.New(fh)
	fh2, err := os.Open("notrep.pdf")
	if err != nil {
		goto ERROR
	}
	p2, err = pdf.Open(fh2)
	if err != nil {
		goto ERROR
	}
	if err = p.ImportPDF(p2); err != nil {
		goto ERROR
	}
	if err = (pdf.Box{
		Rectangle: pdf.RectangleRT(200, 200, 400, 400),
		Fill:      []byte{255, 0, 0, 128},
		Stroke:    []byte{0, 255, 0},
	}).Draw(p); err != nil {
		goto ERROR
	}
	if err = (pdf.Cross{
		Rectangle: pdf.RectangleRT(200, 200, 400, 400),
		LineWidth: 10,
		Stroke:    []byte{0, 0, 255},
	}).Draw(p); err != nil {
		goto ERROR
	}
	if err = (pdf.Circle{
		Center: pdf.PointXY(300, 500),
		Radius: 100,
		Fill:   []byte{255, 0, 0, 128},
		Stroke: []byte{0, 255, 0},
	}).Draw(p); err != nil {
		goto ERROR
	}
	if err = (pdf.Text{
		String:    "Hello, world!\nSecond line.",
		Rectangle: pdf.RectangleRT(200, 200, 400, 400),
	}.Draw(p)); err != nil {
		goto ERROR
	}
	if err = p.Write(); err != nil {
		goto ERROR
	}
	os.Exit(0)
ERROR:
	fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
	os.Exit(1)
}
