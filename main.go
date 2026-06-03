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
type Container struct {
	XMLName   xml.Name `xml:"urn:oasis:names:tc:opendocument:xmlns:container container"`
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

// Metadata represents the book's metadata.
type Metadata struct {
	Title      []string `xml:"http://purl.org/dc/elements/1.1/ title" json:"title,omitempty"`
	Creator    []string `xml:"http://purl.org/dc/elements/1.1/ creator" json:"creator,omitempty"`
	Language   []string `xml:"http://purl.org/dc/elements/1.1/ language" json:"language,omitempty"`
	Publisher  []string `xml:"http://purl.org/dc/elements/1.1/ publisher" json:"publisher,omitempty"`
	Identifier []string `xml:"http://purl.org/dc/elements/1.1/ identifier" json:"identifier,omitempty"`
	Date       []string `xml:"http://purl.org/dc/elements/1.1/ date" json:"date,omitempty"`
	Meta       []struct {
		Property string `xml:"property,attr" json:"property,omitempty"`
		Name     string `xml:"name,attr" json:"name,omitempty"`
		Content  string `xml:"content,attr" json:"content,omitempty"`
		Value    string `xml:",chardata" json:"value,omitempty"`
	} `xml:"meta" json:"meta,omitempty"`
}

// OPF represents the Open Package Format (.opf) file.
type OPF struct {
	XMLName  xml.Name `xml:"http://www.idpf.org/2007/opf package"`
	Metadata Metadata `xml:"metadata"`
	Manifest []Item   `xml:"manifest>item"`
	Spine    struct {
		Direction string `xml:"page-progression-direction,attr"`
		Items     []struct {
			IDRef      string `xml:"idref,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
	Guide []struct {
		Type  string `xml:"type,attr"`
		Title string `xml:"title,attr"`
		Href  string `xml:"href,attr"`
	} `xml:"guide>reference"`
}

// Item represents a resource in the manifest.
type Item struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

// --- Configuration ---

type Config struct {
	InputPaths      []string
	OutputPath      string
	Padding         int
	Verbose         bool
	DryRun          bool
	BlankMode       string
	BlankColor      string
	MetadataJSON    string
	Force           bool
	PrefixParts     bool
	TotalNumbering  bool
	NavType         string
	AlwaysOverwrite bool
	Compression     string
	Quiet           bool
}

// OutputPage represents a page to be written to the ZIP.
type OutputPage struct {
	SourceIdx     int
	PartPageNum   int
	GlobalPageNum int
	PartName      string
	PartIdx       int
}

func main() {
	cfg := parseFlags()

	isDir := false
	if cfg.OutputPath != "" {
		info, err := os.Stat(cfg.OutputPath)
		if err == nil && info.IsDir() {
			isDir = true
		}
	}

	anyFailed := false
	for _, inputPath := range cfg.InputPaths {
		targetOutput := cfg.OutputPath
		if targetOutput == "" {
			base := filepath.Base(inputPath)
			ext := filepath.Ext(base)
			targetOutput = strings.TrimSuffix(base, ext) + ".zip"
		} else if isDir {
			base := filepath.Base(inputPath)
			ext := filepath.Ext(base)
			targetOutput = filepath.Join(cfg.OutputPath, strings.TrimSuffix(base, ext)+".zip")
		} else if len(cfg.InputPaths) > 1 {
			fmt.Fprintf(os.Stderr, "Error: multiple input files provided but output '%s' is not a directory\n", cfg.OutputPath)
			os.Exit(1)
		}

		// Overwrite check
		if !cfg.DryRun && !cfg.AlwaysOverwrite {
			if _, err := os.Stat(targetOutput); err == nil {
				// In quiet (unattended) mode, never prompt: skip an existing
				// output unless -y was given. Otherwise, ask interactively.
				if cfg.Quiet || !askOverwrite(targetOutput) {
					if !cfg.Quiet {
						fmt.Printf("Skipping %s\n", targetOutput)
					}
					continue
				}
			}
		}

		if cfg.Verbose {
			log.Printf("Processing: %s -> %s\n", inputPath, targetOutput)
		}

		if err := run(cfg, inputPath, targetOutput); err != nil {
			fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", inputPath, err)
			anyFailed = true
		} else {
			if !cfg.Quiet && !cfg.DryRun {
				fmt.Println(targetOutput)
			}
		}
	}

	// Exit non-zero if any input failed to process, so scripts and agents can
	// detect partial failure (usage errors above exit 1; processing errors 2).
	if anyFailed {
		os.Exit(2)
	}
}

func askOverwrite(path string) bool {
	fmt.Printf("File '%s' already exists. Overwrite? [y/N]: ", path)
	var response string
	fmt.Scanln(&response)
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

// parseFlags initializes and returns the configuration based on CLI arguments.
// It sets up all available switches including output paths, numbering schemes, and blank page generation modes.
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
	flag.BoolVar(&cfg.PrefixParts, "prefix-parts", true, "Prefix filenames with structural part names")
	flag.BoolVar(&cfg.TotalNumbering, "total-numbering", false, "Include global page numbering")
	flag.StringVar(&cfg.NavType, "nav-type", "toc", "Navigation type: toc or landmarks")
	flag.BoolVar(&cfg.AlwaysOverwrite, "y", false, "Always overwrite existing files without prompting")
	flag.StringVar(&cfg.Compression, "c", "raw", "Compression method: raw, deflate, or store")
	flag.BoolVar(&cfg.Quiet, "q", false, "Disable STDOUT output")

	flag.Parse()

	for _, arg := range flag.Args() {
		if strings.ContainsAny(arg, "*?") {
			matches, _ := filepath.Glob(arg)
			if len(matches) > 0 {
				cfg.InputPaths = append(cfg.InputPaths, matches...)
				continue
			}
		}
		cfg.InputPaths = append(cfg.InputPaths, arg)
	}

	if len(cfg.InputPaths) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	return cfg
}

// run is the entry point for the tool's core logic.
// It orchestrates the EPUB reading, page extraction, logical part alignment, and final ZIP generation phases.
func run(cfg *Config, inputPath, outputPath string) error {
	switch cfg.Compression {
	case "raw", "deflate", "store":
		// valid
	default:
		return fmt.Errorf("invalid compression method: %s (must be raw, deflate, or store)", cfg.Compression)
	}

	if _, err := parseColor(cfg.BlankColor); err != nil {
		return err
	}

	reader, err := zip.OpenReader(inputPath)
	if err != nil {
		return fmt.Errorf("failed to open epub: %w", err)
	}
	defer reader.Close()

	fileMap := make(map[string]*zip.File)
	for _, f := range reader.File {
		fileMap[f.Name] = f
	}

	opfPath, err := findOPF(reader)
	if err != nil {
		return err
	}
	if cfg.Verbose {
		log.Printf("Found OPF: %s\n", opfPath)
	}

	opf, err := parseOPF(reader, opfPath)
	if err != nil {
		return err
	}

	if !isFixedLayout(opf.Metadata) {
		fmt.Fprintf(os.Stderr, "Warning: This EPUB appears to be reflowable (not fixed-layout).\n")
		if !cfg.Force {
			return fmt.Errorf("use -f to proceed")
		}
	}

	if cfg.Verbose {
		log.Printf("Page Progression Direction: %s\n", opf.Spine.Direction)
	}

	manifestMap := make(map[string]Item)
	for _, item := range opf.Manifest {
		manifestMap[item.ID] = item
	}

	var landmarks map[string]string
	if cfg.PrefixParts {
		landmarks = extractLandmarks(reader, opf, opfPath, cfg.NavType)
		if cfg.Verbose && len(landmarks) > 0 {
			log.Printf("Extracted %d structural landmarks using %s\n", len(landmarks), cfg.NavType)
		}
	}

	type ImageInfo struct {
		Path   string
		Width  int
		Height int
	}
	type Page struct {
		SourceHTML string
		Images     []ImageInfo
		IsBlank    bool
		Spread     string
	}
	var pages []Page

	opfBase := filepath.Dir(opfPath)
	for i, spineItem := range opf.Spine.Items {
		item, ok := manifestMap[spineItem.IDRef]
		if !ok {
			continue
		}

		spread := "center"
		if strings.Contains(spineItem.Properties, "page-spread-left") || strings.Contains(spineItem.Properties, "rendition:spread-left") {
			spread = "left"
		} else if strings.Contains(spineItem.Properties, "page-spread-right") || strings.Contains(spineItem.Properties, "rendition:spread-right") {
			spread = "right"
		}

		fullPath := filepath.ToSlash(filepath.Join(opfBase, item.Href))
		var absImgPaths []string
		sourceHTML := ""

		if strings.HasPrefix(item.MediaType, "image/") {
			absImgPaths = []string{fullPath}
		} else {
			sourceHTML = fullPath
			imgPaths, err := extractImageFromHTML(reader, fullPath)
			if err != nil {
				if cfg.Verbose {
					log.Printf("Source %d (%s): %v\n", i+1, fullPath, err)
				}
				pages = append(pages, Page{SourceHTML: sourceHTML, IsBlank: true, Spread: spread})
				continue
			}
			for _, ip := range imgPaths {
				absImgPaths = append(absImgPaths, filepath.ToSlash(filepath.Join(filepath.Dir(fullPath), ip)))
			}
		}

		var pageImages []ImageInfo
		for _, imgPath := range absImgPaths {
			w, h, err := getImageDimensions(reader, imgPath)
			if err != nil && cfg.Verbose {
				log.Printf("Failed to get dimensions for %s: %v\n", imgPath, err)
			}
			pageImages = append(pageImages, ImageInfo{Path: imgPath, Width: w, Height: h})
		}
		pages = append(pages, Page{SourceHTML: sourceHTML, Images: pageImages, Spread: spread})

		if cfg.Verbose {
			log.Printf("Source %d: Found %d images [spread: %s]\n", i+1, len(pageImages), spread)
			for j, img := range pageImages {
				log.Printf("  Image %d: %s (%dx%d)\n", j+1, img.Path, img.Width, img.Height)
			}
		}
	}

	isRTL := opf.Spine.Direction == "rtl"
	currentPartName := "PRELIM"
	currentPartIdx := 0
	partPageNum := 1
	globalPageNum := 1
	globalPhysicalIdx := 1

	var outputPages []OutputPage
	for i := range pages {
		p := &pages[i]
		if p.SourceHTML != "" && landmarks != nil {
			if partType, ok := landmarks[p.SourceHTML]; ok {
				if partType != currentPartName {
					currentPartName = partType
					currentPartIdx++
					partPageNum = 1
					if cfg.Verbose {
						log.Printf("Part %d Started: %s (at source page %d, physical pos %d)\n", currentPartIdx, currentPartName, i+1, globalPhysicalIdx)
					}
				}
			}
		}

		needsPadding := false
		if isRTL {
			if p.Spread == "right" && globalPhysicalIdx%2 != 0 {
				needsPadding = true
			} else if p.Spread == "left" && globalPhysicalIdx%2 == 0 {
				needsPadding = true
			}
		} else {
			if p.Spread == "left" && globalPhysicalIdx%2 != 0 {
				needsPadding = true
			} else if p.Spread == "right" && globalPhysicalIdx%2 == 0 {
				needsPadding = true
			}
		}

		if needsPadding {
			if cfg.BlankMode == "generate" || cfg.BlankMode == "skip" {
				if cfg.Verbose {
					log.Printf("Aligning physical pos %d (Source %d) due to %s spread\n", globalPhysicalIdx, i+1, p.Spread)
				}
				outputPages = append(outputPages, OutputPage{
					SourceIdx:     -1,
					PartPageNum:   partPageNum,
					GlobalPageNum: globalPageNum,
					PartName:      currentPartName,
					PartIdx:       currentPartIdx,
				})
				partPageNum++
				globalPageNum++
				globalPhysicalIdx++
			}
		}

		outputPages = append(outputPages, OutputPage{
			SourceIdx:     i,
			PartPageNum:   partPageNum,
			GlobalPageNum: globalPageNum,
			PartName:      currentPartName,
			PartIdx:       currentPartIdx,
		})
		partPageNum++
		globalPageNum++
		globalPhysicalIdx++
	}

	if cfg.DryRun {
		if !cfg.Quiet {
			fmt.Printf("Dry run: planned output to %s (Direction: %s)\n", outputPath, opf.Spine.Direction)
			for _, op := range outputPages {
				name := generateFileName(cfg, op, 0, op.SourceIdx == -1 || (op.SourceIdx != -1 && pages[op.SourceIdx].IsBlank), ".png")
				if op.SourceIdx == -1 {
					fmt.Printf("  Page %s: [Alignment Blank]\n", name)
				} else if pages[op.SourceIdx].IsBlank {
					fmt.Printf("  Page %s: [Skipped Blank]\n", name)
				} else {
					for j, img := range pages[op.SourceIdx].Images {
						imgIdx := 0
						if len(pages[op.SourceIdx].Images) > 1 {
							imgIdx = j + 1
						}
						ext := strings.ToLower(filepath.Ext(img.Path))
						if ext == "" {
							ext = ".jpg"
						}
						fname := generateFileName(cfg, op, imgIdx, false, ext)
						fmt.Printf("  Page %s: %s\n", fname, img.Path)
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

	if cfg.MetadataJSON != "none" {
		var data []byte
		if cfg.MetadataJSON == "pretty" {
			data, _ = json.MarshalIndent(opf.Metadata, "", "  ")
		} else {
			data, _ = json.Marshal(opf.Metadata)
		}
		var w io.Writer
		method := zip.Deflate
		if cfg.Compression == "store" {
			method = zip.Store
		}
		fh := &zip.FileHeader{
			Name:    "metadata.json",
			Method:  method,
			Flags:   0x800,
			NonUTF8: false,
		}
		w, _ = archive.CreateHeader(fh)
		w.Write(data)
	}

	for _, op := range outputPages {
		var p *Page
		if op.SourceIdx != -1 {
			p = &pages[op.SourceIdx]
		}

		if op.SourceIdx == -1 || p.IsBlank {
			if cfg.BlankMode != "generate" {
				continue
			}
			var w, h int
			if op.SourceIdx != -1 && len(p.Images) > 0 {
				w, h = p.Images[0].Width, p.Images[0].Height
			} else {
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

			name := generateFileName(cfg, op, 0, true, ".png")
			var writer io.Writer
			method := zip.Deflate
			if cfg.Compression == "store" {
				method = zip.Store
			}
			fh := &zip.FileHeader{
				Name:    name,
				Method:  method,
				Flags:   0x800,
				NonUTF8: false,
			}
			writer, _ = archive.CreateHeader(fh)
			col, _ := parseColor(cfg.BlankColor)
			generateBlankImage(writer, w, h, col)
			continue
		}

		for j, img := range p.Images {
			imgIdx := 0
			if len(p.Images) > 1 {
				imgIdx = j + 1
			}
			ext := strings.ToLower(filepath.Ext(img.Path))
			if ext == "" {
				ext = ".jpg"
			}
			name := generateFileName(cfg, op, imgIdx, false, ext)

			srcFile, ok := fileMap[img.Path]
			if !ok {
				return fmt.Errorf("file %s not found in epub", img.Path)
			}

			var writer io.Writer
			var r io.Reader
			var rc io.ReadCloser
			var openErr, createErr error

			if cfg.Compression == "raw" {
				r, openErr = srcFile.OpenRaw()
				if openErr == nil {
					fh := srcFile.FileHeader
					fh.Name = name
					fh.Flags |= 0x800
					fh.NonUTF8 = false
					writer, createErr = archive.CreateRaw(&fh)
				}
			} else {
				rc, openErr = srcFile.Open()
				r = rc
				if openErr == nil {
					fh := srcFile.FileHeader
					fh.Name = name
					if cfg.Compression == "store" {
						fh.Method = zip.Store
					} else {
						fh.Method = zip.Deflate
					}
					fh.Flags |= 0x800
					fh.NonUTF8 = false
					fh.CRC32 = 0
					fh.CompressedSize = 0
					fh.CompressedSize64 = 0
					fh.UncompressedSize = 0
					fh.UncompressedSize64 = 0
					writer, createErr = archive.CreateHeader(&fh)
				}
			}

			if openErr != nil {
				return fmt.Errorf("failed to open source image %s: %w", img.Path, openErr)
			}
			if createErr != nil {
				if rc != nil {
					rc.Close()
				}
				return fmt.Errorf("failed to create zip entry for %s: %w", name, createErr)
			}

			_, err = io.Copy(writer, r)
			if rc != nil {
				rc.Close()
			}
			if err != nil {
				return fmt.Errorf("failed to copy data for %s: %w", name, err)
			}
		}
	}

	if cfg.Verbose {
		log.Printf("Created %s\n", outputPath)
	}
	return nil
}

// generateFileName constructs the output filename based on structural part info and the chosen numbering settings.
// It handles cases for combined global numbering, part-only prefixing, and suffixing for multi-image pages or generated blanks.
func generateFileName(cfg *Config, op OutputPage, imgIdx int, isBlank bool, ext string) string {
	suffix := ""
	if isBlank {
		suffix = "_blank"
	}
	if imgIdx > 0 {
		suffix += fmt.Sprintf("_%d", imgIdx)
	}

	if cfg.PrefixParts && op.PartName != "" {
		if cfg.TotalNumbering {
			// Global_PartIdx_PartName_PartPageNum
			return fmt.Sprintf("%0*d_%02d_%s_%0*d%s%s", cfg.Padding, op.GlobalPageNum, op.PartIdx, op.PartName, cfg.Padding, op.PartPageNum, suffix, ext)
		}
		// PartIdx_PartName_PartPageNum
		return fmt.Sprintf("%02d_%s_%0*d%s%s", op.PartIdx, op.PartName, cfg.Padding, op.PartPageNum, suffix, ext)
	}

	if cfg.TotalNumbering || !cfg.PrefixParts {
		return fmt.Sprintf("%0*d%s%s", cfg.Padding, op.GlobalPageNum, suffix, ext)
	}
	return fmt.Sprintf("%0*d%s%s", cfg.Padding, op.PartPageNum, suffix, ext)
}

// parseColor converts a string (named color like "white" or hex code like "#RRGGBB") into a color.Color interface.
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
		if len(s) == 7 {
			fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b)
			return color.RGBA{r, g, b, a}, nil
		} else if len(s) == 9 {
			fmt.Sscanf(s, "#%02x%02x%02x%02x", &r, &g, &b, &a)
			return color.RGBA{r, g, b, a}, nil
		}
	}
	return nil, fmt.Errorf("unknown color: %s", s)
}

// generateBlankImage creates a solid-color PNG image of the specified dimensions and writes it to the provided writer.
func generateBlankImage(w io.Writer, width, height int, col color.Color) error {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{col}, image.Point{}, draw.Src)
	return png.Encode(w, img)
}

// findOPF reads the META-INF/container.xml file within the EPUB archive to locate the root OPF package file.
func findOPF(r *zip.ReadCloser) (string, error) {
	f, err := r.Open("META-INF/container.xml")
	if err != nil {
		return "", fmt.Errorf("META-INF/container.xml not found")
	}
	defer f.Close()
	var c Container
	xml.NewDecoder(f).Decode(&c)
	if len(c.Rootfiles) == 0 {
		return "", fmt.Errorf("no rootfile")
	}
	return c.Rootfiles[0].FullPath, nil
}

// parseOPF decodes the specified OPF XML file into an accessible Go struct, extracting metadata, manifest, and spine.
func parseOPF(r *zip.ReadCloser, path string) (*OPF, error) {
	f, err := r.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var opf OPF
	xml.NewDecoder(f).Decode(&opf)
	return &opf, nil
}

// extractImageFromHTML parses an XHTML document to find all embedded <img> or <svg><image> elements.
// This is necessary because many fixed-layout EPUBs wrap their images in XHTML documents rather than referencing them directly in the spine.
func extractImageFromHTML(r *zip.ReadCloser, path string) ([]string, error) {
	f, err := r.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	doc, _ := html.Parse(f)
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
			} else if n.Data == "image" {
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
		return nil, fmt.Errorf("no image")
	}
	return imgPaths, nil
}

// extractLandmarks builds a map of spine item paths to their structural part name (e.g., "cover", "bodymatter").
// It supports both EPUB 3 navigation documents (TOC or landmarks) and fallback EPUB 2 <guide> elements.
func extractLandmarks(r *zip.ReadCloser, opf *OPF, opfPath string, navType string) map[string]string {
	landmarks := make(map[string]string)
	opfBase := filepath.Dir(opfPath)
	var navPath string
	for _, item := range opf.Manifest {
		if strings.Contains(item.Properties, "nav") {
			navPath = filepath.ToSlash(filepath.Join(opfBase, item.Href))
			break
		}
	}
	if navPath != "" {
		f, err := r.Open(navPath)
		if err == nil {
			defer f.Close()
			doc, _ := html.Parse(f)
			var fNode func(*html.Node)
			fNode = func(n *html.Node) {
				if n.Type == html.ElementNode && n.Data == "nav" {
					isTarget := false
					for _, a := range n.Attr {
						if a.Key == "epub:type" && a.Val == navType {
							isTarget = true
						}
					}
					if isTarget {
						var fAnchor func(*html.Node)
						fAnchor = func(an *html.Node) {
							if an.Type == html.ElementNode && an.Data == "a" {
								var href string
								for _, a := range an.Attr {
									if a.Key == "href" {
										href = a.Val
									}
								}
								if href != "" {
									text := cleanFilename(getNodeText(an))
									if text != "" {
										cleanHref := strings.Split(href, "#")[0]
										absPath := filepath.ToSlash(filepath.Join(filepath.Dir(navPath), cleanHref))
										landmarks[absPath] = text
									}
								}
							}
							for c := an.FirstChild; c != nil; c = c.NextSibling {
								fAnchor(c)
							}
						}
						fAnchor(n)
					}
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					fNode(c)
				}
			}
			fNode(doc)
		}
	}
	if len(landmarks) == 0 {
		for _, ref := range opf.Guide {
			cleanHref := strings.Split(ref.Href, "#")[0]
			absPath := filepath.ToSlash(filepath.Join(opfBase, cleanHref))
			name := ref.Title
			if name == "" {
				name = ref.Type
			}
			landmarks[absPath] = cleanFilename(name)
		}
	}
	return landmarks
}

// getNodeText recursively extracts and concatenates all text content from an HTML node and its children.
func getNodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var text string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text += getNodeText(c)
	}
	return text
}

// cleanFilename removes or replaces characters that are illegal or problematic in OS filesystems.
// It converts characters like '/', ':', and '*' to their full-width (Japanese) equivalents to preserve readability without breaking paths.
func cleanFilename(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		// Convert illegal OS path characters to fullwidth variants
		switch r {
		case '/':
			return '／'
		case '\\':
			return '＼'
		case ':':
			return '：'
		case '*':
			return '＊'
		case '?':
			return '？'
		case '"':
			return '”'
		case '<':
			return '＜'
		case '>':
			return '＞'
		case '|':
			return '｜'
		}

		// Keep alphanumeric, dash, underscore, and non-ASCII (including Japanese/fullwidth)
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r > 127 {
			return r
		}
		// Replace whitespace with underscore
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return '_'
		}
		return -1 // Remove other unhandled characters
	}, s)
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	return strings.Trim(s, "_")
}

// isFixedLayout inspects the EPUB's metadata to determine if the book is fixed-layout or reflowable text.
func isFixedLayout(m Metadata) bool {
	for _, meta := range m.Meta {
		if meta.Property == "rendition:layout" && meta.Value == "pre-paginated" {
			return true
		}
		if meta.Name == "fixed-layout" && meta.Content == "true" {
			return true
		}
		if strings.Contains(meta.Property, "viewport") {
			return true
		}
	}
	return false
}

// getImageDimensions efficiently decodes the headers of an image file within the ZIP stream to retrieve its width and height,
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
