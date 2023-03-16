package pdfstruct

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// UpdateObject registers new content for the object with the specified
// reference.  The new content will be written if Write is called.
func (p *PDF) UpdateObject(ref Reference, obj Object) {
	if p.updates == nil {
		p.updates = make(map[Reference]Object)
	}
	p.updates[ref] = obj
}

// CreateObject creates a new object with the specified content, and returns a
// reference to it.  The new content will be written if Write is called.
func (p *PDF) CreateObject(obj Object) (ref Reference) {
	if p.updates == nil {
		p.updates = make(map[Reference]Object)
	}
	ref.Number = len(p.xref)
	p.xref = append(p.xref, obj)
	p.updates[ref] = obj
	return ref
}

// Write updates the PDF in place to save the updated objects previously passed
// to UpdateObject.  For this to work, the file handle passed to Open must
// support io.WriteSeeker.  The caller needs to close the file when finished.
func (p *PDF) Write() (err error) {
	var (
		wr      io.WriteSeeker
		offset  int64
		xref    int64
		updates = make([]Reference, 0, len(p.updates))
		offsets = make([]int, len(p.updates))
	)
	if len(p.updates) == 0 {
		return nil
	}
	if w, ok := p.fh.(io.WriteSeeker); ok {
		wr = w
	} else {
		return errors.New("file handle not writable")
	}
	if offset, err = wr.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	for u := range p.updates {
		updates = append(updates, u)
	}
	sort.Slice(updates, func(i, j int) bool {
		return updates[i].Number < updates[j].Number
	})
	for i, ref := range updates {
		offsets[i] = int(offset)
		if err = writeObject(wr, ref, p.updates[ref]); err != nil {
			return err
		}
		if offset, err = wr.Seek(0, io.SeekCurrent); err != nil {
			return err
		}
	}
	xref = offset
	var xdnum int
	if xdnum, err = writeXRefDict(p, wr, p.start, updates); err != nil {
		return err
	}
	updates = append(updates, Reference{Number: xdnum})
	offsets = append(offsets, int(xref))
	if err = writeXRefStream(wr, updates, offsets); err != nil {
		return err
	}
	if err = writeStartXRef(wr, int(xref)); err != nil {
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
		_, err = fmt.Fprint(wr, encodeString(obj))
	case []byte:
		_, err = fmt.Fprint(wr, encodeHexString(obj))
	case Name:
		_, err = fmt.Fprint(wr, encodeName(obj))
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
			if _, err = fmt.Fprintf(wr, " %s ", encodeName(k)); err != nil {
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
			if _, err = fmt.Fprintf(wr, " %s ", encodeName(k)); err != nil {
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
	for k, v := range p.Info {
		xd[k] = v
	}
	if a, ok := xd["ID"].(Array); ok {
		if len(a) == 2 {
			var id2 [16]byte
			rand.Read(id2[:])
			a[1] = id2[:]
		}
	}
	xd["Prev"] = prev
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

func encodeString(s string) string {
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

func encodeHexString(by []byte) string {
	return "<" + hex.EncodeToString(by) + ">"
}

func encodeName(n Name) string {
	var by = []byte(string(n))
	var sb strings.Builder
	sb.WriteByte('/')
	for _, b := range by {
		if isRegularChar(b) {
			sb.WriteByte(b)
		} else {
			fmt.Fprintf(&sb, "#%2X", b)
		}
	}
	return sb.String()
}
