// Package pdf provides methods for reading, creating, and updating PDF files.
package pdf

import (
	"errors"
	"io"
)

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
	rh        Reader
	wh        io.WriteSeeker
	start     int
	xref      []any
	Info      Dict
	Catalog   Dict
	Trailer   Dict
	updates   map[Reference]Object
	pages     []Path
	ttfs      map[string]*ttfInPDF
	toUnicode Reference
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

// A Matrix specifies a coordinate transformation matrix.  Following PDF
// standard, the matrix
//
//	┌a b 0┐
//	│c d 0│
//	└e f 1┘
//
// is represented as the array [a b c d e f].
type Matrix struct{ A, B, C, D, E, F float64 }

func (m Matrix) ToArray() Array { return Array{m.A, m.B, m.C, m.D, m.E, m.F} }

func (a Array) ToMatrix() (m Matrix, err error) {
	if len(a) != 6 {
		return m, errors.New("ill-formed matrix")
	}
	for i, v := range a {
		switch v := v.(type) {
		case float64: // ok
		case int:
			a[i] = float64(v)
		default:
			return m, errors.New("ill-formed matrix")
		}
	}
	return Matrix{a[0].(float64), a[1].(float64), a[2].(float64), a[3].(float64), a[4].(float64), a[5].(float64)}, nil
}

func (m Matrix) Multiply(o Matrix) (p Matrix) {
	p.A = m.A*o.A + m.B*o.C
	p.B = m.A*o.B + m.B*o.D
	p.C = m.C*o.A + m.D*o.C
	p.D = m.C*o.B + m.D*o.D
	p.E = m.E*o.A + m.F*o.C + o.E
	p.F = m.E*o.B + m.F*o.D + o.F
	return p
}
func (m Matrix) PreMultiply(o Matrix) Matrix { return o.Multiply(m) }

func (m Matrix) Transform(p Point) (t Point) {
	t.X = m.A*p.X + m.C*p.Y + m.E
	t.Y = m.B*p.X + m.D*p.Y + m.F
	return t
}
