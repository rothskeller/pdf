// Package pdf provides methods for reading, creating, and updating PDF files.
package pdf

// An Object is an object as defined by the PDF specification.  While an Object
// is defined as "any", it will in fact be one of the following:
//   - nil (a null object)
//   - bool
//   - int
//   - float64
//   - string
//   - []byte (a hex string)
//   - Name
//   - Array
//   - Dict
//   - Stream
//   - Reference
type Object any

// A Name is a PDF/Postscript name, without the leading slash.
type Name string

// An Array is an array of objects.
type Array []Object

// A Dict is a map from Name to Object.
type Dict map[Name]Object

// A Stream is a Dict followed by a block of arbitrary data.  Note that when
// retrieved from the pdfstruct library, stream data has been decompressed and
// decoded.
type Stream struct {
	Dict Dict
	Data []byte
}

// A Reference is an indirect reference to an Object.
type Reference struct {
	Number     int
	Generation int
}

// A PDF is a reference to a PDF file.
type PDF struct {
	fh      Reader
	start   int
	xref    []any
	Info    Dict
	Catalog Dict
	Trailer Dict
	updates map[Reference]Object
}

// A Rectangle specifies a rectangle, in terms of its lower left and upper
// right coordinates, in points.
type Rectangle struct{ LLX, LLY, URX, URY float64 }

func (r Rectangle) toArray() Array { return Array{r.LLX, r.LLY, r.URX, r.URY} }

// RectangleWH returns a Rectangle specified with X, Y, Width, and Height.
func RectangleWH(x, y, w, h float64) Rectangle { return Rectangle{x, y, x + w, y + h} }

// RectangleRT returns a Rectangle specified with X, Y, Right, and Top.
func RectangleRT(x, y, r, t float64) Rectangle { return Rectangle{x, y, r, t} }

// A Point specifies a point.
type Point struct{ X, Y float64 }

// PointXY returns a Point.
func PointXY(x, y float64) Point { return Point{x, y} }
