package pdfstruct

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// readObjectAt returns the Object at the specified location in the file.
func (p *PDF) readObjectAt(addr int) (obj Object, err error) {
	var (
		buf   [256]byte
		count int
		ptr   = addr
	)
	if _, _, obj, err = readObject(addr, nil, func(by []byte) (_ []byte, err error) {
		if count, err = p.fh.ReadAt(buf[:], int64(ptr)); err != nil && (err != io.EOF || count == 0) {
			return nil, err
		}
		ptr += count
		return append(by, buf[:count]...), nil
	}); err != nil {
		return nil, fmt.Errorf("reading object at offset %d: %s", addr, err)
	}
	return obj, nil
}

// readObjectFrom returns the Object from the specified byte array.
func readObjectFrom(by []byte) (obj Object, newoff int, err error) {
	_, newoff, obj, err = readObject(0, by, nil)
	return
}

type parser struct {
	by     []byte
	more   func([]byte) ([]byte, error)
	accum  []byte
	parens int
	offset int
}

type statefunc func(*parser) (statefunc, Object, error)

// readObjectAt returns the Object in the specified buffer.  The more function
// can be used to fetch more data into the buffer as needed.
func readObject(
	offset int, by []byte, more func([]byte) ([]byte, error),
) (remainder []byte, newoff int, obj Object, err error) {
	p := &parser{by: by, more: more, offset: offset}
	state := stStart
	for state != nil {
		state, obj, err = state(p)
	}
	if err != nil {
		return nil, 0, nil, err
	}
	return p.by, p.offset, obj, nil
}

func stStart(p *parser) (statefunc, Object, error) {
	if err := p.extend(1); err != nil {
		return nil, nil, err
	}
	switch p.by[0] {
	case 0, 9, 10, 12, 13, 32:
		p.skip(1)
		return stStart, nil, nil
	case '%':
		return stComment, nil, nil
	case '(':
		p.skip(1)
		p.accum, p.parens = nil, 1
		return stString, nil, nil
	case '<':
		if err := p.extend(2); err != nil {
			return nil, nil, err
		}
		if p.by[1] == '<' {
			p.skip(2)
			return stDict, nil, nil
		}
		p.skip(1)
		p.accum = nil
		return stHex, nil, nil
	case '>':
		return nil, nil, errors.New("unexpected >")
	case '/':
		p.skip(1)
		p.accum = nil
		return stName, nil, nil
	case '[':
		p.skip(1)
		return stArray, nil, nil
	case ']':
		return nil, nil, errors.New("unexpected ]")
	case '-', '+', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return stNumObjRef, nil, nil
	default:
		return stWord, nil, nil
	}
}

func stComment(p *parser) (_ statefunc, _ Object, err error) {
	for {
		idx := bytes.IndexAny(p.by, "\r\n")
		if idx >= 0 && p.by[idx] == '\n' {
			p.skip(idx + 1)
			return stStart, nil, nil
		}
		if idx >= 0 && len(p.by) > idx+1 {
			if p.by[idx+1] == '\n' {
				p.skip(idx + 2)
			} else {
				p.skip(idx + 1)
			}
			return stStart, nil, nil
		}
		if p.by, err = p.more(p.by); err != nil {
			return nil, nil, err
		}
	}
}

func stString(p *parser) (_ statefunc, _ Object, err error) {
	if err = p.extend(1); err != nil {
		return nil, nil, err
	}
	var b = p.by[0]
	p.skip(1)
	switch b {
	case '(':
		p.parens++
		p.accum = append(p.accum, b)
		return stString, nil, nil
	case ')':
		p.parens--
		if p.parens == 0 {
			return nil, string(p.accum), nil
		}
		p.accum = append(p.accum, b)
		return stString, nil, nil
	case '\\':
		return stStringEsc, nil, nil
	case '\r':
		if err = p.extend(1); err != nil {
			return nil, nil, err
		}
		if p.by[0] == '\n' {
			p.skip(1)
		}
		p.accum = append(p.accum, '\n')
		return stString, nil, nil
	default:
		p.accum = append(p.accum, b)
		return stString, nil, nil
	}
}

func stStringEsc(p *parser) (_ statefunc, _ Object, err error) {
	if err = p.extend(1); err != nil {
		return nil, nil, err
	}
	var b = p.by[0]
	p.skip(1)
	switch b {
	case 'n':
		p.accum = append(p.accum, '\n')
		return stString, nil, nil
	case 'r':
		p.accum = append(p.accum, '\r')
		return stString, nil, nil
	case 't':
		p.accum = append(p.accum, '\t')
		return stString, nil, nil
	case 'b':
		p.accum = append(p.accum, '\b')
		return stString, nil, nil
	case 'f':
		p.accum = append(p.accum, '\f')
		return stString, nil, nil
	case '\r':
		if err = p.extend(1); err != nil {
			return nil, nil, err
		}
		if p.by[0] == '\n' {
			p.skip(1)
		}
		return stString, nil, nil
	case '\n':
		return stString, nil, nil
	case '0', '1', '2', '3', '4', '5', '6', '7':
		if err = p.extend(2); err != nil {
			return nil, nil, err
		}
		if p.by[0] >= '0' && p.by[0] <= '7' {
			if p.by[1] >= '0' && p.by[1] <= '7' {
				p.accum = append(p.accum, (b-'0')*64+(p.by[0]-'0')*8+p.by[1]-'0')
				p.skip(2)
			} else {
				p.accum = append(p.accum, (b-'0')*8+p.by[0]-'0')
				p.skip(1)
			}
		} else {
			p.accum = append(p.accum, b-'0')
		}
		return stString, nil, nil
	default:
		p.accum = append(p.accum, b)
		return stString, nil, nil
	}
}

func stHex(p *parser) (_ statefunc, _ Object, err error) {
	if err = p.extend(2); err != nil {
		return nil, nil, err
	}
	if p.by[0] == '>' {
		var hex []byte
		hex, p.accum = p.accum, nil
		p.skip(1)
		return nil, hex, nil
	}
	var b byte
	switch p.by[0] {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		b = p.by[0] - '0'
	case 'A', 'B', 'C', 'D', 'E', 'F':
		b = p.by[0] - 'A' + 10
	case 'a', 'b', 'c', 'd', 'e', 'f':
		b = p.by[0] - 'a' + 10
	default:
		return nil, nil, errors.New("invalid character in hex string")
	}
	switch p.by[1] {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		b = b*16 + p.by[1] - '0'
	case 'A', 'B', 'C', 'D', 'E', 'F':
		b = b*16 + p.by[1] - 'A' + 10
	case 'a', 'b', 'c', 'd', 'e', 'f':
		b = b*16 + p.by[1] - 'a' + 10
	default:
		return nil, nil, errors.New("invalid character in hex string")
	}
	p.accum = append(p.accum, b)
	p.skip(2)
	return stHex, nil, nil
}

func stName(p *parser) (_ statefunc, _ Object, err error) {
	if err = p.extend(1); err != nil {
		return nil, nil, err
	}
	switch p.by[0] {
	case 0, 9, 10, 12, 13, 32, '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return nil, Name(p.accum), nil
	case '#':
		if err = p.extend(3); err != nil {
			return nil, nil, err
		}
		var b byte
		switch p.by[1] {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			b = p.by[1] - '0'
		case 'A', 'B', 'C', 'D', 'E', 'F':
			b = p.by[1] - 'A' + 10
		case 'a', 'b', 'c', 'd', 'e', 'f':
			b = p.by[1] - 'a' + 10
		default:
			return nil, nil, errors.New("invalid character in hex escape in /Name")
		}
		switch p.by[2] {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			b = b*16 + p.by[2] - '0'
		case 'A', 'B', 'C', 'D', 'E', 'F':
			b = b*16 + p.by[2] - 'A' + 10
		case 'a', 'b', 'c', 'd', 'e', 'f':
			b = b*16 + p.by[2] - 'a' + 10
		default:
			return nil, nil, errors.New("invalid character in hex escape in /Name")
		}
		p.accum = append(p.accum, b)
		p.skip(3)
		return stName, nil, nil
	default:
		p.accum = append(p.accum, p.by[0])
		p.skip(1)
		return stName, nil, nil
	}
}

func stWord(p *parser) (_ statefunc, _ Object, err error) {
	if err = p.extend(6); err != nil {
		return nil, nil, err
	}
	if bytes.HasPrefix(p.by, []byte("null")) && !isRegularChar(p.by[4]) {
		p.skip(4)
		return nil, nil, nil
	}
	if bytes.HasPrefix(p.by, []byte("true")) && !isRegularChar(p.by[4]) {
		p.skip(4)
		return nil, true, nil
	}
	if bytes.HasPrefix(p.by, []byte("false")) && !isRegularChar(p.by[5]) {
		p.skip(5)
		return nil, false, nil
	}
	return nil, nil, errors.New("unexpected bare word")
}

var refObjRE = regexp.MustCompile(`^([0-9]+)[ \t\r\n\f\x00]+([0-9]+)[ \t\r\n\f\x00]+(R|obj)[\x00\t\n\f\r ()<>[\]{}/%]`)
var refObjPrefixRE = regexp.MustCompile(`^[0-9]+(?:[ \t\r\n\f\x00]+(?:[0-9]+(?:[ \t\r\n\f\x00]+(?:R|o|ob|obj)?)?)?)?$`)

func stNumObjRef(p *parser) (_ statefunc, _ Object, err error) {
	// We saw a character that is the start of a number.  There are four
	// (valid) possibilities:
	//   - integer
	//   - realnum
	//   - unsignedint unsignedint R
	//   - unsignedint unsignedint obj «object» endobj
	// We'll start by checking for the last two.
	for {
		if match := refObjRE.FindSubmatch(p.by); match != nil {
			if match[3][0] == 'R' {
				return stRef(p, match)
			}
			return stNumberedObj(p, match)
		}
		// It doesn't match either of the last two, but does it match a
		// prefix of them?
		if !refObjPrefixRE.Match(p.by) {
			return stNumber, nil, nil
		}
		// Yes, it's a prefix.  Load more input into the buffer and
		// check again.
		if p.by, err = p.more(p.by); err != nil {
			return nil, nil, err
		}
	}
}

func stNumber(p *parser) (_ statefunc, _ Object, err error) {
	var idx int
	idx = bytes.IndexAny(p.by, nonRegularChars)
	for idx < 0 {
		if p.by, err = p.more(p.by); err != nil {
			return nil, nil, err
		}
		idx = bytes.IndexAny(p.by, nonRegularChars)
	}
	if num, err := strconv.Atoi(string(p.by[:idx])); err == nil {
		p.skip(idx)
		return nil, num, nil
	}
	if num, err := strconv.ParseFloat(string(p.by[:idx]), 64); err == nil {
		p.skip(idx)
		return nil, num, nil
	}
	return nil, nil, errors.New("invalid numeric constant")
}

func stRef(p *parser, match [][]byte) (_ statefunc, _ Object, err error) {
	var r Reference
	r.Number, _ = strconv.Atoi(string(match[1]))
	r.Generation, _ = strconv.Atoi(string(match[2]))
	p.skip(len(match[0]) - 1)
	return nil, r, nil
}

func stNumberedObj(p *parser, match [][]byte) (_ statefunc, _ Object, err error) {
	p.skip(len(match[0]) - 1)
	// Read the object that comes after the obj keyword.
	var obj Object
	if p.by, p.offset, obj, err = readObject(p.offset, p.by, p.more); err != nil {
		return nil, nil, err
	}
	if err = p.skipWhitespace(); err != nil {
		return nil, nil, err
	}
	// Make sure the next word is "endobj".
	if err = p.extend(7); err != nil {
		return nil, nil, err
	}
	if bytes.HasPrefix(p.by, []byte("endobj")) && !isRegularChar(p.by[6]) {
		p.skip(6)
		return nil, obj, nil
	}
	return nil, nil, errors.New(`expected "endobj" after indirect object`)
}

func stArray(p *parser) (_ statefunc, _ Object, err error) {
	var a Array
	for {
		if err = p.skipWhitespace(); err != nil {
			return nil, nil, err
		}
		// Is it the end of the array?
		if p.by[0] == ']' {
			p.skip(1)
			return nil, a, nil
		}
		// No, so read an object.
		var obj Object
		var newoff int
		if p.by, newoff, obj, err = readObject(p.offset, p.by, p.more); err == nil {
			p.offset = newoff
		} else {
			return nil, nil, fmt.Errorf("reading array value at offset %d: %s", p.offset, err)
		}
		a = append(a, obj)
	}
}

func stDict(p *parser) (_ statefunc, _ Object, err error) {
	var d = make(Dict)
	for {
		if err = p.skipWhitespace(); err != nil {
			return nil, nil, err
		}
		// Is it the end of the dictionary?
		if err = p.extend(2); err != nil {
			return nil, nil, err
		}
		if p.by[0] == '>' && p.by[1] == '>' {
			p.skip(2)
			break
		}
		// No, so read a name.
		var obj Object
		var key Name
		var newoff int
		if p.by, newoff, obj, err = readObject(p.offset, p.by, p.more); err == nil {
			switch obj := obj.(type) {
			case Name:
				key = obj
			default:
				return nil, nil, fmt.Errorf("expected /Name in dict at offset %d", p.offset)
			}
			p.offset = newoff
		} else {
			return nil, nil, fmt.Errorf("reading /Name in dict at offset %d: %s", p.offset, err)
		}
		// And then read an object.
		if p.by, newoff, obj, err = readObject(p.offset, p.by, p.more); err == nil {
			p.offset = newoff
		} else {
			return nil, nil, fmt.Errorf("reading value for /%s in dict at offset %d: %s", key, p.offset, err)
		}
		d[key] = obj
	}
	// We've read the dict.  But maybe it's actually a stream?
	if err = p.skipWhitespace(); err != nil {
		return nil, d, nil
	}
	// Is it the word "stream"?
	if err = p.extend(8); err != nil {
		return nil, d, nil
	}
	if !bytes.HasPrefix(p.by, []byte("stream")) {
		return nil, d, nil
	}
	if (p.by[6] != '\r' && p.by[6] != '\n') || (p.by[6] == '\r' && p.by[7] != '\n') {
		return nil, d, nil
	}
	if p.by[6] == '\r' {
		p.skip(8)
	} else {
		p.skip(7)
	}
	// Get the length of the stream from the dict.
	var size int
	switch i := d["Length"].(type) {
	case int:
		size = i
	default:
		return nil, nil, errors.New("invalid Length for stream")
	}
	if err = p.extend(size + 12); err != nil {
		return nil, nil, err
	}
	// Make the stream.
	var s Stream
	s.Dict = d
	s.Data = p.by[:size]
	p.skip(size)
	// Skip a possible (expected?) newline.
	if p.by[0] == '\r' && p.by[1] == '\n' {
		p.skip(2)
	} else if p.by[0] == '\r' || p.by[0] == '\n' {
		p.skip(1)
	}
	// Check for "endstream" at the end.
	if !bytes.HasPrefix(p.by, []byte("endstream")) || isRegularChar(p.by[9]) {
		return nil, nil, errors.New(`expected "endstream" at end of stream`)
	}
	p.skip(9)
	return nil, s, nil
}

func (p *parser) skipWhitespace() (err error) {
	for {
		if err = p.extend(1); err != nil {
			return err
		}
		if strings.IndexByte(" \t\r\n\f\x00", p.by[0]) < 0 {
			return nil
		}
		p.skip(1)
	}
}

func (p *parser) extend(size int) (err error) {
	for len(p.by) < size {
		if p.more == nil {
			return io.EOF
		}
		if p.by, err = p.more(p.by); err != nil {
			return err
		}
	}
	return nil
}

func (p *parser) skip(size int) {
	p.offset += size
	p.by = p.by[size:]
}

const nonRegularChars = "\x00\t\n\f\r ()<>[]{}/%"

func isRegularChar(b byte) bool {
	return strings.IndexByte(nonRegularChars, b) < 0
}
