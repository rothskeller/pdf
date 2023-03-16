package pdfstruct

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
)

// xrefFree is a free-list entry in the cross-reference table.
type xrefFree struct {
	next int
	gen  int
}

// xrefDirect is a direct object entry in the cross-reference table.
type xrefDirect struct {
	offset int
	gen    int
	cache  Object
}

// xrefStream is a cross-reference entry for an object within a stream.
type xrefStream struct {
	stream int
	index  int
	cache  Object
}

// readXRef reads all of the cross reference sections from the PDF and builds a
// merged cross-reference table.
func (p *PDF) readXRef() (err error) {
	var addr int

	if err = p.readStartXRef(); err != nil {
		return fmt.Errorf(`reading "startxref": %s`, err)
	}
	addr = p.start
	for addr != 0 {
		var next int
		if next, err = p.readXRefSection(addr); err != nil {
			return fmt.Errorf("reading xref section at offset %d: %s", addr, err)
		}
		addr = next
	}
	return
}

var xrefAddrRE = regexp.MustCompile(`(?:\r|\n|\r\n)startxref(?:\r|\n|\r\n)(\d+)(?:\r|\n|\r\n)%%EOF(?:\r|\n|\r\n)?$`)

// readStartXRef finds the "startxref" keyword at the end of the file and reads
// the integer on the line after it, which is the offset to the first cross
// reference section.
func (p *PDF) readStartXRef() (err error) {
	var (
		end   int64
		buf   [64]byte
		match [][]byte
	)
	if end, err = p.fh.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	if _, err = p.fh.ReadAt(buf[:], end-int64(len(buf))); err != nil {
		return err
	}
	if match = xrefAddrRE.FindSubmatch(buf[:]); match == nil {
		return errors.New(`no "startxref" found at end of file`)
	}
	p.start, _ = strconv.Atoi(string(match[1]))
	return nil
}

// readXRefSection reads the cross reference section at the specified address
// into the table.  Because the sections are read in reverse "chronological"
// order, only those entries not already existing in the table are added to it.
// readXRefSection returns the address of the next earlier section, which is
// zero for the earliest section.
func (p *PDF) readXRefSection(addr int) (prev int, err error) {
	var (
		buf [5]byte
		n   int
	)
	// There are two different kinds of cross-reference section: tables and
	// streams.  Tables start with the word "xref", so look to see if
	// that's present.
	if n, err = p.fh.ReadAt(buf[:], int64(addr)); err != nil || n < 5 {
		return
	}
	if bytes.Equal(buf[:4], []byte("xref")) && (buf[4] == '\r' || buf[4] == '\n') {
		// Found the xref keyword, so it's a cross reference table.
		return p.readXRefTable(addr)
	}
	// It's a cross reference stream.
	return p.readXRefStream(addr)
}

var xrefLineRE = regexp.MustCompile(`^(\d{10}) (\d{5}) ([nf]) ?$`)

// readXRefTable reads an old-style cross-reference table.
func (p *PDF) readXRefTable(addr int) (prev int, err error) {
	var (
		buf [20]byte
		obj Object
	)
	// Skip the "xref" line.
	if _, err = p.fh.ReadAt(buf[:6], int64(addr)); err != nil {
		return 0, err
	}
	if buf[4] == '\r' && buf[5] == '\n' {
		addr += 6
	} else {
		addr += 5
	}
	// Repeat reading xref table sections until we see "trailer".
	for {
		if _, err = p.fh.ReadAt(buf[:], int64(addr)); err != nil {
			return 0, err
		}
		if bytes.HasPrefix(buf[:], []byte("trailer")) && (buf[7] == '\r' || buf[7] == '\n') {
			if buf[7] == '\r' && buf[8] == '\n' {
				addr += 9
			} else {
				addr += 8
			}
			break
		}
		if addr, err = p.readXRefTableSection(addr, buf[:]); err != nil {
			return 0, err
		}
	}
	// Read the trailer dictionary and merge its data into the PDF info,
	// except for two special-case keys.  Prev gets returned; XRefStm
	// points to an xref stream that gets read.
	if obj, err = p.readObjectAt(addr); err != nil {
		return 0, fmt.Errorf("reading trailer dict at offset %d: %s", addr, err)
	}
	switch obj := obj.(type) {
	case Dict:
		for key, val := range obj {
			switch key {
			case "Prev":
				switch val := val.(type) {
				case int:
					prev = val
				default:
					return 0, fmt.Errorf("value of /Prev should be an integer in trailer dict at offset %d", addr)
				}
			case "XRefStm":
				switch val := val.(type) {
				case int:
					if _, err = p.readXRefStream(val); err != nil {
						return 0, err
					}
				default:
					return 0, fmt.Errorf("value of /XRefStm should be an integer in trailer dict at offset %d", addr)
				}
			default:
				if _, ok := p.Info[key]; !ok {
					p.Info[key] = val
				}
			}
		}
	default:
		return 0, fmt.Errorf(`expected dict after "trailer" at offset %d`, addr)
	}
	return prev, nil
}

// readXRefTableSection reads a single section of an xref table:  that is, a
// line containing a starting object number and count, followed by count lines
// containing xref entries for those objects.  It returns the resulting address.
func (p *PDF) readXRefTableSection(addr int, line []byte) (_ int, err error) {
	var start, count int
	// Line should have a starting object number and a count of
	// objects starting at that number.
	if idx := bytes.IndexAny(line, "\r\n"); idx >= 0 {
		var n int
		if n, err = fmt.Sscanf(string(line[:idx]), "%d %d", &start, &count); err != nil || n != 2 {
			return 0, fmt.Errorf("invalid cross-reference table section header at offset %d", addr)
		}
		if line[idx] == '\r' && idx < len(line)-1 && line[idx+1] == '\n' {
			addr += idx + 2
		} else {
			addr += idx + 1
		}
	} else {
		return 0, fmt.Errorf("invalid cross-reference table section header at offset %d", addr)
	}
	// Extend the table to have room for all objects in the group.
	if len(p.xref) < start+count {
		t := make([]any, start+count)
		copy(t, p.xref)
		p.xref = t
	}
	// Subsequent lines, one per object in the group, have either offset,
	// generation, "n" (for active objects) or next pointer, generation, "f"
	// (for free list entries).  They are always 20 bytes, which is the size
	// of the line buffer passed to us.
	for i := 0; i < count; i, addr = i+1, addr+20 {
		if p.xref[start+i] != nil {
			continue
		}
		if _, err = p.fh.ReadAt(line, int64(addr)); err != nil {
			return 0, fmt.Errorf("reading cross-reference table entry at offset %d: %s", addr, err)
		}
		switch line[17] {
		case 'n':
			var xd xrefDirect
			if xd.offset, err = strconv.Atoi(string(line[:10])); err != nil {
				return 0, fmt.Errorf("invalid cross-reference table entry at offset %d", addr)
			}
			if xd.gen, err = strconv.Atoi(string(line[11:16])); err != nil {
				return 0, fmt.Errorf("invalid cross-reference table entry at offset %d", addr)
			}
			p.xref[start+i] = xd
		case 'f':
			var xf xrefFree
			if xf.next, err = strconv.Atoi(string(line[:10])); err != nil {
				return 0, fmt.Errorf("invalid cross-reference table entry at offset %d", addr)
			}
			if xf.gen, err = strconv.Atoi(string(line[11:16])); err != nil {
				return 0, fmt.Errorf("invalid cross-reference table entry at offset %d", addr)
			}
			p.xref[start+i] = xf
		default:
			return 0, fmt.Errorf("invalid cross-reference table entry at offset %d", addr)
		}
	}
	return addr, nil
}

// readXRefStream reads the newer cross-reference stream and adds its data to
// the document cross-reference table.
func (p *PDF) readXRefStream(addr int) (prev int, err error) {
	var (
		obj   Object
		str   Stream
		ok    bool
		index []int
		w     []int
		data  []byte
	)
	if obj, err = p.readObjectAt(addr); err != nil {
		return 0, fmt.Errorf("reading xref stream at offset %d: %s", addr, err)
	}
	if str, ok = obj.(Stream); !ok {
		return 0, fmt.Errorf("expected xref stream at offset %d", addr)
	}
	if str.Dict["Type"] != Name("XRef") {
		return 0, fmt.Errorf(`expected /Type "XRef" in xref stream at offset %d`, addr)
	}
	for key, val := range str.Dict {
		switch key {
		case "Prev":
			switch val := val.(type) {
			case int:
				prev = val
			default:
				return 0, fmt.Errorf("value of /Prev should be integer in xref stream at offset %d", addr)
			}
		case "Index":
			index = index[:0]
			switch val := val.(type) {
			case Array:
				for _, vi := range val {
					switch vi := vi.(type) {
					case int:
						index = append(index, vi)
					default:
						return 0, fmt.Errorf("value of element of /Index should be integer in xref stream at offset %d", addr)
					}
				}
				if len(index) < 2 || len(index)%2 != 0 {
					return 0, fmt.Errorf("invalid number of elements in /Index in xref stream at offset %d", addr)
				}
			default:
				return 0, fmt.Errorf("value of /Index should be array in xref stream at offset %d", addr)
			}
		case "Size":
			switch val := val.(type) {
			case int:
				if len(index) == 0 {
					index = append(index, 0, val)
				}
			default:
				return 0, fmt.Errorf("value of /Size should be integer in xref stream at offset %d", addr)
			}
			if _, ok := p.Info[key]; !ok {
				p.Info[key] = val
			}
		case "W":
			switch val := val.(type) {
			case Array:
				if len(val) != 3 {
					return 0, fmt.Errorf("value of /W should be array of length 3 in xref stream at offset %d", addr)
				}
				for _, vi := range val {
					switch vi := vi.(type) {
					case int:
						w = append(w, vi)
					default:
						return 0, fmt.Errorf("value of element of /W should be integer in xref stream at offset %d", addr)
					}
				}
			default:
				return 0, fmt.Errorf("value of /W should be array in xref stream at offset %d", addr)
			}
		case "Type", "Length", "Filter", "DecodeParms", "F", "FFilter", "FDecodeParms", "DL":
			break // not document information
		default:
			if _, ok := p.Info[key]; !ok {
				p.Info[key] = val
			}
		}
	}
	if len(index) == 0 {
		return 0, fmt.Errorf("missing both /Index and /Size in xref stream at offset %d", addr)
	}
	if len(w) == 0 {
		return 0, fmt.Errorf("missing /W in xref stream at offset %d", addr)
	}
	if err = str.Decompress(w[0] + w[1] + w[2]); err != nil {
		return 0, fmt.Errorf("decompressing xref stream at offset %d: %s", addr, err)
	}
	// If the table doesn't have enough room for all indexed objects,
	// extend it.
	if max := index[len(index)-2] + index[len(index)-1]; len(p.xref) < max {
		t := make([]any, max)
		copy(t, p.xref)
		p.xref = t
	}
	// Walk through each of the entries in the stream data and add the
	// corresponding info to the cross-reference table.
	data = str.Data
	for len(index) != 0 {
		var start, count int

		start, count, index = index[0], index[1], index[2:]
		for i := start; i < start+count; i++ {
			var (
				xtype int
				xr    any
			)
			data, xtype = getStreamElement(data, w[0], 1)
			switch xtype {
			case 0:
				var xf xrefFree
				data, xf.next = getStreamElement(data, w[1], 0)
				data, xf.gen = getStreamElement(data, w[2], 0)
				xr = xf
			case 1:
				var xd xrefDirect
				data, xd.offset = getStreamElement(data, w[1], 0)
				data, xd.gen = getStreamElement(data, w[2], 0)
				xr = xd
			case 2:
				var xs xrefStream
				data, xs.stream = getStreamElement(data, w[1], 0)
				data, xs.index = getStreamElement(data, w[2], 0)
				xr = xs
			default:
				return 0, fmt.Errorf("invalid type %d in xref stream at offset %d, index %d", xtype, addr, i)
			}
			// Don't overwrite an existing entry for this object
			// from a later (i.e., previously read) cross-ref
			// section.
			if p.xref[i] == nil {
				p.xref[i] = xr
			}
		}
	}
	if len(data) != 0 {
		return 0, fmt.Errorf("extra data left in cross-reference stream at offset %d", addr)
	}
	return
}

// getStreamElement reads one field of a cross-reference element from a stream.
// It takes the field size, and a default value if the field size is zero.  It
// returns the remaining part of the stream and the value read.
func getStreamElement(data []byte, size int, def int) (_ []byte, ret int) {
	if size == 0 {
		return data, def
	}
	for i := 0; i < size; i++ {
		ret = ret*256 + int(data[0])
		data = data[1:]
	}
	return data, ret
}
