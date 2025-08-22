# Steve’s PDF Library

This repository contains code for reading and writing PDF files, as needed for
various other projects of mine.

The top package is the library used by other programs.  The sub-packages are
mostly utility commands based on that library.  Specifically:

- `pdfdump` is a program that dumps the entire structure of a PDF file in a
  human-readable form.
- `pdfinspect` is a program that dumps individual objects from the PDF file in a
  human-readable form.

There is also a sub-package `pdfform` that knows how to read and write fillable
forms in PDF files.  I don't have any use for it anymore so it isn't being
maintained, but as far as I know it still works.

The sub-package `pdftest` is just a test program used during development of the
library.
