# Steve’s PDF Libraries

This repository contains code for reading and writing PDF files, as needed for
various other projects of mine.

Package `pdfstruct` is the base package.  It contains methods for opening an
existing PDF file, traversing its structure, and making updates to it.

Package `pdfform` is a layer on top of `pdfstruct` that particularly knows how
to deal with interactive forms in PDF files.  It can fetch the form fields and
their values, and update them.

Package `pdfinspect` is a command line tool to inspect the contents of a PDF
file.

## Legal Text

This software was written by Steve Roth.

Copyright © 2023 by Steven Roth <steve@rothskeller.net>

Permission to use, copy, modify, and/or distribute this software for any purpose
with or without fee is hereby granted.

DISCLAIMER: THIS SOFTWARE IS WITHOUT WARRANTY.
