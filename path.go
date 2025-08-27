package pdf

import (
	"fmt"
	"iter"
	"strconv"
	"strings"
)

// PGet returns the object at the specified path.  A path is a filesystem-like
// path where "/" is the trailer dictionary and a relative path is interpreted
// related to "/Root".  Each element of the path corresponds to a key into a
// Dict or an index into an Array; in the latter case the path element must
// be an integer >= 0.  If the file structure isn't consistent with the path,
// an error is returned.  If the last element of the path doesn't exist, a nil
// object is returned with no error.
func (pdf *PDF) PGet(path string) (object Object, err error) {
	object, _, _, err = pdf.pget(path, true)
	return object, err
}
func (pdf *PDF) pget(path string, derefLast bool) (object, lastTgt Object, lastRef Reference, err error) {
	var (
		loc   Object
		epath string
		elms  []string
		last  string
	)
	if path == "" || (path != "/" && strings.HasSuffix(path, "/")) || strings.Contains(path, "//") {
		err = fmt.Errorf("%q: invalid path", path)
		return
	}
	if strings.HasPrefix(path, "/") {
		loc = pdf.Trailer
		path = path[1:]
		epath = "/"
	} else {
		loc = pdf.Catalog
		epath = "/Root"
		lastTgt, lastRef = loc, pdf.Trailer["Root"].(Reference)
	}
	if path == "" {
		return loc, lastTgt, lastRef, nil
	}
	elms = strings.Split(path, "/")
	elms, last = elms[:len(elms)-1], elms[len(elms)-1]
	for _, elm := range elms {
		switch loct := loc.(type) {
		case Dict:
			if o, ok := loct[Name(elm)]; !ok {
				err = fmt.Errorf("%s/%s does not exist", epath, elm)
				return
			} else {
				loc = o
			}
		case Stream:
			if o, ok := loct.Dict[Name(elm)]; !ok {
				err = fmt.Errorf("%s/%s does not exist", epath, elm)
				return
			} else {
				loc = o
			}
		case Array:
			if i, err2 := strconv.Atoi(elm); err2 != nil || i < 0 {
				err = fmt.Errorf("%s is an Array and %s is not a valid index", epath, elm)
				return
			} else if i >= len(loct) {
				err = fmt.Errorf("%s/%s does not exist (index out of range)", epath, elm)
				return
			} else {
				loc = loct[i]
			}
		default:
			err = fmt.Errorf("%s/%s is not an Array, Dict, or Stream", epath, elm)
			return
		}
		epath += "/" + elm
		if ref, ok := loc.(Reference); ok {
			if loc, err = pdf.Get(ref); err != nil {
				err = fmt.Errorf("%s is a dangling reference", epath)
				return
			}
			lastTgt, lastRef = loc, ref
		}
	}
	switch loct := loc.(type) {
	case Dict:
		loc = loct[Name(last)]
	case Stream:
		loc = loct.Dict[Name(last)]
	case Array:
		if i, err2 := strconv.Atoi(last); err2 != nil || i < 0 {
			err = fmt.Errorf("%s is an Array and %s is not a valid index", epath, last)
			return
		} else if i >= len(loct) {
			err = fmt.Errorf("%s/%s does not exist (index out of range)", epath, last)
			return
		} else {
			loc = loct[i]
		}
	default:
		err = fmt.Errorf("%s/%s is not an Array, Dict, or Stream", epath, last)
		return
	}
	if ref, ok := loc.(Reference); ok && derefLast {
		if loc, err = pdf.Get(ref); err != nil {
			loc = nil
		}
		lastTgt, lastRef = loc, ref
	}
	return loc, lastTgt, lastRef, nil
}

// PGetBool is like PGet but asserts that the result is a boolean.
func (pdf *PDF) PGetBool(path string) (v bool, err error) {
	if o, err := pdf.PGet(path); err != nil {
		return false, err
	} else if v, ok := o.(bool); !ok {
		return false, fmt.Errorf("%s is %T, not bool", path, o)
	} else {
		return v, nil
	}
}

// PGetInt is like PGet but asserts that the result is an int.
func (pdf *PDF) PGetInt(path string) (v int, err error) {
	if o, err := pdf.PGet(path); err != nil {
		return 0, err
	} else if v, ok := o.(int); !ok {
		return 0, fmt.Errorf("%s is %T, not int", path, o)
	} else {
		return v, nil
	}
}

// PGetReal is like PGet but asserts that the result is a float64 (or an int,
// which is converted to float64).
func (pdf *PDF) PGetReal(path string) (v float64, err error) {
	if o, err := pdf.PGet(path); err != nil {
		return 0, err
	} else if v, ok := o.(float64); ok {
		return v, nil
	} else if v, ok := o.(int); ok {
		return float64(v), nil
	} else {
		return 0, fmt.Errorf("%s is %T, not float64 or int", path, o)
	}
}

// PGetString is like PGet but asserts that the result is a string (or a []byte,
// which is converted to string).
func (pdf *PDF) PGetString(path string) (v string, err error) {
	if o, err := pdf.PGet(path); err != nil {
		return "", err
	} else if v, ok := o.(string); ok {
		return v, nil
	} else if v, ok := o.([]byte); ok {
		return string(v), nil
	} else {
		return "", fmt.Errorf("%s is %T, not string or []byte", path, o)
	}
}

// PGetName is like PGet but asserts that the result is a Name.
func (pdf *PDF) PGetName(path string) (v Name, err error) {
	if o, err := pdf.PGet(path); err != nil {
		return "", err
	} else if v, ok := o.(Name); !ok {
		return "", fmt.Errorf("%s is %T, not Name", path, o)
	} else {
		return v, nil
	}
}

// PGetArray is like PGet but asserts that the result is an Array.
func (pdf *PDF) PGetArray(path string) (v Array, err error) {
	if o, err := pdf.PGet(path); err != nil {
		return nil, err
	} else if v, ok := o.(Array); !ok {
		return nil, fmt.Errorf("%s is %T, not an Array", path, o)
	} else {
		return v, nil
	}
}

// PGetDict is like PGet but asserts that the result is a Dict.
func (pdf *PDF) PGetDict(path string) (v Dict, err error) {
	if o, err := pdf.PGet(path); err != nil {
		return nil, err
	} else if v, ok := o.(Dict); !ok {
		return nil, fmt.Errorf("%s is %T, not Dict", path, o)
	} else {
		return v, nil
	}
}

// PGetStream is like PGet but asserts that the result is a Stream.
func (pdf *PDF) PGetStream(path string) (v Stream, err error) {
	if o, err := pdf.PGet(path); err != nil {
		return v, err
	} else if v, ok := o.(Stream); !ok {
		return v, fmt.Errorf("%s is %T, not Stream", path, o)
	} else {
		return v, nil
	}
}

// PGetRef is like PGet but asserts that the result is a Reference.
func (pdf *PDF) PGetRef(path string) (v Reference, err error) {
	if o, _, _, err := pdf.pget(path, false); err != nil {
		return v, err
	} else if v, ok := o.(Reference); !ok {
		return v, fmt.Errorf("%s is %T, not Reference", path, o)
	} else {
		return v, nil
	}
}

// PSet sets the value at the specified path to the specified value.  It marks
// the nearest ancestor object as dirty.  The path is interpreted as in PGet.
func (pdf *PDF) PSet(path string, value Object) (err error) {
	var (
		lastelm string
		anchor  Object
		lasttgt Object
		lastref Reference
	)
	if path == "" || (path != "/" && strings.HasSuffix(path, "/")) || strings.Contains(path, "//") {
		return fmt.Errorf("%q: invalid path", path)
	}
	// Take the last index off of the path.
	if idx := strings.LastIndexByte(path, '/'); idx < 1 {
		return fmt.Errorf("%q: invalid path", path)
	} else {
		path, lastelm = path[:idx], path[idx+1:]
	}
	// Get the object before the last index, which I'll call the anchor.
	if anchor, lasttgt, lastref, err = pdf.pget(path, true); err != nil {
		return err
	}
	switch anchor := anchor.(type) {
	case Dict:
		anchor[Name(lastelm)] = value
	case Stream:
		anchor.Dict[Name(lastelm)] = value
	case Array:
		if i, err := strconv.Atoi(lastelm); err != nil || i < 0 {
			return fmt.Errorf("%s is an Array and %s is not a valid index", path, lastelm)
		} else if i >= len(anchor) {
			return fmt.Errorf("%s/%s does not exist (index out of range)", path, lastelm)
		} else {
			anchor[i] = value
		}
	default:
		return fmt.Errorf("%s is not an Array, Dict, or Stream", path)
	}
	pdf.UpdateObject(lastref, lasttgt)
	return nil
}

// PAppend appends the specified value to the Array at the specified path.  It
// marks the nearest ancestor object as dirty.  The path is interpreted as in
// PGet.
func (pdf *PDF) PAppend(path string, value Object) (err error) {
	var aOrR Object

	// There are two cases to handle: the array could be an "indirect
	// object" accessed through a Reference, or it could be embedded in some
	// other object.  We'll need to use pget(derefLast=false) to find out
	// which.
	if aOrR, _, _, err = pdf.pget(path, false); err != nil {
		return err
	}
	switch aOrR := aOrR.(type) {
	case Reference:
		// It's a Reference to (presumably) an Array.  Append to the
		// Array and update the Reference to point to the result.
		if o, err := pdf.Get(aOrR); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		} else if array, ok := o.(Array); !ok {
			return fmt.Errorf("%s is %T, not Array", path, o)
		} else {
			array = append(array, value)
			pdf.UpdateObject(aOrR, array)
			return nil
		}
	case Array:
		// It's an embedded Array.  Append to it, and set the path to
		// the result.
		var array = append(aOrR, value)
		return pdf.PSet(path, array)
	default:
		return fmt.Errorf("%s is %T, not Array", path, aOrR)
	}
}

// PathIndex adds an integer index to a path.
func PathIndex(path string, index int) string {
	return fmt.Sprintf("%s/%d", path, index)
}

// PathKey adds dictionary key to a path.
func PathKey(path string, key Name) string {
	return fmt.Sprintf("%s/%s", path, key)
}

// ArrayPaths returns an iterator of paths to the elements of an array.
func ArrayPaths(arrayPath string, array Array) iter.Seq[string] {
	return func(yield func(string) bool) {
		for i := range array {
			if !yield(PathIndex(arrayPath, i)) {
				return
			}
		}
	}
}

// DictPaths returns an iterator of paths to the elements of a Dict (or Stream
// Dict).
func DictPaths(dictPath string, dict Dict) iter.Seq[string] {
	return func(yield func(string) bool) {
		for k := range dict {
			if !yield(PathKey(dictPath, k)) {
				return
			}
		}
	}
}
