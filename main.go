package main

/*
	epub2zip
	extract images in EPUB and save it into ZIP

	2026-04, mixcode@github
*/

import (
	"archive/zip"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

// --- XML Structures for EPUB ---

// Container represents the META-INF/container.xml file in an EPUB archive.
// It is the first file parsed to locate the root OPF file which contains the book's metadata and spine.
type Container struct {
	XMLName   xml.Name `xml:"urn:oasis:names:tc:opendocument:xmlns:container container"`
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"` // Path to the .opf file (e.g., "OEBPS/content.opf")
	} `xml:"rootfiles>rootfile"`
}

// Metadata represents the Dublin Core and custom metadata extracted from the OPF file.
// It uses slices for most fields because EPUB specifications allow multiple entries for titles, creators, etc.
type Metadata struct {
	Title      []string `xml:"http://purl.org/dc/elements/1.1/ title" json:"title,omitempty"`
	Creator    []string `xml:"http://purl.org/dc/elements/1.1/ creator" json:"creator,omitempty"`
	Language   []string `xml:"http://purl.org/dc/elements/1.1/ language" json:"language,omitempty"`
	Publisher  []string `xml:"http://purl.org/dc/elements/1.1/ publisher" json:"publisher,omitempty"`
	Identifier []string `xml:"http://purl.org/dc/elements/1.1/ identifier" json:"identifier,omitempty"`
	Date       []string `xml:"http://purl.org/dc/elements/1.1/ date" json:"date,omitempty"`
	// Meta contains custom and EPUB 3 properties like 'dcterms:modified' or 'omf:viewport'.
	Meta []struct {
		Property string `xml:"property,attr" json:"property,omitempty"` // EPUB 3 style (e.g., "rendition:layout")
		Name     string `xml:"name,attr" json:"name,omitempty"`         // EPUB 2 style (e.g., "cover")
		Content  string `xml:"content,attr" json:"content,omitempty"`   // Value for EPUB 2 style
		Value    string `xml:",chardata" json:"value,omitempty"`        // Inner text for EPUB 3 style
	} `xml:"meta" json:"meta,omitempty"`
}

// OPF represents the Open Package Format (.opf) file.
// It serves as the manifest (listing all files) and the spine (defining the linear reading order).
type OPF struct {
	XMLName  xml.Name `xml:"http://www.idpf.org/2007/opf package"`
	Metadata Metadata `xml:"metadata"`
	Manifest []Item   `xml:"manifest>item"`
	Spine    struct {
		// Direction indicates 'ltr' (Left-to-Right) or 'rtl' (Right-to-Left) progression.
		// This is critical for determining which side (Left/Right) is associated with Odd/Even page numbers.
		Direction string `xml:"page-progression-direction,attr"`
		Items     []struct {
			IDRef      string `xml:"idref,attr"`      // References an ID in the manifest
			Properties string `xml:"properties,attr"` // e.g., "page-spread-left" or "rendition:page-spread-center"
		} `xml:"itemref"`
	} `xml:"spine"`
}

// Item represents an individual resource (HTML, Image, CSS) listed in the OPF manifest.
type Item struct {
	ID        string `xml:"id,attr"`
	Href      string `xml:"href,attr"`       // Relative path to the file from the OPF file location
	MediaType string `xml:"media-type,attr"` // MIME type (e.g., "image/jpeg", "application/xhtml+xml")
}

// --- Command-line Switches Configuration ---

// Config holds the runtime configuration parsed from CLI flags.
type Config struct {
	InputPaths   []string // List of source .epub files
	OutputPath   string   // Target output directory or filename
	Padding      int      // Number of zero-padded digits for sequential filenames (default: 4)
	Verbose      bool     // If true, prints detailed runtime logs
	DryRun       bool     // If true, simulates the process without writing any files
	BlankMode    string   // "skip" (increment sequence but no file) or "generate" (create alignment placeholder)
	BlankColor   string   // Color for generated blanks: "white", "black", "transparent", or #HEX
	MetadataJSON string   // Control for metadata.json inclusion: "none", "compact", or "pretty"
	Force        bool     // Proceed even if the book is detected as reflowable
}

func main() {
	cfg := parseFlags()

	// Determine if OutputPath is a directory
	isDir := false
	if cfg.OutputPath != "" {
		info, err := os.Stat(cfg.OutputPath)
		if err == nil && info.IsDir() {
			isDir = true
		}
	}

	for _, inputPath := range cfg.InputPaths {
		targetOutput := cfg.OutputPath

		if targetOutput == "" {
			// Default: input filename but .zip in the CURRENT directory
			base := filepath.Base(inputPath)
			ext := filepath.Ext(base)
			targetOutput = strings.TrimSuffix(base, ext) + ".zip"
		} else if isDir {
			// Directory: input filename but .zip inside that directory
			base := filepath.Base(inputPath)
			ext := filepath.Ext(base)
			targetOutput = filepath.Join(cfg.OutputPath, strings.TrimSuffix(base, ext)+".zip")
		} else if len(cfg.InputPaths) > 1 {
			// Multiple files but OutputPath is not a directory
			fmt.Fprintf(os.Stderr, "Error: multiple input files provided but output '%s' is not a directory\n", cfg.OutputPath)
			os.Exit(1)
		}

		if cfg.Verbose {
			log.Printf("Processing: %s -> %s\n", inputPath, targetOutput)
		}

		if err := run(cfg, inputPath, targetOutput); err != nil {
			fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", inputPath, err)
			// Continue to next file
		}
	}
}

// parseFlags initializes and returns the configuration based on CLI arguments.
func parseFlags() *Config {
	cfg := &Config{}
	flag.StringVar(&cfg.OutputPath, "o", "", "Output zip filename or directory")
	flag.IntVar(&cfg.Padding, "p", 4, "Filename padding size")
	flag.BoolVar(&cfg.Verbose, "v", false, "Enable verbose logging")
	flag.BoolVar(&cfg.DryRun, "d", false, "Dry run: list pages without creating zip")
	flag.StringVar(&cfg.BlankMode, "b", "generate", "Blank page handling: skip or generate")
	flag.StringVar(&cfg.BlankColor, "blank-color", "transparent", "Blank page color: transparent, white, black, or #RRGGBB[AA]")
	flag.StringVar(&cfg.MetadataJSON, "m", "pretty", "Include metadata as JSON: none, compact, pretty")
	flag.BoolVar(&cfg.Force, "f", false, "Force execution even if reflowable book is detected")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <input.epub> [<input2.epub> ...]\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	// Resolve and expand input paths (for Windows wildcard support)
	for _, arg := range flag.Args() {
		if strings.ContainsAny(arg, "*?") {
			matches, err := filepath.Glob(arg)
			if err == nil && len(matches) > 0 {
				cfg.InputPaths = append(cfg.InputPaths, matches...)
				continue
			}
		}
		cfg.InputPaths = append(cfg.InputPaths, arg)
	}

	return cfg
}

// run is the entry point for the tool's core logic.
// It orchestrates the EPUB reading, page alignment, and ZIP generation phases.
func run(cfg *Config, inputPath, outputPath string) error {
	// Validate color early to prevent failing mid-process during ZIP creation.
	if _, err := parseColor(cfg.BlankColor); err != nil {
		return err
	}

	reader, err := zip.OpenReader(inputPath)
	if err != nil {
		return fmt.Errorf("failed to open epub: %w", err)
	}
	defer reader.Close()

	// 1. Find OPF
	opfPath, err := findOPF(reader)
	if err != nil {
		return err
	}
	if cfg.Verbose {
		log.Printf("Found OPF: %s\n", opfPath)
	}

	// 2. Parse OPF
	opf, err := parseOPF(reader, opfPath)
	if err != nil {
		return err
	}

	// 2.5 Check if book is fixed-layout.
	// We look for metadata like 'rendition:layout=pre-paginated' or 'fixed-layout=true'.
	if !isFixedLayout(opf.Metadata) {
		fmt.Fprintf(os.Stderr, "Warning: This EPUB appears to be reflowable (not fixed-layout).\n")
		fmt.Fprintf(os.Stderr, "Reflowable books may result in many blank pages or incomplete extraction.\n")
		if !cfg.Force {
			return fmt.Errorf("use -f or --force to proceed anyway")
		}
	}

	if cfg.Verbose {
		log.Printf("Page Progression Direction: %s\n", opf.Spine.Direction)
	}

	opfBase := filepath.Dir(opfPath)

	// 3. Map manifest for easy lookup
	manifestMap := make(map[string]Item)
	for _, item := range opf.Manifest {
		manifestMap[item.ID] = item
	}

	// 4. Extract image paths from spine.
	// We iterate through the spine (the linear reading order) and resolve either direct image references
	// or HTML wrapper files containing one or more images.
	type ImageInfo struct {
		Path   string
		Width  int
		Height int
	}
	type Page struct {
		Images  []ImageInfo
		IsBlank bool
		Spread  string // "left", "right", or "center" (used for alignment)
	}
	var pages []Page

	for i, spineItem := range opf.Spine.Items {
		item, ok := manifestMap[spineItem.IDRef]
		if !ok {
			if cfg.Verbose {
				log.Printf("Spine item %s not found in manifest\n", spineItem.IDRef)
			}
			continue
		}

		// Extract spread metadata to ensure images land on the correct physical page side.
		spread := "center"
		if strings.Contains(spineItem.Properties, "page-spread-left") || strings.Contains(spineItem.Properties, "rendition:spread-left") {
			spread = "left"
		} else if strings.Contains(spineItem.Properties, "page-spread-right") || strings.Contains(spineItem.Properties, "rendition:spread-right") {
			spread = "right"
		}

		fullPath := filepath.Join(opfBase, item.Href)
		// Standardize to forward slashes for internal ZIP path lookups.
		fullPath = filepath.ToSlash(fullPath)

		var absImgPaths []string
		if strings.HasPrefix(item.MediaType, "image/") {
			// Direct image reference in spine.
			absImgPaths = []string{fullPath}
		} else {
			// HTML wrapper: parse the DOM to find all target images.
			imgPaths, err := extractImageFromHTML(reader, fullPath)
			if err != nil {
				if cfg.Verbose {
					log.Printf("Page %d (%s): %v\n", i+1, fullPath, err)
				}
				pages = append(pages, Page{IsBlank: true, Spread: spread})
				continue
			}
			for _, ip := range imgPaths {
				// imgPath is relative to the HTML file location.
				absImgPaths = append(absImgPaths, filepath.ToSlash(filepath.Join(filepath.Dir(fullPath), ip)))
			}
		}

		var pageImages []ImageInfo
		for _, imgPath := range absImgPaths {
			// Read dimensions (needed for correctly sized blank placeholders).
			w, h, err := getImageDimensions(reader, imgPath)
			if err != nil {
				if cfg.Verbose {
					log.Printf("Failed to get dimensions for %s: %v\n", imgPath, err)
				}
			}
			pageImages = append(pageImages, ImageInfo{Path: imgPath, Width: w, Height: h})
		}

		pages = append(pages, Page{Images: pageImages, Spread: spread})
		if cfg.Verbose {
			log.Printf("Page %d: Found %d images [spread: %s]\n", i+1, len(pageImages), spread)
			for j, img := range pageImages {
				log.Printf("  Image %d: %s (%dx%d)\n", j+1, img.Path, img.Width, img.Height)
			}
		}
	}

	// 5. Calculate Final Page Numbers based on Direction and Spread.
	// This step handles the Japanese book logic where 'Odd' pages are the front side.
	// In LTR: Odd = Right side, Even = Left side.
	// In RTL: Odd = Left side, Even = Right side.
	// If a page's metadata says it's a 'right' page but the sequence is currently 'odd' in RTL,
	// we must insert a blank to shift it to an 'even' position.
	type OutputPage struct {
		SourceIdx int // index in 'pages' slice, or -1 for alignment blanks
		PageNum   int
	}
	var outputPages []OutputPage

	isRTL := opf.Spine.Direction == "rtl"

	currentPageNum := 1
	for i := range pages {
		p := &pages[i]

		needsPadding := false
		if isRTL {
			// In RTL: Odd is Left, Even is Right.
			if p.Spread == "right" && currentPageNum%2 != 0 {
				needsPadding = true // 'Right' must be Even (2, 4, 6...)
			} else if p.Spread == "left" && currentPageNum%2 == 0 {
				needsPadding = true // 'Left' must be Odd (1, 3, 5...)
			}
		} else {
			// In LTR: Odd is Right, Even is Left.
			if p.Spread == "left" && currentPageNum%2 != 0 {
				needsPadding = true // 'Left' must be Even (2, 4, 6...)
			} else if p.Spread == "right" && currentPageNum%2 == 0 {
				needsPadding = true // 'Right' must be Odd (1, 3, 5...)
			}
		}

		if needsPadding {
			// Insert a blank page for alignment if requested.
			if cfg.BlankMode == "generate" || cfg.BlankMode == "skip" {
				if cfg.Verbose {
					log.Printf("Aligning page %d (Source %d) due to %s spread\n", currentPageNum, i+1, p.Spread)
				}
				outputPages = append(outputPages, OutputPage{SourceIdx: -1, PageNum: currentPageNum})
				currentPageNum++
			}
		}

		outputPages = append(outputPages, OutputPage{SourceIdx: i, PageNum: currentPageNum})
		currentPageNum++
	}

	// 6. Handle blank pages dimensions for those with SourceIdx == -1
	// (Reuse logic from previous step 5 but adapted)

	// 7. Output Generation (DryRun or ZIP write).
	if cfg.DryRun {
		fmt.Printf("Dry run: planned output to %s (Direction: %s)\n", outputPath, opf.Spine.Direction)
		for _, op := range outputPages {
			if op.SourceIdx == -1 {
				fmt.Printf("  Page %0*d: [Alignment Blank]\n", cfg.Padding, op.PageNum)
			} else {
				p := pages[op.SourceIdx]
				if p.IsBlank {
					fmt.Printf("  Page %0*d: [Skipped Blank]\n", cfg.Padding, op.PageNum)
				} else {
					if len(p.Images) == 1 {
						fmt.Printf("  Page %0*d: %s (%s)\n", cfg.Padding, op.PageNum, p.Images[0].Path, p.Spread)
					} else {
						fmt.Printf("  Page %0*d: (%d images) (%s)\n", cfg.Padding, op.PageNum, len(p.Images), p.Spread)
						for j, img := range p.Images {
							fmt.Printf("    [%d]: %s\n", j+1, img.Path)
						}
					}
				}
			}
		}
		return nil
	}

	outF, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outF.Close()

	archive := zip.NewWriter(outF)
	defer archive.Close()

	// 6. Add metadata.json if requested.
	if cfg.MetadataJSON != "none" {
		var data []byte
		var err error
		if cfg.MetadataJSON == "pretty" {
			data, err = json.MarshalIndent(opf.Metadata, "", "  ")
		} else {
			data, err = json.Marshal(opf.Metadata)
		}
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}

		w, err := archive.Create("metadata.json")
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if cfg.Verbose {
			log.Printf("Added metadata.json (%s mode)\n", cfg.MetadataJSON)
		}
	}

	// Final ZIP loop: copy images and generate necessary blanks.
	for _, op := range outputPages {
		var p *Page
		if op.SourceIdx != -1 {
			p = &pages[op.SourceIdx]
		}

		if op.SourceIdx == -1 || p.IsBlank {
			if cfg.BlankMode != "generate" {
				continue
			}

			// Determine dimensions for blank.
			var w, h int
			if op.SourceIdx != -1 && len(p.Images) > 0 {
				w, h = p.Images[0].Width, p.Images[0].Height
			} else {
				// Search neighbors for dimension reference.
				for j := range pages {
					if !pages[j].IsBlank && len(pages[j].Images) > 0 {
						w, h = pages[j].Images[0].Width, pages[j].Images[0].Height
						break
					}
				}
				if w == 0 {
					w, h = 800, 1200
				}
			}

			name := fmt.Sprintf("%0*d_blank.png", cfg.Padding, op.PageNum)
			writer, err := archive.Create(name)
			if err != nil {
				return err
			}
			col, err := parseColor(cfg.BlankColor)
			if err != nil {
				return err
			}
			if err := generateBlankImage(writer, w, h, col); err != nil {
				return err
			}
			continue
		}

		// Real images
		for j, img := range p.Images {
			ext := strings.ToLower(filepath.Ext(img.Path))
			if ext == "" {
				ext = ".jpg"
			}

			var name string
			if len(p.Images) == 1 {
				name = fmt.Sprintf("%0*d%s", cfg.Padding, op.PageNum, ext)
			} else {
				name = fmt.Sprintf("%0*d_%d%s", cfg.Padding, op.PageNum, j+1, ext)
			}

			writer, err := archive.Create(name)
			if err != nil {
				return err
			}

			rc, err := reader.Open(img.Path)
			if err != nil {
				return err
			}
			_, err = io.Copy(writer, rc)
			rc.Close()
			if err != nil {
				return err
			}
		}
	}

	if cfg.Verbose {
		log.Printf("Created %s\n", outputPath)
	}

	return nil
}

// parseColor converts a string (named color or #HEX) into a color.Color interface.
// Supports "white", "black", "transparent", and CSS-style "#RRGGBB" or "#RRGGBBAA".
func parseColor(s string) (color.Color, error) {
	switch strings.ToLower(s) {
	case "transparent":
		return color.Transparent, nil
	case "white":
		return color.White, nil
	case "black":
		return color.Black, nil
	}

	if strings.HasPrefix(s, "#") {
		var r, g, b, a uint8
		a = 255
		if len(s) == 7 { // #RRGGBB
			_, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b)
			if err != nil {
				return nil, fmt.Errorf("invalid hex color: %s", s)
			}
			return color.RGBA{r, g, b, a}, nil
		} else if len(s) == 9 { // #RRGGBBAA
			_, err := fmt.Sscanf(s, "#%02x%02x%02x%02x", &r, &g, &b, &a)
			if err != nil {
				return nil, fmt.Errorf("invalid hex color: %s", s)
			}
			return color.RGBA{r, g, b, a}, nil
		}
	}

	return nil, fmt.Errorf("unknown color: %s", s)
}

// generateBlankImage creates a solid-color PNG image and writes it to the provided writer.
func generateBlankImage(w io.Writer, width, height int, col color.Color) error {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{col}, image.Point{}, draw.Src)
	return png.Encode(w, img)
}

// findOPF reads META-INF/container.xml to find the location of the root OPF package file.
func findOPF(r *zip.ReadCloser) (string, error) {
	f, err := r.Open("META-INF/container.xml")
	if err != nil {
		return "", fmt.Errorf("META-INF/container.xml not found")
	}
	defer f.Close()

	var c Container
	if err := xml.NewDecoder(f).Decode(&c); err != nil {
		return "", err
	}

	if len(c.Rootfiles) == 0 {
		return "", fmt.Errorf("no rootfiles in container.xml")
	}

	return c.Rootfiles[0].FullPath, nil
}

// parseOPF decodes the specified OPF XML file into a Go struct.
func parseOPF(r *zip.ReadCloser, path string) (*OPF, error) {
	f, err := r.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var opf OPF
	if err := xml.NewDecoder(f).Decode(&opf); err != nil {
		return nil, err
	}

	return &opf, nil
}

// extractImageFromHTML parses an XHTML document to find all <img> or <svg><image> elements.
// Returns a slice of relative paths found in 'src' or 'href' attributes.
func extractImageFromHTML(r *zip.ReadCloser, path string) ([]string, error) {
	f, err := r.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	doc, err := html.Parse(f)
	if err != nil {
		return nil, err
	}

	var imgPaths []string
	var fNode func(*html.Node)
	fNode = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "img" {
				for _, a := range n.Attr {
					if a.Key == "src" {
						imgPaths = append(imgPaths, a.Val)
					}
				}
			} else if n.Data == "image" { // SVG image
				for _, a := range n.Attr {
					if a.Key == "href" || a.Key == "xlink:href" {
						imgPaths = append(imgPaths, a.Val)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			fNode(c)
		}
	}
	fNode(doc)

	if len(imgPaths) == 0 {
		return nil, fmt.Errorf("no image found in %s", path)
	}
	return imgPaths, nil
}

// isFixedLayout checks the book metadata for properties indicating a fixed layout.
func isFixedLayout(m Metadata) bool {
	for _, meta := range m.Meta {
		// EPUB 3 standard property
		if meta.Property == "rendition:layout" && meta.Value == "pre-paginated" {
			return true
		}
		// Common extensions and EPUB 2 style
		if meta.Name == "fixed-layout" && meta.Content == "true" {
			return true
		}
		// Presence of a viewport usually implies fixed layout in these types of books.
		if strings.Contains(meta.Property, "viewport") {
			return true
		}
	}
	return false
}

// getImageDimensions decodes image configuration (headers) to get pixel dimensions
// without loading the entire image data into memory.
func getImageDimensions(r *zip.ReadCloser, path string) (int, int, error) {
	f, err := r.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}
