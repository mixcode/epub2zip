# TODO: epub2zip

## Overview
A robust Go CLI utility to extract sequential images from fixed-layout EPUBs (manga, comics, illustrations) into standard ZIP archives.

## Completed Features
- [x] **EPUB Parsing**: Robust extraction of OPF manifest and spine reading order.
- [x] **Fixed-Layout Detection**: Metadata-based detection (rendition:layout, fixed-layout, viewport) with reflowable book warnings.
- [x] **Japanese Page Alignment**: Support for `rtl` and `ltr` directions. Automatic insertion of alignment blanks based on spread metadata (`page-spread-left/right`).
- [x] **Multi-Image Pages**: Extraction of multiple images per HTML page with `[PAGENO]_[INDEX]` naming.
- [x] **Batch Processing**: Support for multiple input files and internal glob/wildcard expansion (Windows compatibility).
- [x] **Metadata Export**: Inclusion of `metadata.json` (compact or pretty) in the output ZIP.
- [x] **Blank Page Generation**: Smart dimension calculation (neighboring page area comparison) for alignment blanks.
- [x] **Flexible Colors**: Support for named colors and CSS-style HEX codes for generated blanks.

## Future Considerations & Potential Improvements
- [ ] **Image Optimization**: Add a flag to optionally resize or re-compress images (e.g., convert PNG to JPEG or WebP) during extraction to reduce ZIP size.
- [ ] **OCR Integration**: Optional OCR pass on extracted images to generate a companion text file or search index.
- [ ] **Enhanced Reflow Handling**: Better heuristics for "mostly image" reflowable books where text is minimal (e.g., art books with short captions).
- [ ] **ZIP Comment Support**: Store book metadata in the ZIP archive comment field for better compatibility with some comic readers.
- [ ] **Parallel Processing**: Use Go goroutines to process multiple EPUB files in parallel for significantly faster batch conversions.
- [ ] **Progress Bar**: Implement a CLI progress bar for large books or batch operations.
- [ ] **Archive Verification**: Add a post-process check to ensure the generated ZIP is valid and contains the expected number of files.

## Misc
* EPUB 3.3 Specification: https://www.w3.org/TR/epub-33/
* Sample files located in `/example_epub`
