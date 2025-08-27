package pdf

// This file defines a Cursor, which is used to navigate and update the
// structure of a PDF.

import (
	"fmt"
	"path"
	"slices"
	"strconv"
)

// A Cursor is used to navigate the structure of a PDF.
type Cursor struct {
	p *PDF
	// The list of objects encountered while traversing the structure.
	// objs[0] is the trailer Dict.  The last object is the current cursor
	// location.
	objs []Object
	// The list of keys used to traverse the structure.  path[i] is the
	// key used to get from objs[i-1] to objs[i].  It will be nil if
	// objs[i-1] is a Reference; a Name if objs[i-1] is a Dict or Stream;
	// and an int if objs[i-1] is an Array.
	path []any
	err  error
}

// Cursor returns a new Cursor on the PDF, initially pointing at the trailer
// dictionary.
func (p *PDF) Cursor() *Cursor {
	return &Cursor{p: p, path: []any{nil}, objs: []Object{p.Trailer}}
}

// CursorForPage returns a new Cursor on the PDF, initially pointing at the page
// dictionary for the specified page number.
func (pdf *PDF) CursorForPage(pagenum int) (c *Cursor, err error) {
	c = pdf.Cursor().Key("Root").Key("Pages")
	c, _, err = cursorForPage(c, pagenum)
	return c, err
}
func cursorForPage(in *Cursor, pagenum int) (*Cursor, int, error) {
	var (
		c    *Cursor
		dict Dict
		kids Array
		kidC *Cursor
		err  error
	)
	c = in.Clone()
	if dict, err = c.Dict(); err != nil {
		return in, pagenum, err
	}
	switch dict["Type"] {
	case Name("Pages"):
		if kids, err = c.Key("Kids").Array(); err != nil {
			return in, pagenum, err
		}
		for i := range kids {
			kidC = c.Clone().Index(i)
			if _, err = kidC.Dict(); err != nil {
				return in, pagenum, err
			}
			if kidC, pagenum, err = cursorForPage(kidC, pagenum); err == nil {
				return kidC, 0, nil
			} else if err != ErrNoSuchPage {
				return in, pagenum, err
			}
		}
		return in, pagenum, ErrNoSuchPage
	case Name("Page"):
		if pagenum == 1 {
			return c, 0, nil
		} else {
			return in, pagenum - 1, ErrNoSuchPage
		}
	default:
		return in, pagenum, fmt.Errorf("%s does not have type /Pages or /Page", c.Path())
	}
}

// Path returns a string describing the path to the current cursor location
// (used in error messages).
func (c *Cursor) Path() string {
	var p string

	for _, item := range c.path {
		switch item := item.(type) {
		case Name:
			p += "/" + string(item)
		case int:
			p += "[" + strconv.Itoa(item) + "]"
		case nil:
			p += "/"
		}
	}
	return path.Clean(p)
}

// Object returns the Object at the current cursor location, or any accumulated
// error.
func (c *Cursor) Object() (Object, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.objs[len(c.objs)-1], nil
}

// ObjectDeref returns the Object at the current cursor location, or any
// accumulated error.  However, if the Object at the current cursor location is
// a Reference, it is dereferenced first (which moves the cursor).
func (c *Cursor) ObjectDeref() (Object, error) {
	c.deref()
	return c.Object()
}

// Bool asserts that the current cursor location is a boolean, or a Reference to
// a boolean, and returns the boolean value.
func (c *Cursor) Bool() (bool, error) {
	if o, err := c.ObjectDeref(); err != nil {
		return false, err
	} else if v, ok := o.(bool); !ok {
		return false, fmt.Errorf("%s is %T, not bool", c.Path(), v)
	} else {
		return v, nil
	}
}

// Int asserts that the current cursor location is an integer, or a Reference to
// an integer, and returns the integer value.
func (c *Cursor) Int() (int, error) {
	if o, err := c.ObjectDeref(); err != nil {
		return 0, err
	} else if v, ok := o.(int); !ok {
		return 0, fmt.Errorf("%s is %T, not int", c.Path(), v)
	} else {
		return v, nil
	}
}

// Float64 asserts that the current cursor location is a real number, or a
// Reference to a real number, and returns the real number value.
func (c *Cursor) Float64() (float64, error) {
	if o, err := c.ObjectDeref(); err != nil {
		return 0, err
	} else if v, ok := o.(float64); !ok {
		return 0, fmt.Errorf("%s is %T, not float64", c.Path(), v)
	} else {
		return v, nil
	}
}

// String asserts that the current cursor location is a string, or a Reference
// to a string, and returns the string value.
func (c *Cursor) String() (string, error) {
	if o, err := c.ObjectDeref(); err != nil {
		return "", err
	} else if v, ok := o.(string); !ok {
		return "", fmt.Errorf("%s is %T, not string", c.Path(), v)
	} else {
		return v, nil
	}
}

// HexString asserts that the current cursor location is a hex string, or a
// Reference to a hex string, and returns the hex string value.
func (c *Cursor) HexString() ([]byte, error) {
	if o, err := c.ObjectDeref(); err != nil {
		return nil, err
	} else if v, ok := o.([]byte); !ok {
		return nil, fmt.Errorf("%s is %T, not []byte", c.Path(), v)
	} else {
		return v, nil
	}
}

// Name asserts that the current cursor location is a Name, or a Reference to a
// Name, and returns the Name value.
func (c *Cursor) Name() (Name, error) {
	if o, err := c.ObjectDeref(); err != nil {
		return "", err
	} else if v, ok := o.(Name); !ok {
		return "", fmt.Errorf("%s is %T, not Name", c.Path(), v)
	} else {
		return v, nil
	}
}

// Array asserts that the current cursor location is an Array, or a Reference to
// an Array, and returns the Array value.
func (c *Cursor) Array() (Array, error) {
	if o, err := c.ObjectDeref(); err != nil {
		return nil, err
	} else if v, ok := o.(Array); !ok {
		return nil, fmt.Errorf("%s is %T, not Array", c.Path(), v)
	} else {
		return v, nil
	}
}

// Dict asserts that the current cursor location is a Dict, or a Reference to a
// Dict, and returns the Dict value.
func (c *Cursor) Dict() (Dict, error) {
	if o, err := c.ObjectDeref(); err != nil {
		return nil, err
	} else if v, ok := o.(Dict); !ok {
		return nil, fmt.Errorf("%s is %T, not Dict", c.Path(), v)
	} else {
		return v, nil
	}
}

// Stream asserts that the current cursor location is a Stream, or a Reference
// to a Stream, and returns the Stream value.
func (c *Cursor) Stream() (Stream, error) {
	if o, err := c.ObjectDeref(); err != nil {
		return Stream{}, err
	} else if v, ok := o.(Stream); !ok {
		return Stream{}, fmt.Errorf("%s is %T, not Stream", c.Path(), v)
	} else {
		return v, nil
	}
}

// Reference asserts that the current cursor location is a Reference and returns
// the Reference value.
func (c *Cursor) Reference() (Reference, error) {
	if o, err := c.Object(); err != nil {
		return Reference{}, err
	} else if v, ok := o.(Reference); !ok {
		return Reference{}, fmt.Errorf("%s is %T, not Reference", c.Path(), v)
	} else {
		return v, nil
	}
}

// deref dereferences the current cursor location if it is a Reference.  It is a
// no-op otherwise.
func (c *Cursor) deref() {
	if c.err != nil {
		return
	}
	if r, ok := c.objs[len(c.objs)-1].(Reference); ok {
		if o, err := c.p.Get(r); err != nil {
			c.err = fmt.Errorf("deref %s: %w", c.Path(), err)
		} else {
			c.path = append(c.path, nil)
			c.objs = append(c.objs, o)
		}
	}
}

// Deref asserts that the current cursor location is a Reference and
// dereferences it.  An error "ruins" the cursor.
func (c *Cursor) Deref() *Cursor {
	if r, err := c.Reference(); err == nil {
		if o, err := c.p.Get(r); err != nil {
			c.err = fmt.Errorf("deref %s: %w", c.Path(), err)
		} else {
			c.path = append(c.path, nil)
			c.objs = append(c.objs, o)
		}
	}
	return c
}

// Index asserts that the current cursor location is an Array (or Reference to
// Array) and moves the cursor to the idx'th element of the Array.  An error
// "ruins" the cursor.
func (c *Cursor) Index(idx int) *Cursor {
	c.deref()
	if a, err := c.Array(); err == nil {
		if idx < 0 || idx >= len(a) {
			c.err = fmt.Errorf("index %s[%d]: index out of range (len %d)", c.Path(), idx, len(a))
		} else {
			c.path = append(c.path, idx)
			c.objs = append(c.objs, a[idx])
		}
	}
	return c
}

// Key asserts that the current cursor location is a Dict or Stream (or
// Reference to Dict or Stream) and moves the cursor to the named element of the
// Dict or the Stream's dictionary.  An error "ruins" the cursor.
func (c *Cursor) Key(name Name) *Cursor {
	c.deref()
	if c.err == nil {
		switch o := c.objs[len(c.objs)-1].(type) {
		case Dict:
			if k, ok := o[name]; !ok {
				c.err = fmt.Errorf("/%s does not exist in %s", name, c.Path())
			} else {
				c.path = append(c.path, name)
				c.objs = append(c.objs, k)
			}
		case Stream:
			if k, ok := o.Dict[name]; !ok {
				c.err = fmt.Errorf("/%s does not exist in %s", name, c.Path())
			} else {
				c.path = append(c.path, name)
				c.objs = append(c.objs, k)
			}
		default:
			c.err = fmt.Errorf("%s is %T, not Dict or Stream", c.Path(), o)
		}
	}
	return c
}

// Clone returns a copy of the cursor.
func (c *Cursor) Clone() *Cursor {
	return &Cursor{
		p:    c.p,
		objs: slices.Clone(c.objs),
		path: slices.Clone(c.path),
		err:  c.err,
	}
}

// Pop pops the current cursor location, undoing the last call to Deref, Index,
// or Key.  An error "ruins" the cursor.
func (c *Cursor) Pop() *Cursor {
	if c.err == nil {
		if len(c.path) == 1 {
			c.err = fmt.Errorf("cannot Pop past the trailer dictionary")
		} else {
			c.path = c.path[:len(c.path)-1]
			c.objs = c.objs[:len(c.objs)-1]
		}
	}
	return c
}

// Error returns any accumulated error on the cursor.
func (c *Cursor) Error() error { return c.err }

// SetError allows external code to put a Cursor into an error state.
func (c *Cursor) SetError(err error) {
	if c.err == nil {
		c.err = err
	}
}

// SetObject sets the object at the current cursor location.  This should be
// called even if the object is accessed by pointer and the pointer hasn't
// changed (e.g., updating a Dict) , as it will mark the object as dirty and
// needing to be written to the file.
func (c *Cursor) SetObject(obj Object) (err error) {
	if c.err != nil {
		return c.err
	}
	for idx := len(c.objs) - 1; idx > 0; idx-- {
		c.objs[idx] = obj
		switch pred := c.objs[idx-1].(type) {
		case Reference:
			c.p.UpdateObject(pred, obj)
			return nil
		case Dict:
			pred[c.path[idx].(Name)] = obj
			obj = pred
		case Stream:
			pred.Dict[c.path[idx].(Name)] = obj
			obj = pred
		case Array:
			pred[c.path[idx].(int)] = obj
			obj = pred
		default:
			panic("unknown type in object path")
		}
	}
	c.err = fmt.Errorf("can't change the trailer dictionary object")
	return c.err
}

// Append asserts that the current cursor location is an Array (or Reference
// to Array) and appends the supplied object to it, leaving the cursor on the
// appended object.
func (c *Cursor) Append(obj Object) (err error) {
	if ary, err := c.Array(); err != nil {
		return err
	} else {
		idx := len(ary)
		c.SetObject(append(ary, nil))
		c.Index(idx)
		return c.SetObject(obj)
	}
}

// SetKey asserts that the current cursor location is a Dict or Stream (or
// Reference to Dict or Stream) and sets the value of the named element of the
// Dict or Stream's dictionary to obj.  The name does not already have to exist
// in the dictionary.  It leaves the cursor on obj.
func (c *Cursor) SetKey(name Name, obj Object) (err error) {
	c.deref()
	if c.err != nil {
		return c.err
	}
	switch o := c.objs[len(c.objs)-1].(type) {
	case Dict:
		if _, ok := o[name]; !ok {
			o[name] = nil
		}
		c.Key(name)
		return c.SetObject(obj)
	case Stream:
		if _, ok := o.Dict[name]; !ok {
			o.Dict[name] = nil
		}
		c.Key(name)
		return c.SetObject(obj)
	default:
		c.err = fmt.Errorf("%s is %T, not Array", c.Path(), o)
		return c.err
	}
}
