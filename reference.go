package pdf

// This file contains code that knows how to resolve references and retrieve
// objects from the PDF structure.

import (
	"errors"
	"fmt"
)

// NumPages returns the number of pages in the PDF.
func (pdf *PDF) NumPages() (count int, err error) {
	return pdf.Cursor().Key("Root").Key("Pages").Key("Count").Int()
}

// GetArray gets the object as an Array.
func (p *PDF) GetArray(o Object) (array Array, err error) {
	switch o := o.(type) {
	case Array:
		return o, nil
	case Reference:
		if obj, err := p.Get(o); err != nil {
			return nil, err
		} else if obj, ok := obj.(Array); ok {
			return obj, nil
		}
	}
	return nil, errors.New("not an Array or Reference to Array")
}

// GetDict gets the object as a Dict.
func (p *PDF) GetDict(o Object) (dict Dict, err error) {
	switch o := o.(type) {
	case Dict:
		return o, nil
	case Reference:
		if obj, err := p.Get(o); err != nil {
			return nil, err
		} else if obj, ok := obj.(Dict); ok {
			return obj, nil
		}
	}
	return nil, errors.New("not a Dict or Reference to Dict")
}

// GetStream gets object as a Stream.
func (p *PDF) GetStream(o Object) (stream Stream, err error) {
	switch o := o.(type) {
	case Stream:
		return o, nil
	case Reference:
		if obj, err := p.Get(o); err != nil {
			return stream, err
		} else if obj, ok := obj.(Stream); ok {
			return obj, nil
		}
	}
	return stream, errors.New("not a Stream or Reference to Stream")
}

// GetString gets the object as a String.
func (p *PDF) GetString(o Object) (str string, err error) {
	switch o := o.(type) {
	case string:
		return o, nil
	case Reference:
		if obj, err := p.Get(o); err != nil {
			return "", err
		} else if obj, ok := obj.(string); ok {
			return obj, nil
		}
	}
	return "", errors.New("not a String or Reference to String")
}

// GetNumber gets the object as a real number.
func (p *PDF) GetNumber(o Object) (num float64, err error) {
	switch o := o.(type) {
	case float64:
		return o, nil
	case int:
		return float64(o), nil
	case Reference:
		if obj, err := p.Get(o); err != nil {
			return 0, err
		} else if f, ok := obj.(float64); ok {
			return f, nil
		} else if obj, ok := obj.(int); ok {
			return float64(obj), nil
		}
	}
	return 0, errors.New("not a Number or Reference to Number")
}

// Get returns the object specified by the reference.
func (p *PDF) Get(r Reference) (obj Object, err error) {
	if r.Number < 1 || r.Number >= len(p.xref) {
		return nil, fmt.Errorf("object number %d is out of range for document (max %d)", r.Number, len(p.xref)-1)
	}
	switch xe := p.xref[r.Number].(type) {
	case xrefFree:
		return nil, fmt.Errorf("object number %d is on the free list", r.Number)
	case xrefDirect:
		if xe.gen != r.Generation {
			return nil, fmt.Errorf("object number %d has generation %d but %d was requested", r.Number, xe.gen, r.Generation)
		}
		if xe.cache != nil {
			return xe.cache, nil
		}
		if obj, err = p.readObjectAt(xe.offset); err != nil {
			return nil, fmt.Errorf("reading object number %d: %s", r.Number, err)
		}
		xe.cache = obj
		return obj, nil
	case xrefStream:
		if r.Generation != 0 {
			return nil, fmt.Errorf("object number %d is in an object stream but has a nonzero generation number", r.Number)
		}
		if xe.cache != nil {
			return xe.cache, nil
		}
		var str Stream
		if str, err = p.GetStream(Reference{xe.stream, 0}); err != nil {
			return nil, fmt.Errorf("reading stream %d containing object %d: %s", xe.stream, r.Number, err)
		}
		str.Decompress(0)
		if obj, err = extractObjectFromStream(str, xe.index); err != nil {
			return nil, fmt.Errorf("extracting object %d from stream %d at index %d: %s", r.Number, xe.stream, xe.index, err)
		}
		xe.cache = obj
		return obj, nil
	default:
		// This is an object that we've added, which hasn't been written
		// to the file yet.  The xref table contains the actual object.
		return p.xref[r.Number], nil
	}
}

func extractObjectFromStream(s Stream, idx int) (obj Object, err error) {
	var first, offset int

	// Verify that the index is in the stream, and get basic stream info.
	if ty, ok := s.Dict["Type"].(Name); !ok || ty != "ObjStm" {
		return nil, errors.New("stream is not an object stream")
	}
	if n, ok := s.Dict["N"].(int); !ok || idx < 0 || idx >= n {
		return nil, errors.New("index out of range for object stream")
	}
	if f, ok := s.Dict["First"].(int); ok {
		first = f
	} else {
		return nil, errors.New("object stream missing First value")
	}
	// Read the integers from the stream header to get the offset of the
	// object desired.
	for i := 0; i < idx*2+1; i++ {
		var delta int
		if obj, delta, err = readObjectFrom(s.Data[offset:]); err != nil {
			return nil, fmt.Errorf("reading integer in stream header at offset %d: %s", offset, err)
		}
		if _, ok := obj.(int); !ok {
			return nil, fmt.Errorf("expected integer in stream header at offset %d", offset)
		}
		offset += delta
	}
	if obj, _, err = readObjectFrom(s.Data[offset:]); err != nil {
		return nil, fmt.Errorf("reading integer in stream header at offset %d: %s", offset, err)
	}
	switch obj := obj.(type) {
	case int:
		offset = first + obj
	default:
		return nil, fmt.Errorf("expected integer in stream header at offset %d", offset)
	}
	// Read the object at that position.
	if obj, _, err = readObjectFrom(s.Data[offset:]); err != nil {
		return nil, fmt.Errorf("reading object in stream at offset %d: %s", offset, err)
	}
	return obj, nil
}
