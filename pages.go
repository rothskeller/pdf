package pdf

import (
	"fmt"
	"iter"
)

// NumPages returns the number of pages in the PDF.
func (pdf *PDF) NumPages() (count int, err error) {
	_, _ = pdf.PagePath(1) // make sure page paths are cached
	return len(pdf.pages), nil
}

// AddPage adds a page to the PDF, with the specified dimensions.
func (pdf *PDF) AddPage(mediaBox Rectangle) (err error) {
	var (
		page    Dict
		pageref Reference
	)
	_, _ = pdf.PagePath(1) // make sure page paths are cached
	page = Dict{
		"Type":   Name("Page"),
		"Parent": pdf.Catalog["Pages"],
		"Resources": Dict{"ProcSet": Array{
			Name("PDF"),
			Name("Text"),
			Name("ImageB"),
			Name("ImageC"),
			Name("ImageI"),
		}},
		"MediaBox": mediaBox.toArray(),
	}
	pageref = pdf.CreateObject(page)
	if err = pdf.Append("/Root/Pages/Kids", pageref); err != nil {
		return err
	}
	if arr, err := pdf.GetArray("/Root/Pages/Kids"); err != nil {
		return err
	} else {
		pdf.pages = append(pdf.pages, Path(fmt.Sprintf("/Root/Pages/Kids/%d", len(arr)-1)))
	}
	if err = pdf.Set("/Root/Pages/Count", len(pdf.pages)); err != nil {
		return err
	}
	return nil
}

// PagePath returns the path to the page dictionary for the specified page
// number (starting from 1).
func (pdf *PDF) PagePath(pagenum int) (p Path, err error) {
	if pdf.pages == nil {
		for _, p := range pdf.pagePaths() {
			pdf.pages = append(pdf.pages, p)
		}
	}
	if pagenum < 1 || pagenum > len(pdf.pages) {
		return "", fmt.Errorf("no such page %d", pagenum)
	}
	return pdf.pages[pagenum-1], nil
}

// PagePaths returns an iterator of paths to the page dicts in the PDF.
func (pdf *PDF) pagePaths() iter.Seq2[int, Path] {
	var pagenum int

	return func(yield func(int, Path) bool) {
		pdf.everyPage("/Root/Pages", func(pagepath Path) bool {
			pagenum++
			return yield(pagenum, pagepath)
		})
	}
}
func (pdf *PDF) everyPage(root Path, f func(Path) bool) bool {
	var dict, err = pdf.GetDict(root)
	if err != nil {
		return true
	}
	switch dict["Type"] {
	case Name("Page"):
		return f(root)
	case Name("Pages"):
		var path = root.K("Kids")
		var kids, err = pdf.GetArray(path)
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
