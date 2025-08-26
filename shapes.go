package pdf

// This file contains code that draws common geometric shapes on a page.

import (
	"errors"
	"fmt"
	"strings"
)

// Distance from a point on the edge of a circle to the control points for the
// Bezier curve making up that part of the circle, assuming 4 arcs to make the
// complete circle.  Taken from
// https://stackoverflow.com/questions/1734745/how-to-create-circle-with-b%C3%A9zier-curves
const circleControlPointDistance = 0.552284749831

// Line is a structure containing all of the parameters for drawing a line.  To
// draw a line, create a Line structure and call its Draw method.
type Line struct {
	// P1 and P2 give the endpoints of the line.  Required.
	P1, P2 Point
	// Page gives the page number to draw on.  Default is 1.
	Page int
	// Stroke gives the stroke color for the line, as an array of three or
	// four bytes (R, G, B, and maybe A).  Default is solid black.
	Stroke []byte
	// Width gives the line width.  Default is 1pt.
	Width float64
}

func (b Line) Draw(pdf *PDF) (err error) {
	var (
		c      *Cursor
		gstate Name
		sb     strings.Builder
	)
	// Check parameters.
	if b.Page == 0 {
		b.Page = 1
	} else if b.Page < 0 {
		return errors.New("invalid Page")
	}
	if len(b.Stroke) != 0 && len(b.Stroke) != 3 && len(b.Stroke) != 4 {
		return errors.New("invalid Stroke")
	}
	if len(b.Stroke) == 4 && b.Stroke[3] == 0 {
		return nil // alpha=0 is the same as no stroke
	}
	if len(b.Stroke) == 0 {
		b.Stroke = []byte{0, 0, 0, 255}
	}
	if len(b.Stroke) == 3 {
		b.Stroke = append(b.Stroke, 255)
	}
	if b.Width == 0 {
		b.Width = 1.0
	}
	// Get the page cursor.
	if c, err = pdf.CursorForPage(b.Page); err != nil {
		return err
	}
	// Create the alpha graphic state if needed.
	if gstate, err = maybeAddAlpha(c, nil, b.Stroke); err != nil {
		return err
	}
	// Draw the box.
	sb.WriteString("q")
	if gstate != "" {
		fmt.Fprintf(&sb, " %s gs", EncodeName(gstate))
	}
	fmt.Fprintf(&sb, " %.2f %.2f %.2f RG %2.f w",
		float64(b.Stroke[0])/255, float64(b.Stroke[1])/255, float64(b.Stroke[2])/255, b.Width)
	fmt.Fprintf(&sb, " %.2f %.2f m %.2f %.2f l h s",
		b.P1.X, b.P1.Y, b.P2.X, b.P2.Y)
	sb.WriteString(" Q")
	return pdf.AddPageContent(b.Page, sb.String())
}

// Box is a structure containing all of the parameters for drawing a box.  To
// draw a box, create a Box structure and call its Draw method.
type Box struct {
	// Rectangle gives the size and location of the box.  Required.
	Rectangle Rectangle
	// Page gives the page number to draw on.  Default is 1.
	Page int
	// Fill gives the fill color for the box, as an array of three or four
	// bytes (R, G, B, and maybe A).  Default is no fill.
	Fill []byte
	// Stroke gives the stroke color for the box, as an array of three or
	// four bytes (R, G, B, and maybe A).  Default is no stroke.
	Stroke []byte
}

func (b Box) Draw(pdf *PDF) (err error) {
	var (
		c      *Cursor
		gstate Name
		sb     strings.Builder
	)
	// Check parameters.
	if b.Rectangle.LLX >= b.Rectangle.URX || b.Rectangle.LLY >= b.Rectangle.URY {
		return errors.New("invalid Rectangle")
	}
	if b.Page == 0 {
		b.Page = 1
	} else if b.Page < 0 {
		return errors.New("invalid Page")
	}
	if len(b.Fill) != 0 && len(b.Fill) != 3 && len(b.Fill) != 4 {
		return errors.New("invalid Fill")
	}
	if len(b.Fill) == 4 && b.Fill[3] == 0 {
		b.Fill = nil // alpha=0 is the same as no fill
	}
	if len(b.Fill) == 3 {
		b.Fill = append(b.Fill, 255)
	}
	if len(b.Stroke) != 0 && len(b.Stroke) != 3 && len(b.Stroke) != 4 {
		return errors.New("invalid Stroke")
	}
	if len(b.Stroke) == 4 && b.Stroke[3] == 0 {
		b.Stroke = nil // alpha=0 is the same as no stroke
	}
	if len(b.Stroke) == 3 {
		b.Stroke = append(b.Stroke, 255)
	}
	if len(b.Fill) == 0 && len(b.Stroke) == 0 {
		return nil // nothing to do
	}
	// Get the page cursor.
	if c, err = pdf.CursorForPage(b.Page); err != nil {
		return err
	}
	// Create the alpha graphic state if needed.
	if gstate, err = maybeAddAlpha(c, b.Fill, b.Stroke); err != nil {
		return err
	}
	// Draw the box.
	sb.WriteString("q")
	if gstate != "" {
		fmt.Fprintf(&sb, " %s gs", EncodeName(gstate))
	}
	if len(b.Fill) != 0 {
		fmt.Fprintf(&sb, " %d %d %d rg", b.Fill[0], b.Fill[1], b.Fill[2])
	}
	if len(b.Stroke) != 0 {
		fmt.Fprintf(&sb, " %d %d %d RG", b.Stroke[0], b.Stroke[1], b.Stroke[2])
	}
	fmt.Fprintf(&sb, " %.2f %.2f %.2f %.2f re",
		b.Rectangle.LLX, b.Rectangle.LLY,
		b.Rectangle.URX-b.Rectangle.LLX, b.Rectangle.URY-b.Rectangle.LLY)
	if len(b.Fill) != 0 && len(b.Stroke) != 0 {
		sb.WriteString(" b")
	} else if len(b.Fill) != 0 {
		sb.WriteString(" f")
	} else {
		sb.WriteString(" s")
	}
	sb.WriteString(" Q")
	return pdf.AddPageContent(b.Page, sb.String())
}

// Cross is a structure containing all of the parameters for drawing a cross
// (i.e., two perpendicular lines crossing a rectangular area).  To draw a
// cross, create a Cross structure and call its Draw method.
type Cross struct {
	// Rectangle gives the size and location of the cross.  Required.
	Rectangle Rectangle
	// Page gives the page number to draw on.  Default is 1.
	Page int
	// LineWidth gives the line width.  Default is 1 point.
	LineWidth float64
	// Stroke gives the stroke color for the lines, as an array of three or
	// four bytes (R, G, B, and maybe A).  Default is black.
	Stroke []byte
}

func (b Cross) Draw(pdf *PDF) (err error) {
	var (
		c      *Cursor
		gstate Name
		sb     strings.Builder
	)
	// Check parameters.
	if b.Rectangle.LLX >= b.Rectangle.URX || b.Rectangle.LLY >= b.Rectangle.URY {
		return errors.New("invalid Rectangle")
	}
	if b.Page == 0 {
		b.Page = 1
	} else if b.Page < 0 {
		return errors.New("invalid Page")
	}
	if b.LineWidth == 0 {
		b.LineWidth = 1
	} else if b.LineWidth < 0 {
		return errors.New("invalid LineWidth")
	}
	if len(b.Stroke) != 0 && len(b.Stroke) != 3 && len(b.Stroke) != 4 {
		return errors.New("invalid Stroke")
	}
	if len(b.Stroke) == 4 && b.Stroke[3] == 0 {
		return nil // alpha=0 means nothing to do
	}
	if len(b.Stroke) == 0 {
		b.Stroke = []byte{0, 0, 0, 255}
	}
	if len(b.Stroke) == 3 {
		b.Stroke = append(b.Stroke, 255)
	}
	// Get the page cursor.
	if c, err = pdf.CursorForPage(b.Page); err != nil {
		return err
	}
	// Create the alpha graphic state if needed.
	if gstate, err = maybeAddAlpha(c, nil, b.Stroke); err != nil {
		return err
	}
	// Draw the cross.
	sb.WriteString("q")
	if gstate != "" {
		fmt.Fprintf(&sb, " %s gs", EncodeName(gstate))
	}
	fmt.Fprintf(&sb, " %.2f %.2f %.2f %.2f re W n %d %d %d RG 0 J %.2f w %.2f %.2f m %.2f %.2f l %.2f %.2f m %.2f %.2f l s Q",
		b.Rectangle.LLX, b.Rectangle.LLY,
		b.Rectangle.URX-b.Rectangle.LLX, b.Rectangle.URY-b.Rectangle.LLY,
		b.Stroke[0], b.Stroke[1], b.Stroke[2], b.LineWidth,
		b.Rectangle.LLX, b.Rectangle.LLY,
		b.Rectangle.URX, b.Rectangle.URY,
		b.Rectangle.URX, b.Rectangle.LLY,
		b.Rectangle.LLX, b.Rectangle.URY)
	return pdf.AddPageContent(b.Page, sb.String())
}

// Circle is a structure containing all of the parameters for drawing a circle.
// To draw a circle, create a Circle structure and call its Draw method.
type Circle struct {
	// Center gives the center point of the circle.  Required.
	Center Point
	// Radius gives the radius of the circle.  Required.
	Radius float64
	// Page gives the page number to draw on.  Default is 1.
	Page int
	// Fill gives the fill color for the circle, as an array of three or
	// four bytes (R, G, B, and maybe A).  Default is no fill.
	Fill []byte
	// Stroke gives the stroke color for the circle, as an array of three or
	// four bytes (R, G, B, and maybe A).  Default is no stroke.
	Stroke []byte
}

func (b Circle) Draw(pdf *PDF) (err error) {
	var (
		c      *Cursor
		gstate Name
		sb     strings.Builder
	)
	// Check parameters.
	if b.Radius <= 0 {
		return errors.New("invalid Radius")
	}
	if b.Page == 0 {
		b.Page = 1
	} else if b.Page < 0 {
		return errors.New("invalid Page")
	}
	if len(b.Fill) != 0 && len(b.Fill) != 3 && len(b.Fill) != 4 {
		return errors.New("invalid Fill")
	}
	if len(b.Fill) == 4 && b.Fill[3] == 0 {
		b.Fill = nil // alpha=0 is the same as no fill
	}
	if len(b.Fill) == 3 {
		b.Fill = append(b.Fill, 255)
	}
	if len(b.Stroke) != 0 && len(b.Stroke) != 3 && len(b.Stroke) != 4 {
		return errors.New("invalid Stroke")
	}
	if len(b.Stroke) == 4 && b.Stroke[3] == 0 {
		b.Stroke = nil // alpha=0 is the same as no stroke
	}
	if len(b.Stroke) == 3 {
		b.Stroke = append(b.Stroke, 255)
	}
	if len(b.Fill) == 0 && len(b.Stroke) == 0 {
		return nil // nothing to do
	}
	// Get the page cursor.
	if c, err = pdf.CursorForPage(b.Page); err != nil {
		return err
	}
	// Create the alpha graphic state if needed.
	if gstate, err = maybeAddAlpha(c, b.Fill, b.Stroke); err != nil {
		return err
	}
	// Draw the circle.
	sb.WriteString("q")
	if gstate != "" {
		fmt.Fprintf(&sb, " %s gs", EncodeName(gstate))
	}
	if len(b.Fill) != 0 {
		fmt.Fprintf(&sb, " %d %d %d rg", b.Fill[0], b.Fill[1], b.Fill[2])
	}
	if len(b.Stroke) != 0 {
		fmt.Fprintf(&sb, " %d %d %d RG", b.Stroke[0], b.Stroke[1], b.Stroke[2])
	}
	fmt.Fprintf(&sb, " %.2f %.2f m", b.Center.X-b.Radius, b.Center.Y)
	fmt.Fprintf(&sb, " %.2f %.2f %.2f %.2f %.2f %.2f c",
		b.Center.X-b.Radius, b.Center.Y-b.Radius*circleControlPointDistance,
		b.Center.X-b.Radius*circleControlPointDistance, b.Center.Y-b.Radius,
		b.Center.X, b.Center.Y-b.Radius)
	fmt.Fprintf(&sb, " %.2f %.2f %.2f %.2f %.2f %.2f c",
		b.Center.X+b.Radius*circleControlPointDistance, b.Center.Y-b.Radius,
		b.Center.X+b.Radius, b.Center.Y-b.Radius*circleControlPointDistance,
		b.Center.X+b.Radius, b.Center.Y)
	fmt.Fprintf(&sb, " %.2f %.2f %.2f %.2f %.2f %.2f c",
		b.Center.X+b.Radius, b.Center.Y+b.Radius*circleControlPointDistance,
		b.Center.X+b.Radius*circleControlPointDistance, b.Center.Y+b.Radius,
		b.Center.X, b.Center.Y+b.Radius)
	fmt.Fprintf(&sb, " %.2f %.2f %.2f %.2f %.2f %.2f c h",
		b.Center.X-b.Radius*circleControlPointDistance, b.Center.Y+b.Radius,
		b.Center.X-b.Radius, b.Center.Y+b.Radius*circleControlPointDistance,
		b.Center.X-b.Radius, b.Center.Y)
	if len(b.Fill) != 0 && len(b.Stroke) != 0 {
		sb.WriteString(" b")
	} else if len(b.Fill) != 0 {
		sb.WriteString(" f")
	} else {
		sb.WriteString(" s")
	}
	sb.WriteString(" Q")
	return pdf.AddPageContent(b.Page, sb.String())
}

func maybeAddAlpha(pageC *Cursor, fill, stroke []byte) (name Name, err error) {
	var (
		fillAlpha   byte
		strokeAlpha byte
		resources   Dict
		extGState   Dict
		state       Dict
		c           *Cursor
	)
	if len(fill) > 3 {
		fillAlpha = fill[3]
	} else {
		fillAlpha = 255
	}
	if len(stroke) > 3 {
		strokeAlpha = stroke[3]
	} else {
		strokeAlpha = 255
	}
	if fillAlpha == 255 && strokeAlpha == 255 {
		return "", nil
	}
	name = Name(fmt.Sprintf("F%dS%d", fillAlpha, strokeAlpha))
	c = pageC.Clone().Key("Resources")
	if resources, err = c.Dict(); err != nil {
		return "", err
	}
	if _, ok := resources["ExtGState"]; !ok {
		extGState = Dict{}
		if err = c.SetKey("ExtGState", extGState); err != nil {
			return "", err
		}
	} else if extGState, err = c.Key("ExtGState").Dict(); err != nil {
		return "", err
	}
	if _, ok := extGState[name]; ok {
		return name, nil
	}
	state = Dict{"Type": Name("ExtGState")}
	if fillAlpha != 255 {
		state["ca"] = float64(fillAlpha) / 255.0
	}
	if strokeAlpha != 255 {
		state["CA"] = float64(strokeAlpha) / 255.0
	}
	return name, c.SetKey(name, state)
}
