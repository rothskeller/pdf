package pdf

import (
	"fmt"
	"iter"
)

// PagePaths returns an iterator of paths to the page dicts in the PDF.
func (pdf *PDF) PagePaths() iter.Seq2[int, string] {
	var pagenum int

	return func(yield func(int, string) bool) {
		pdf.everyPage("/Root/Pages", func(pagepath string) bool {
			pagenum++
			return yield(pagenum, pagepath)
		})
	}
}
func (pdf *PDF) everyPage(root string, f func(string) bool) bool {
	var dict, err = pdf.PGetDict(root)
	if err != nil {
		return true
	}
	switch dict["Type"] {
	case Name("Page"):
		return f(root)
	case Name("Pages"):
		var path = PathKey(root, "Kids")
		var kids, err = pdf.PGetArray(path)
		if err != nil {
			return true
		}
		for kidPath := range ArrayPaths(path, kids) {
			if !pdf.everyPage(kidPath, f) {
				return false
			}
		}
		return true
	default:
		return true
	}
}

// Pages returns an iterator that yields (page number, cursor pointing to page
// dictionary) for all pages in the PDF in proper sequence.  Once yielded, each
// cursor is discarded, so the calling code can use it however it will.  If an
// error occurs, the final yielded cursor will be in an error state.
func (pdf *PDF) Pages() iter.Seq2[int, *Cursor] {
	return func(yield func(int, *Cursor) bool) {
		type pagesArray struct {
			pages  Array
			index  int
			cursor *Cursor
		}
		var (
			parrays []*pagesArray
			c       *Cursor
			parray  *pagesArray
			err     error
			pagenum = 1
		)
		c = pdf.Cursor().Key("Root").Key("Pages").Key("Kids")
		parray = new(pagesArray)
		if parray.pages, err = c.Array(); err != nil {
			c.SetError(err)
			yield(pagenum, c)
			return
		}
		parray.cursor = c
		parrays = append(parrays, parray)
		for len(parrays) != 0 {
			var dict Dict

			parray = parrays[len(parrays)-1]
			if parray.index >= len(parray.pages) {
				parrays = parrays[:len(parrays)-1]
				continue
			}
			c = parray.cursor.Clone().Index(parray.index)
			parray.index++
			if dict, err = c.Dict(); err != nil {
				c.SetError(err)
				yield(pagenum, c)
				return
			}
			switch dict["Type"] {
			case Name("Pages"):
				parray = new(pagesArray)
				if parray.pages, err = c.Key("Kids").Array(); err != nil {
					c.SetError(err)
					yield(pagenum, c)
					return
				}
				parray.cursor = c
				parrays = append(parrays, parray)
			case Name("Page"):
				if !yield(pagenum, c) {
					return
				}
				pagenum++
			default:
				c.SetError(fmt.Errorf("%s/Type: not /Pages or /Page", c.Path()))
				yield(pagenum, c)
				return
			}
		}
	}
}
