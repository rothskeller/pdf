package main

import (
	"cmp"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"

	"github.com/davecgh/go-spew/spew"
	"github.com/rothskeller/pdf/v2"
)

func main() {
	var (
		fh      *os.File
		p       *pdf.PDF
		refs    map[pdf.Reference]struct{}
		reflist []pdf.Reference
		err     error
	)
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: pdfdump pdf-file")
		os.Exit(2)
	}
	if fh, err = os.Open(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s: %s\n", os.Args[1], err)
		os.Exit(1)
	}
	if p, err = pdf.Open(fh); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s: %s\n", os.Args[1], err)
		os.Exit(1)
	}
	refs = make(map[pdf.Reference]struct{})
	if err = getAllRefs(p, refs, p.Trailer); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s: %s\n", os.Args[1], err)
		os.Exit(1)
	}
	reflist = slices.Collect(maps.Keys(refs))
	slices.SortFunc(reflist, func(a, b pdf.Reference) int {
		return cmp.Compare(a.Number, b.Number)

	})
	fmt.Printf("Trailer -> ")
	dump(p, p.Trailer, 0)
	for _, ref := range reflist {
		var obj pdf.Object

		if obj, err = p.Fetch(ref); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s: %s\n", os.Args[1], err)
			os.Exit(1)
		}
		fmt.Printf("\n(#%d,%d) -> ", ref.Number, ref.Generation)
		dump(p, obj, 0)
	}
}

func getAllRefs(p *pdf.PDF, refs map[pdf.Reference]struct{}, obj pdf.Object) (err error) {
	switch obj := obj.(type) {
	case pdf.Reference:
		if _, ok := refs[obj]; ok {
			return nil
		}
		refs[obj] = struct{}{}
		var reffed pdf.Object
		if reffed, err = p.Fetch(obj); err != nil {
			return err
		}
		return getAllRefs(p, refs, reffed)
	case pdf.Dict:
		for _, v := range obj {
			if err = getAllRefs(p, refs, v); err != nil {
				return err
			}
		}
		return nil
	case pdf.Array:
		for _, v := range obj {
			if err = getAllRefs(p, refs, v); err != nil {
				return err
			}
		}
		return nil
	case pdf.Stream:
		for _, v := range obj.Dict {
			if err = getAllRefs(p, refs, v); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}

func dump(p *pdf.PDF, obj pdf.Object, indent int) {
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
			fmt.Printf("%*s[%d] ", indent*4+4, "", i)
			dump(p, obj[i], indent+1)
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
		fmt.Printf("%*s/%s: ", indent*4+4, "", string(key))
		dump(p, d[key], indent+1)
	}
}
