// Package pdfstruct provides methods for reading and updating the basic
// structure of a PDF.  It doesn't understand the semantics of the PDF at all;
// it just knows how to locate, parse, update, add, and remove objects.
package pdfstruct

import (
	"bytes"
	"errors"
	"fmt"
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
	fh      Reader
	start   int
	xref    []any
	Info    Dict
	Catalog Dict
	updates map[Reference]Object
}

// Reader is the interface that must be satisfied by any file passed to Open.
type Reader interface {
	io.Seeker
	io.ReaderAt
}

// Open opens a PDF file.  The supplied file handle must honor io.ReadSeeker,
// but if Write is going to be called, it must also honor io.Writer.
func Open(fh Reader) (p *PDF, err error) {
	p = &PDF{fh: fh, Info: make(Dict)}
	if err = p.verifySignature(); err != nil {
		return nil, err
	}
	if err = p.readXRef(); err != nil {
		return nil, err
	}
	switch root := p.Info["Root"].(type) {
	case Reference:
		var obj Object
		if obj, err = p.Get(root); err != nil {
			return nil, fmt.Errorf("reading document catalog: %s", err)
		}
		switch catalog := obj.(type) {
		case Dict:
			p.Catalog = catalog
		default:
			return nil, fmt.Errorf("document catalog is %T, not Dict", catalog)
		}
	default:
		return nil, fmt.Errorf("document Root is %T, not Reference", root)
	}
	return p, nil
}

func (p *PDF) verifySignature() (err error) {
	var buf [5]byte
	if _, err = p.fh.ReadAt(buf[:], 0); err != nil {
		return fmt.Errorf("verify signature: %s", err)
	}
	if !bytes.Equal(buf[:], []byte("%PDF-")) {
		return errors.New("not a PDF file")
	}
	return nil
}
