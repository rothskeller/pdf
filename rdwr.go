package pdf

// This file contains the code that knows how to read and write PDF files.

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// pdfHeader is the header to start a new PDF file.  The four hex bytes don't
// have any specific meaning; the spec just calls for four bytes with high bit
// set.
var pdfHeader = []byte("%PDF-1.5\r\n%\xE2\xE3\xCF\xD3\r\n")

// New creates a new PDF file.  After objects are added to it, Write must be
// called on it.
func New(wh io.WriteSeeker) (pdf *PDF) {
	var now = strings.Replace(time.Now().Format("20060102150405Z07:00"), ":", "'", 1)

	pdf = &PDF{
		wh: wh,
		Info: Dict{
			"CreationTime": "D: " + now,
			"ModTime":      "D: " + now,
			"Producer":     "https://github.com/rothskeller/pdf",
		},
		Catalog: Dict{},
		Trailer: Dict{},
	}
	pdf.Trailer["Info"] = pdf.CreateObject(pdf.Info)
	pdf.Catalog["Type"] = Name("Catalog")
	pdf.Catalog["Pages"] = pdf.CreateObject(Dict{
		"Type": Name("Pages"), "Kids": Array{}, "Count": 0,
	})
	pdf.Trailer["Root"] = pdf.CreateObject(pdf.Catalog)
	return pdf
}

// Reader is the interface that must be satisfied by any file passed to Open.
type Reader interface {
	io.Seeker
	io.ReaderAt
}

// Open opens an existing PDF file.
func Open(fh Reader) (p *PDF, err error) {
	p = &PDF{rh: fh, Info: make(Dict), Trailer: make(Dict)}
	p.wh, _ = fh.(io.WriteSeeker)
	if err = p.verifySignature(); err != nil {
		return nil, err
	}
	if err = p.readXRef(); err != nil {
		return nil, err
	}
	if obj, err := p.Fetch(p.Trailer["Root"].(Reference)); err != nil {
		return nil, err
	} else if _, ok := obj.(Dict); !ok {
		return nil, fmt.Errorf("/Root is %T, not Dict", obj)
	} else {
		p.Catalog = obj.(Dict)
	}
	return p, nil
}

func (p *PDF) verifySignature() (err error) {
	var buf [5]byte
	if _, err = p.rh.ReadAt(buf[:], 0); err != nil {
		return fmt.Errorf("verify signature: %s", err)
	}
	if !bytes.Equal(buf[:], []byte("%PDF-")) {
		return errors.New("not a PDF file")
	}
	return nil
}

// Write updates the PDF in place to save the updated objects previously passed
// to UpdateObject.  For this to work, the receiver must have been created by
// New, or by Open with a file handle that supports io.WriteSeeker.  The caller
// needs to close the file when finished.
func (p *PDF) Write() (err error) {
	var (
		offset  int64
		xref    int64
		updates = make([]Reference, 0, len(p.updates))
		offsets = make([]int, len(p.updates))
	)
	if len(p.updates) == 0 {
		return nil
	}
	if p.wh == nil {
		return errors.New("file handle not writable")
	}
	if offset, err = p.wh.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	if offset == 0 {
		// This is a new file.  Add a header.
		if _, err = p.wh.Write(pdfHeader); err != nil {
			return err
		}
	}
	for u := range p.updates {
		updates = append(updates, u)
	}
	sort.Slice(updates, func(i, j int) bool {
		return updates[i].Number < updates[j].Number
	})
	for i, ref := range updates {
		offsets[i] = int(offset)
		if err = writeObject(p.wh, ref, p.updates[ref]); err != nil {
			return err
		}
		if offset, err = p.wh.Seek(0, io.SeekCurrent); err != nil {
			return err
		}
	}
	xref = offset
	var xdnum int
	if xdnum, err = writeXRefDict(p, p.wh, p.start, updates); err != nil {
		return err
	}
	updates = append(updates, Reference{Number: xdnum})
	offsets = append(offsets, int(xref))
	if err = writeXRefStream(p.wh, updates, offsets); err != nil {
		return err
	}
	if err = writeStartXRef(p.wh, int(xref)); err != nil {
		return err
	}
	return nil
}

func writeObject(wr io.Writer, ref Reference, obj Object) (err error) {
	if _, err = fmt.Fprintf(wr, "%d %d obj ", ref.Number, ref.Generation); err != nil {
		return err
	}
	if err = writeRawObject(wr, obj); err != nil {
		return err
	}
	if _, err = fmt.Fprint(wr, " endobj\r\n"); err != nil {
		return err
	}
	return nil
}

func writeRawObject(wr io.Writer, obj Object) (err error) {
	switch obj := obj.(type) {
	case nil:
		_, err = fmt.Fprint(wr, "null")
	case bool, int:
		_, err = fmt.Fprint(wr, obj)
	case float64:
		_, err = fmt.Fprintf(wr, "%f", obj)
	case string:
		_, err = fmt.Fprint(wr, EncodeString(obj))
	case []byte:
		_, err = fmt.Fprint(wr, EncodeHexString(obj))
	case Name:
		_, err = fmt.Fprint(wr, EncodeName(obj))
	case Array:
		if _, err = fmt.Fprint(wr, "[ "); err != nil {
			return err
		}
		for i, o := range obj {
			if i != 0 {
				if _, err = fmt.Fprint(wr, " "); err != nil {
					return err
				}
			}
			if err = writeRawObject(wr, o); err != nil {
				return err
			}
		}
		_, err = fmt.Fprint(wr, " ]")
	case Dict:
		if _, err = fmt.Fprint(wr, "<<"); err != nil {
			return err
		}
		for k, v := range obj {
			if _, err = fmt.Fprintf(wr, " %s ", EncodeName(k)); err != nil {
				return err
			}
			if err = writeRawObject(wr, v); err != nil {
				return err
			}
		}
		_, err = fmt.Fprint(wr, " >>")
	case Stream:
		obj.Dict["Length"] = len(obj.Data)
		if _, err = fmt.Fprint(wr, "<<"); err != nil {
			return err
		}
		for k, v := range obj.Dict {
			if _, err = fmt.Fprintf(wr, " %s ", EncodeName(k)); err != nil {
				return err
			}
			if err = writeRawObject(wr, v); err != nil {
				return err
			}
		}
		if _, err = fmt.Fprint(wr, " >> stream\n"); err != nil {
			return err
		}
		if _, err = wr.Write(obj.Data); err != nil {
			return err
		}
		_, err = fmt.Fprint(wr, "\nendstream")
	case Reference:
		_, err = fmt.Fprintf(wr, "%d %d R", obj.Number, obj.Generation)
	default:
		return errors.New("unsupported object type")
	}
	return err
}

func writeXRefDict(p *PDF, wr io.Writer, prev int, refs []Reference) (xdnum int, err error) {
	var xd = make(Dict)
	for k, v := range p.Trailer {
		xd[k] = v
	}
	if a, ok := xd["ID"].(Array); ok {
		if len(a) == 2 {
			var id2 [16]byte
			rand.Read(id2[:])
			a[1] = id2[:]
		}
	}
	if prev != 0 {
		xd["Prev"] = prev
	}
	xd["Length"] = 6 * (len(refs) + 1)
	xd["Type"] = Name("XRef")
	xdnum = len(p.xref)
	var index Array
	for _, r := range refs {
		index = append(index, r.Number, 1)
	}
	index = append(index, xdnum, 1)
	xd["Index"] = index
	xd["Size"] = xdnum + 1
	xd["W"] = Array{1, 4, 1}
	if _, err = fmt.Fprintf(wr, "%d 0 obj ", xdnum); err != nil {
		return 0, err
	}
	return xdnum, writeRawObject(wr, xd)
}

func writeXRefStream(wr io.Writer, refs []Reference, offsets []int) (err error) {
	var buf [6]byte
	if _, err = fmt.Fprint(wr, " stream\r\n"); err != nil {
		return err
	}
	for i := range refs {
		buf[0] = 1
		binary.BigEndian.PutUint32(buf[1:], uint32(offsets[i]))
		buf[5] = byte(refs[i].Generation)
		if _, err = wr.Write(buf[:]); err != nil {
			return err
		}
	}
	_, err = fmt.Fprint(wr, "\r\nendstream endobj\r\n")
	return err
}

func writeStartXRef(wr io.Writer, start int) (err error) {
	_, err = fmt.Fprintf(wr, "startxref\r\n%d\r\n%%%%EOF\r\n", start)
	return err
}

func EncodeString(s string) string {
	var sb strings.Builder
	var by = []byte(s)
	sb.WriteByte('(')
	for _, b := range by {
		switch b {
		case '\r':
			sb.WriteByte('\\')
			sb.WriteByte('r')
		case '\\', '(', ')':
			sb.WriteByte('\\')
			sb.WriteByte(b)
		default:
			sb.WriteByte(b)
		}
	}
	sb.WriteByte(')')
	return sb.String()
}

func EncodeHexString(by []byte) string {
	return "<" + hex.EncodeToString(by) + ">"
}

func EncodeName(n Name) string {
	var by = []byte(string(n))
	var sb strings.Builder
	sb.WriteByte('/')
	for _, b := range by {
		if isRegularChar(b) && b != '#' {
			sb.WriteByte(b)
		} else {
			fmt.Fprintf(&sb, "#%2X", b)
		}
	}
	return sb.String()
}
