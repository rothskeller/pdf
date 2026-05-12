// pdf-baselines prints out the baselines of each text string in a PDF.
package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const tempSVGName = "/tmp/pdf-baselines.svg"

func main() {
	var mstack [][]float64
	var pagenum = flag.Int("p", 1, "page number")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: pdf-baselines [-p pagenum] pdf-file")
		os.Exit(2)
	}
	// Convert the PDF to SVG.
	cmd := exec.Command("/Applications/Inkscape.app/Contents/MacOS/inkscape", "--pdf-page", strconv.Itoa(*pagenum), "--export-type", "svg", "--export-filename", tempSVGName, flag.Arg(0))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		os.Exit(1)
	}
	// Parse the SVG.
	if fh, err := os.Open(tempSVGName); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		os.Exit(1)
	} else {
		dec := xml.NewDecoder(fh)
		for {
			if tok, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
				os.Exit(1)
			} else {
				switch tok := tok.(type) {
				case xml.StartElement:
					var m = []float64{1, 0, 0, 1, 0, 0}
					for _, attr := range tok.Attr {
						switch attr.Name.Local {
						case "viewBox":
							var ulx, uly, lrx, lry float64
							fmt.Sscanf(attr.Value, "%f %f %f %f", &ulx, &uly, &lrx, &lry)
							w, h := lrx-ulx, lry-uly
							m[0] = 612 / w
							m[3] = -792 / h
							m[4] = -ulx * m[0]
							m[5] = 792 + uly*m[3]
						case "transform":
							fmt.Sscanf(attr.Value, "matrix(%f,%f,%f,%f,%f,%f)", &m[0], &m[1], &m[2], &m[3], &m[4], &m[5])
						case "y":
							fmt.Sscanf(attr.Value, "%f", &m[5])
						}
					}
					mstack = append(mstack, m)
				case xml.EndElement:
					mstack = mstack[:len(mstack)-1]
				case xml.CharData:
					str := strings.TrimSpace(string(tok))
					if str != "" {
						var x, y float64
						for i := len(mstack) - 1; i >= 0; i-- {
							m := mstack[i]
							// fmt.Printf("%f %f %f %f %f %f   %f %f\n", m[0], m[1], m[2], m[3], m[4], m[5], x, y)
							x, y = m[0]*x+m[2]*y+m[4], m[1]*x+m[3]*y+m[5]
						}
						fmt.Printf("%6.2f  %s\n", y, str)
					}
				}
			}
		}
	}
}
