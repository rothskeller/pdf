package pdfstruct

import (
	"errors"
	"fmt"
)

// GetArray gets the array object specified by the reference.
func (p *PDF) GetArray(r Reference) (array Array, err error) {
	var obj Object

	if obj, err = p.Get(r); err != nil {
		return nil, err
	}
	switch obj := obj.(type) {
	case Array:
		return obj, nil
	default:
		return nil, errors.New("specified object is not an Array")
	}
}

// GetDict gets the dict object specified by the reference.
func (p *PDF) GetDict(r Reference) (dict Dict, err error) {
	var obj Object

	if obj, err = p.Get(r); err != nil {
		return nil, err
	}
	switch obj := obj.(type) {
	case Dict:
		return obj, nil
	default:
		return nil, errors.New("specified object is not a Dict")
	}
}

// GetStream gets the stream object specified by the reference.
func (p *PDF) GetStream(r Reference) (stream Stream, err error) {
	var obj Object

	if obj, err = p.Get(r); err != nil {
		return Stream{}, err
	}
	switch obj := obj.(type) {
	case Stream:
		return obj, nil
	default:
		return Stream{}, errors.New("specified object is not a Stream")
	}
}

// GetString gets the string object specified by the reference.
func (p *PDF) GetString(r Reference) (str string, err error) {
	var obj Object

	if obj, err = p.Get(r); err != nil {
		return "", err
	}
	switch obj := obj.(type) {
	case string:
		return obj, nil
	default:
		return "", errors.New("specified object is not a string")
	}
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
		if obj, err = p.Get(Reference{xe.stream, 0}); err != nil {
			return nil, fmt.Errorf("reading stream %d containing object %d: %s", xe.stream, r.Number, err)
		}
		var str Stream
		var ok bool
		if str, ok = obj.(Stream); !ok {
			return nil, fmt.Errorf("reading stream %d containing object %d: object %d is not a stream", xe.stream, r.Number, xe.stream)
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
