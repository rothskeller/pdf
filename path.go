package pdf

import (
	"encoding/hex"
	"fmt"
	"iter"
	"regexp"
	"strconv"
	"strings"
)

// A Path represents a path to an object in a PDF, navigating from the PDF's
// trailer dictionary.  It looks like a file system path, with "/" representing
// the trailer dictionary and subsequent components of the path being either
// Dict keys, Stream.Dict keys, or Array indices.  So, for example,
// /Root/Pages/Kids/0/Content might be the path to the content stream of the
// first page of the PDF.  Indirect object references are not represented in the
// path.
type Path string

// K adds a Dict or Stream.Dict key to the path.
func (p Path) K(n Name) Path { return p + Path(EncodeName(n)) }

// I adds an Array index to the path.
func (p Path) I(i int) Path { return p + "/" + Path(strconv.Itoa(i)) }

// Get returns the object at the specified Path.  If the file structure isn't
// consistent with the path, the object returned is an error.
func (pdf *PDF) Get(p Path) (object Object) {
	if object, _, _, err := pdf.pget(p, true); err != nil {
		return err
	} else {
		return object
	}
}

// pget returns the object at the specified Path.  If that object is a
// Reference, derefLast indicates whether to return the Reference itself (false)
// or the object addressed by that Reference (true).  The returned lastRef and
// lastTgt indicate the last Reference followed while traversing the path and
// the object that it addressed.
func (pdf *PDF) pget(p Path, derefLast bool) (object, lastTgt Object, lastRef Reference, err error) {
	var (
		epath string   // the path traversed so far (for error messages)
		elms  []string // the elements of the path, except the last
		last  string   // the last element of the path
	)
	// Handle special cases.
	if p == "/" {
		return pdf.Catalog, pdf.Catalog, pdf.Trailer["Root"].(Reference), nil
	}
	if !strings.HasPrefix(string(p), "/") || strings.HasSuffix(string(p), "/") || strings.Contains(string(p), "//") {
		err = fmt.Errorf("%q: invalid path", p)
		return
	}
	// Start at the trailer dictionary.
	object = pdf.Trailer
	epath = "/"
	// Split the path up into elements, keeping the last one separate.
	elms = strings.Split(string(p[1:]), "/")
	elms, last = elms[:len(elms)-1], elms[len(elms)-1]
	// Walk through the non-last elements, one at a time.
	for _, elm := range elms {
		switch objectt := object.(type) {
		case Dict:
			// If the current object is a Dict, the current path
			// element must be a Name defined in that Dict.
			if o, ok := objectt[decodeName(elm)]; !ok {
				err = fmt.Errorf("%s/%s does not exist", epath, elm)
				return
			} else {
				object = o
			}
		case Stream:
			// If the current object is a Stream, the current path
			// element must be a Name defined in the Stream's Dict.
			if o, ok := objectt.Dict[decodeName(elm)]; !ok {
				err = fmt.Errorf("%s/%s does not exist", epath, elm)
				return
			} else {
				object = o
			}
		case Array:
			// If the current object is an Array, the current path
			// element must be an integer within the Array bounds.
			if i, err2 := strconv.Atoi(elm); err2 != nil || i < 0 {
				err = fmt.Errorf("%s is an Array and %s is not a valid index", epath, elm)
				return
			} else if i >= len(objectt) {
				err = fmt.Errorf("%s/%s does not exist (index out of range)", epath, elm)
				return
			} else {
				object = objectt[i]
			}
		default:
			err = fmt.Errorf("%s/%s is not an Array, Dict, or Stream", epath, elm)
			return
		}
		// The current path element was valid and has been traversed.
		// Add it to the error path.
		epath += "/" + elm
		// The current object, that we just moved to, might be a
		// Reference.  If so, dereference it, and note it as the last
		// Reference followed.
		if ref, ok := object.(Reference); ok {
			if object, err = pdf.Fetch(ref); err != nil {
				err = fmt.Errorf("%s is a dangling reference", epath)
				return
			}
			lastTgt, lastRef = object, ref
		}
	}
	// Now handle the last element in the path.
	switch objectt := object.(type) {
	case Dict:
		// If the current object is a Dict, the last path element is
		// treated as a Name in that Dict.  If the Name is not defined
		// in that Dict, we get a nil.
		object = objectt[decodeName(last)]
	case Stream:
		// If the current object is a Stream, the last path element is
		// treated as a Name in that Stream's Dict.  If the Name is not
		// defined in that Dict, we get a nil.
		object = objectt.Dict[decodeName(last)]
	case Array:
		// If the current object is an Array, the last path element must
		// be an integer within the Array bounds.
		if i, err2 := strconv.Atoi(last); err2 != nil || i < 0 {
			err = fmt.Errorf("%s is an Array and %s is not a valid index", epath, last)
			return
		} else if i >= len(objectt) {
			err = fmt.Errorf("%s/%s does not exist (index out of range)", epath, last)
			return
		} else {
			object = objectt[i]
		}
	default:
		err = fmt.Errorf("%s/%s is not an Array, Dict, or Stream", epath, last)
		return
	}
	// The final object, that we just moved to, might be a Reference.  If
	// so, and if derefLast is set, dereference it, and note it as the last
	// Reference followed.
	if ref, ok := object.(Reference); ok && derefLast {
		if object, err = pdf.Fetch(ref); err != nil {
			object = nil
		}
		lastTgt, lastRef = object, ref
	}
	return object, lastTgt, lastRef, nil
}

// GetBool is like Get but asserts that the result is a boolean.
func (pdf *PDF) GetBool(p Path) (v bool, err error) {
	switch o := pdf.Get(p).(type) {
	case error:
		return false, o
	case bool:
		return o, nil
	default:
		return false, fmt.Errorf("%s is %T, not bool", p, o)
	}
}

// GetInt is like Get but asserts that the result is an int.
func (pdf *PDF) GetInt(p Path) (v int, err error) {
	switch o := pdf.Get(p).(type) {
	case error:
		return 0, o
	case int:
		return o, nil
	default:
		return 0, fmt.Errorf("%s is %T, not int", p, o)
	}
}

// GetReal is like Get but asserts that the result is a float64 (or an int,
// which is converted to float64).
func (pdf *PDF) GetReal(p Path) (v float64, err error) {
	switch o := pdf.Get(p).(type) {
	case error:
		return 0, o
	case float64:
		return o, nil
	case int:
		return float64(o), nil
	default:
		return 0, fmt.Errorf("%s is %T, not float64 or int", p, o)
	}
}

// GetString is like Get but asserts that the result is a string (or a []byte,
// which is converted to string).
func (pdf *PDF) GetString(p Path) (v string, err error) {
	switch o := pdf.Get(p).(type) {
	case error:
		return "", o
	case string:
		return o, nil
	case []byte:
		return string(o), nil
	default:
		return "", fmt.Errorf("%s is %T, not string or []byte", p, o)
	}
}

// GetName is like Get but asserts that the result is a Name.
func (pdf *PDF) GetName(p Path) (v Name, err error) {
	switch o := pdf.Get(p).(type) {
	case error:
		return "", o
	case Name:
		return o, nil
	default:
		return "", fmt.Errorf("%s is %T, not Name", p, o)
	}
}

// GetArray is like Get but asserts that the result is an Array.
func (pdf *PDF) GetArray(p Path) (v Array, err error) {
	switch o := pdf.Get(p).(type) {
	case error:
		return nil, o
	case Array:
		return o, nil
	default:
		return nil, fmt.Errorf("%s is %T, not Array", p, o)
	}
}

// GetRectangle is like Get but asserts that the result is a Rectangle (i.e.,
// an Array of four numbers).
func (pdf *PDF) GetRectangle(p Path) (v Rectangle, err error) {
	switch o := pdf.Get(p).(type) {
	case error:
		return v, o
	case Array:
		if len(o) != 4 {
			return v, fmt.Errorf("%s has length %d, not 4 for Rectangle", p, len(o))
		}
		if v.LLX, err = pdf.GetReal(p.I(0)); err != nil {
			return v, err
		}
		if v.LLY, err = pdf.GetReal(p.I(1)); err != nil {
			return v, err
		}
		if v.URX, err = pdf.GetReal(p.I(2)); err != nil {
			return v, err
		}
		if v.URY, err = pdf.GetReal(p.I(3)); err != nil {
			return v, err
		}
		return v, nil
	default:
		return v, fmt.Errorf("%s is %T, not Array", p, o)
	}
}

// GetDict is like Get but asserts that the result is a Dict.
func (pdf *PDF) GetDict(p Path) (v Dict, err error) {
	switch o := pdf.Get(p).(type) {
	case error:
		return nil, o
	case Dict:
		return o, nil
	default:
		return nil, fmt.Errorf("%s is %T, not Dict", p, o)
	}
}

// GetStream is like Get but asserts that the result is a Stream.
func (pdf *PDF) GetStream(p Path) (v Stream, err error) {
	switch o := pdf.Get(p).(type) {
	case error:
		return v, o
	case Stream:
		return o, nil
	default:
		return v, fmt.Errorf("%s is %T, not Stream", p, o)
	}
}

// GetReference is like Get but asserts that the result is a Reference.
func (pdf *PDF) GetReference(p Path) (v Reference, err error) {
	if o, _, _, err := pdf.pget(p, false); err != nil {
		return v, err
	} else if v, ok := o.(Reference); !ok {
		return v, fmt.Errorf("%s is %T, not Reference", p, o)
	} else {
		return v, nil
	}
}

// Set sets the value at the specified path to the specified value.  It marks
// the nearest ancestor object as dirty.
func (pdf *PDF) Set(p Path, value Object) (err error) {
	var (
		lastelm string
		anchor  Object
		lasttgt Object
		lastref Reference
	)
	// Validate the path.
	if p == "/" || !strings.HasPrefix(string(p), "/") || strings.HasSuffix(string(p), "/") || strings.Contains(string(p), "//") {
		err = fmt.Errorf("%q: invalid path", p)
		return
	}
	// Take the last element off of the path.
	if idx := strings.LastIndexByte(string(p), '/'); idx < 1 {
		return fmt.Errorf("%q: invalid path", p)
	} else {
		p, lastelm = p[:idx], string(p[idx+1:])
	}
	// Fetch the object before the last index, which I'll call the anchor.
	if anchor, lasttgt, lastref, err = pdf.pget(p, true); err != nil {
		return err
	}
	switch anchor := anchor.(type) {
	case Dict:
		// The anchor is a Dict.  If the last element is a Name already
		// in that Dict and its value is a Reference, we set the value
		// of the object addressed by that Reference.
		if v, ok := anchor[Name(lastelm)].(Reference); ok {
			pdf.UpdateObject(v, value)
			return nil
		}
		// Otherwise we change the value of that Name in the Dict.
		anchor[Name(lastelm)] = value
	case Stream:
		// The anchor is a Stream.  If the last element is a Name
		// already in that Stream's Dict and its value is a Reference,
		// we set the value of the object addressed by that Reference.
		if v, ok := anchor.Dict[Name(lastelm)].(Reference); ok {
			pdf.UpdateObject(v, value)
			return nil
		}
		// Otherwise we change the value of that Name in the Stream's
		// Dict.
		anchor.Dict[Name(lastelm)] = value
	case Array:
		// The anchor is an Array.  The last element must be an integer
		// index within the array bounds.
		if i, err := strconv.Atoi(lastelm); err != nil || i < 0 {
			return fmt.Errorf("%s is an Array and %s is not a valid index", p, lastelm)
		} else if i >= len(anchor) {
			return fmt.Errorf("%s/%s does not exist (index out of range)", p, lastelm)
		} else if v, ok := anchor[i].(Reference); ok {
			// The indexed element of the Array is a Reference.  Set
			// the value of the object addressed by that Reference.
			pdf.UpdateObject(v, value)
			return nil
		} else {
			// The indexed element of the Array is anything else.
			// Replace it with the new value.
			anchor[i] = value
		}
	default:
		return fmt.Errorf("%s is not an Array, Dict, or Stream", p)
	}
	// Mark the last indirect object we saw in the traversal as being dirty
	// and needing to be rewritten.
	pdf.UpdateObject(lastref, lasttgt)
	return nil
}

// Append appends the specified value to the Array at the specified path.  It
// marks the nearest ancestor object as dirty.
func (pdf *PDF) Append(p Path, value Object) (err error) {
	if array, err := pdf.GetArray(p); err != nil {
		return err
	} else {
		array = append(array, value)
		return pdf.Set(p, array)
	}
}

// ArrayPaths returns an iterator of paths to the elements of an array.
func ArrayPaths(arrayPath Path, array Array) iter.Seq[Path] {
	return func(yield func(Path) bool) {
		for i := range array {
			if !yield(arrayPath.I(i)) {
				return
			}
		}
	}
}

// DictPaths returns an iterator of paths to the elements of a Dict (or Stream
// Dict).
func DictPaths(dictPath Path, dict Dict) iter.Seq[Path] {
	return func(yield func(Path) bool) {
		for k := range dict {
			if !yield(dictPath.K(k)) {
				return
			}
		}
	}
}

var nameDecodeRE = regexp.MustCompile(`#[0-9A-Fa-f][0-9A-Fa-f]`)

func decodeName(s string) Name {
	return Name(nameDecodeRE.ReplaceAllStringFunc(s, func(c string) string {
		by, _ := hex.DecodeString(s[1:])
		return string(by[0])
	}))
}
