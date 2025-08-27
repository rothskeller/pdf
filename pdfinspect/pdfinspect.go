// pdfinspect dumps one or more objects from a PDF file.
//
//	usage: pdfinspect pdf-file path
//
// path is a slash-separated path of Dict keys or Array indexes leading to the
// object in question.  If the path starts with a /, it starts in the trailer
// dictionary.  If the path does not start with a /, it starts in the document
// catalog (i.e., it behaves as if the "current directory" is /Root).  The path
// may contain "*" wildcards replacing an entire component, in which case all
// Dict entries or Array elements at that component are listed.
package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/rothskeller/pdf"
)

func main() {
	var (
		fh     *os.File
		p      *pdf.PDF
		path   []string
		root   pdf.Dict
		prefix string
		err    error
	)
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: pdfinspect pdf-file path/to/object|object-id\n")
		os.Exit(2)
	}
	if fh, err = os.Open(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		os.Exit(1)
	}
	defer fh.Close()
	if p, err = pdf.Open(fh); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s: %s\n", os.Args[1], err)
		os.Exit(1)
	}
	if objid, err := strconv.Atoi(os.Args[2]); err == nil {
		if obj, err := p.Fetch(pdf.Reference{Number: objid}); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %d: %s\n", objid, err)
			os.Exit(1)
		} else {
			dump(p, obj, os.Args[2], 0)
			os.Exit(0)
		}
	}
	path = strings.Split(os.Args[2], "/")
	if path[0] == "" {
		path, root = path[1:], p.Trailer
	} else {
		root, prefix = p.Catalog, "/Root"
	}
	find(p, root, prefix, path)
}

func find(p *pdf.PDF, root pdf.Object, prefix string, path []string) {
	var err error

	if len(path) == 0 {
		dump(p, root, prefix, 0)
		return
	}
	if ref, ok := root.(pdf.Reference); ok {
		if root, err = p.Fetch(ref); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s: (#%d,%d): %s\n", prefix, ref.Number, ref.Generation, err)
			os.Exit(1)
		}
	}
	if str, ok := root.(pdf.Stream); ok {
		root = str.Dict
	}
	switch root := root.(type) {
	case pdf.Array:
		if path[0] == "*" {
			for i := range root {
				find(p, root[i], fmt.Sprintf("%s/%d", prefix, i), path[1:])
			}
			break
		}
		var idx int
		if idx, err = strconv.Atoi(path[0]); err != nil || idx < 0 {
			fmt.Fprintf(os.Stderr, "ERROR: %s is an Array but %q is not a valid array index\n", prefix, path[0])
			return
		}
		if idx >= len(root) {
			fmt.Fprintf(os.Stderr, "ERROR: index %d is out of bounds for %s (length %d)\n", idx, prefix, len(root))
			return
		}
		find(p, root[idx], fmt.Sprintf("%s/%d", prefix, idx), path[1:])
	case pdf.Dict:
		if path[0] == "*" {
			var keys = make([]string, 0, len(root))
			for key := range root {
				keys = append(keys, string(key))
			}
			sort.Strings(keys)
			for _, key := range keys {
				find(p, root[pdf.Name(key)], fmt.Sprintf("%s/%s", prefix, key), path[1:])
			}
			break
		}
		if obj, ok := root[pdf.Name(path[0])]; ok {
			find(p, obj, fmt.Sprintf("%s/%s", prefix, path[0]), path[1:])
		} else {
			fmt.Fprintf(os.Stderr, "ERROR: key %q does not exist in %s\n", path[0], prefix)
		}
	default:
		fmt.Fprintf(os.Stderr, "Error: %s is a %T, not a Dict, Stream, or Array\n", prefix, root)
	}
}

func dump(p *pdf.PDF, obj pdf.Object, path string, indent int) {
	if ref, ok := obj.(pdf.Reference); ok && indent == 0 {
		var err error
		if obj, err = p.Fetch(ref); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s: (#%d,%d): %s\n", path, ref.Number, ref.Generation, err)
			os.Exit(1)
		}
		fmt.Printf("%s = (#%d,%d) -> ", path, ref.Number, ref.Generation)
	} else {
		fmt.Printf("%s = ", path)
	}
	switch obj := obj.(type) {
	case nil:
		fmt.Println("null")
	case bool, int:
		fmt.Printf("%v\n", obj)
	case float64:
		fmt.Printf("%f\n", obj)
	case string:
		fmt.Printf("%q\n", obj)
	case []byte:
		fmt.Printf("<%s>\n", hex.EncodeToString(obj))
	case pdf.Name:
		fmt.Printf("/%s\n", string(obj))
	case pdf.Array:
		fmt.Println("Array[")
		for i := range obj {
			dump(p, obj[i], fmt.Sprintf("%*s[%d]", indent*4+4, "", i), indent+1)
		}
		fmt.Printf("%*s]\n", indent*4, "")
	case pdf.Dict:
		fmt.Println("Dict<<")
		dumpDict(p, obj, indent)
		fmt.Printf("%*s>>\n", indent*4, "")
	case pdf.Stream:
		fmt.Println("Stream<<")
		dumpDict(p, obj.Dict, indent)
		fmt.Printf("%*s>>\n", indent*4, "")
		obj.Decompress(0)
		spew.Dump(obj.Data)
	case pdf.Reference:
		fmt.Printf("(#%d,%d)\n", obj.Number, obj.Generation)
	default:
		panic("unknown object type")
	}
}

func dumpDict(p *pdf.PDF, d pdf.Dict, indent int) {
	var keys = make([]pdf.Name, 0, len(d))
	for key := range d {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, key := range keys {
		dump(p, d[key], fmt.Sprintf("%*s/%s", indent*4+4, "", string(key)), indent+1)
	}
}
