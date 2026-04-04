package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/carlos7ags/folio/document"
	foliohtml "github.com/carlos7ags/folio/html"
	"github.com/carlos7ags/folio/layout"
	"github.com/carlos7ags/folio/reader"
)

// renderHTML converts HTML to PDF bytes using the folio library directly.
func renderHTML(htmlStr string, opts renderOpts) ([]byte, error) {
	pageSize := resolvePageSize(opts.Format)

	doc := document.NewDocument(pageSize)

	if opts.hasMargins() {
		doc.SetMargins(layout.Margins{
			Top:    parsePt(opts.MarginTop),
			Right:  parsePt(opts.MarginRight),
			Bottom: parsePt(opts.MarginBottom),
			Left:   parsePt(opts.MarginLeft),
		})
	}

	// Inject bundled fonts so Inter and Geist are available via CSS font-family
	fontsPath, err := ensureFonts()
	if err == nil && fontsPath != "" {
		htmlStr = injectFontCSS(htmlStr, fontFaceCSS(fontsPath))
	}

	if err := doc.AddHTML(htmlStr, &foliohtml.Options{
		PageWidth:  pageSize.Width,
		PageHeight: pageSize.Height,
	}); err != nil {
		return nil, fmt.Errorf("html render failed: %w", err)
	}

	if opts.Watermark != "" {
		doc.SetWatermark(opts.Watermark)
	}

	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("pdf write failed: %w", err)
	}
	return buf.Bytes(), nil
}

// renderHTMLTemplate renders HTML with Go template variables.
func renderHTMLTemplate(tmplStr string, data map[string]any, opts renderOpts) ([]byte, error) {
	pageSize := resolvePageSize(opts.Format)

	doc := document.NewDocument(pageSize)

	if opts.hasMargins() {
		doc.SetMargins(layout.Margins{
			Top:    parsePt(opts.MarginTop),
			Right:  parsePt(opts.MarginRight),
			Bottom: parsePt(opts.MarginBottom),
			Left:   parsePt(opts.MarginLeft),
		})
	}

	if err := doc.AddHTMLTemplate(tmplStr, data, &foliohtml.Options{
		PageWidth:  pageSize.Width,
		PageHeight: pageSize.Height,
	}); err != nil {
		return nil, fmt.Errorf("template render failed: %w", err)
	}

	if opts.Watermark != "" {
		doc.SetWatermark(opts.Watermark)
	}

	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("pdf write failed: %w", err)
	}
	return buf.Bytes(), nil
}

// manipulatePDF performs merge, split, or rotate operations on PDFs.
func manipulatePDF(operation string, inputs [][]byte, pages string, degrees int) ([]byte, error) {
	switch strings.ToLower(operation) {
	case "merge":
		return mergePDFs(inputs)
	case "split":
		return splitPDF(inputs, pages)
	case "rotate":
		return rotatePDF(inputs, degrees)
	default:
		return nil, fmt.Errorf("unsupported operation: %s", operation)
	}
}

func mergePDFs(inputs [][]byte) ([]byte, error) {
	if len(inputs) < 2 {
		return nil, fmt.Errorf("merge requires at least two PDFs")
	}

	readers := make([]*reader.PdfReader, len(inputs))
	for i, data := range inputs {
		pr, err := reader.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse PDF %d: %w", i+1, err)
		}
		readers[i] = pr
	}

	modifier, err := reader.Merge(readers...)
	if err != nil {
		return nil, fmt.Errorf("merge failed: %w", err)
	}

	var buf bytes.Buffer
	if _, err := modifier.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("write merged PDF failed: %w", err)
	}
	return buf.Bytes(), nil
}

func splitPDF(inputs [][]byte, pages string) ([]byte, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("split requires at least one PDF")
	}
	if pages == "" {
		return nil, fmt.Errorf("split requires a 'pages' value (e.g. \"1-3\")")
	}

	pr, err := reader.Parse(inputs[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse PDF: %w", err)
	}

	modifier, err := reader.Merge(pr)
	if err != nil {
		return nil, fmt.Errorf("split init failed: %w", err)
	}

	keepIndices, err := parsePageRange(pages, modifier.PageCount())
	if err != nil {
		return nil, fmt.Errorf("invalid page range: %w", err)
	}

	// Build set of pages to keep
	keep := make(map[int]bool, len(keepIndices))
	for _, idx := range keepIndices {
		keep[idx] = true
	}

	// Remove pages not in the keep set, in reverse order to preserve indices
	for i := modifier.PageCount() - 1; i >= 0; i-- {
		if !keep[i] {
			if err := modifier.RemovePage(i); err != nil {
				return nil, fmt.Errorf("remove page %d: %w", i+1, err)
			}
		}
	}

	var buf bytes.Buffer
	if _, err := modifier.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("write split PDF failed: %w", err)
	}
	return buf.Bytes(), nil
}

func rotatePDF(inputs [][]byte, degrees int) ([]byte, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("rotate requires at least one PDF")
	}
	if degrees == 0 {
		return nil, fmt.Errorf("rotate requires a non-zero 'degrees' value")
	}

	pr, err := reader.Parse(inputs[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse PDF: %w", err)
	}

	modifier, err := reader.Merge(pr)
	if err != nil {
		return nil, fmt.Errorf("rotate init failed: %w", err)
	}

	for i := range modifier.PageCount() {
		if err := modifier.RotatePage(i, degrees); err != nil {
			return nil, fmt.Errorf("rotate page %d failed: %w", i+1, err)
		}
	}

	var buf bytes.Buffer
	if _, err := modifier.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("write rotated PDF failed: %w", err)
	}
	return buf.Bytes(), nil
}

// extractPDF extracts text and metadata from a PDF.
func extractPDF(data []byte) (*extractResult, error) {
	pr, err := reader.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PDF: %w", err)
	}

	title, author, subject, creator, producer := pr.Info()
	result := &extractResult{
		Metadata: extractMetadata{
			PageCount: pr.PageCount(),
			Title:     title,
			Author:    author,
			Subject:   subject,
			Creator:   creator,
			Producer:  producer,
			Version:   pr.Version(),
		},
	}

	var allText strings.Builder
	for i := range pr.PageCount() {
		page, err := pr.Page(i)
		if err != nil {
			continue
		}

		text, _ := page.ExtractText()
		box := page.VisibleBox()

		result.Pages = append(result.Pages, extractPage{
			PageNumber: i + 1,
			Text:       text,
			Width:      box.Width(),
			Height:     box.Height(),
		})

		if allText.Len() > 0 {
			allText.WriteString("\n\n")
		}
		allText.WriteString(text)
	}
	result.Text = allText.String()

	return result, nil
}

// pdfPageCount parses PDF bytes and returns the page count.
func pdfPageCount(data []byte) (int, error) {
	pr, err := reader.Parse(data)
	if err != nil {
		return 0, err
	}
	return pr.PageCount(), nil
}

// --- Helpers ---

type renderOpts struct {
	Format       string
	MarginTop    string
	MarginRight  string
	MarginBottom string
	MarginLeft   string
	Watermark    string
}

func (o renderOpts) hasMargins() bool {
	return o.MarginTop != "" || o.MarginRight != "" || o.MarginBottom != "" || o.MarginLeft != ""
}

func resolvePageSize(format string) document.PageSize {
	switch strings.ToLower(format) {
	case "a3":
		return document.PageSizeA3
	case "a4":
		return document.PageSizeA4
	case "a5":
		return document.PageSizeA5
	case "legal":
		return document.PageSizeLegal
	case "tabloid":
		return document.PageSizeTabloid
	case "letter", "":
		return document.PageSizeLetter
	default:
		return document.PageSizeLetter
	}
}

// parsePt converts CSS margin strings to PDF points.
func parsePt(s string) float64 {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0
	}

	var val float64
	var unit string

	for i, c := range s {
		if (c < '0' || c > '9') && c != '.' {
			fmt.Sscanf(s[:i], "%f", &val)
			unit = s[i:]
			break
		}
		if i == len(s)-1 {
			fmt.Sscanf(s, "%f", &val)
			unit = "pt"
		}
	}

	switch strings.TrimSpace(unit) {
	case "in":
		return val * 72
	case "cm":
		return val * 28.3465
	case "mm":
		return val * 2.83465
	case "pt", "":
		return val
	default:
		return val
	}
}

// parsePageRange parses "1-3", "2,4,6", or "1,3-5" into 0-based page indices.
func parsePageRange(s string, totalPages int) ([]int, error) {
	var indices []int
	parts := strings.Split(s, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			var start, end int
			fmt.Sscanf(bounds[0], "%d", &start)
			fmt.Sscanf(bounds[1], "%d", &end)
			if start < 1 || end > totalPages || start > end {
				return nil, fmt.Errorf("range %s out of bounds (1-%d)", part, totalPages)
			}
			for i := start; i <= end; i++ {
				indices = append(indices, i-1)
			}
		} else {
			var page int
			fmt.Sscanf(part, "%d", &page)
			if page < 1 || page > totalPages {
				return nil, fmt.Errorf("page %d out of bounds (1-%d)", page, totalPages)
			}
			indices = append(indices, page-1)
		}
	}

	if len(indices) == 0 {
		return nil, fmt.Errorf("no pages specified")
	}
	return indices, nil
}
