package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html/template"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxFileSizeBytes = 100 * 1024 * 1024 // 100 MB

// --- Input Structs ---

type RenderHTMLInput struct {
	HTML         string         `json:"html" jsonschema:"The HTML to render. Supports full CSS, Tailwind v3 (if enabled), and Go template syntax like {{.name}}."`
	Data         map[string]any `json:"data,omitempty" jsonschema:"Variables for Go template placeholders. Use {{.key}} in HTML, pass values here."`
	OutputPath   string         `json:"output_path,omitempty" jsonschema:"Save path on the host filesystem, e.g. '/tmp/invoice.pdf'. If omitted, auto-generates a temp path."`
	Format       string         `json:"format,omitempty" jsonschema:"Page size: A4, Letter, Legal, A3, A5, Tabloid. Default: Letter"`
	MarginTop    string         `json:"margin_top,omitempty" jsonschema:"Top margin, e.g. '1in', '20mm', '2cm'. Default: 1in"`
	MarginRight  string         `json:"margin_right,omitempty" jsonschema:"Right margin, e.g. '1in', '20mm'. Default: 1in"`
	MarginBottom string         `json:"margin_bottom,omitempty" jsonschema:"Bottom margin. Default: 1in"`
	MarginLeft   string         `json:"margin_left,omitempty" jsonschema:"Left margin. Default: 1in"`
	HeaderHTML   string         `json:"header_html,omitempty" jsonschema:"HTML for page header (repeats on every page). Use <span class='pageNumber'></span> and <span class='totalPages'></span> for page numbers."`
	FooterHTML   string         `json:"footer_html,omitempty" jsonschema:"HTML for page footer (repeats on every page). Use <span class='pageNumber'></span> and <span class='totalPages'></span> for page numbers."`
	Tailwind     bool           `json:"tailwind,omitempty" jsonschema:"Set true to enable Tailwind CSS v3 utility classes (bundled locally, no internet needed). Tailwind v4 is not supported."`
	Watermark    string         `json:"watermark,omitempty" jsonschema:"Diagonal text overlay, e.g. 'DRAFT' or 'CONFIDENTIAL'"`
}

type RenderMarkdownInput struct {
	Markdown       string         `json:"markdown" jsonschema:"The Markdown to render. Supports code blocks with syntax highlighting."`
	Data           map[string]any `json:"data,omitempty" jsonschema:"Variables for template placeholders. Use {{.key}} in markdown, pass values here."`
	OutputPath     string         `json:"output_path,omitempty" jsonschema:"Save path on the host filesystem. If omitted, auto-generates a temp path."`
	Theme          string         `json:"theme,omitempty" jsonschema:"Document theme: github (clean, readable), minimal (serif, no decoration), print (formal, justified). Default: github"`
	SyntaxTheme    string         `json:"syntax_theme,omitempty" jsonschema:"Code block color scheme, e.g. monokai, dracula, github"`
	Format         string         `json:"format,omitempty" jsonschema:"Page size: A4, Letter, Legal, A3, A5, Tabloid. Default: Letter"`
	MarginTop      string         `json:"margin_top,omitempty" jsonschema:"Top margin, e.g. '1in', '20mm'. Default: 1in"`
	MarginRight    string         `json:"margin_right,omitempty" jsonschema:"Right margin. Default: 1in"`
	MarginBottom   string         `json:"margin_bottom,omitempty" jsonschema:"Bottom margin. Default: 1in"`
	MarginLeft     string         `json:"margin_left,omitempty" jsonschema:"Left margin. Default: 1in"`
	Watermark      string         `json:"watermark,omitempty" jsonschema:"Diagonal text overlay, e.g. 'DRAFT' or 'CONFIDENTIAL'"`
	WatermarkScope string         `json:"watermark_scope,omitempty" jsonschema:"Where to show watermark: all-pages (default) or first-page"`
	Tailwind       bool           `json:"tailwind,omitempty" jsonschema:"Set true to enable Tailwind CSS v3 utility classes (bundled locally). Tailwind v4 is not supported."`
}

type ManipulatePDFInput struct {
	Operation string   `json:"operation" jsonschema:"What to do: 'merge' to combine PDFs, 'split' to extract pages, 'rotate' to rotate pages"`
	Files     []string `json:"files,omitempty" jsonschema:"PDF file paths to process. Provide one or more absolute paths."`
	Data      []string `json:"data,omitempty" jsonschema:"Base64-encoded PDF data. Alternative to file paths for remote use."`
	Pages     string   `json:"pages,omitempty" jsonschema:"Pages to extract (split only), e.g. '1-3' or '1,3,5'. 1-indexed."`
	Degrees   int      `json:"degrees,omitempty" jsonschema:"Rotation amount (rotate only). Must be a multiple of 90, e.g. 90, 180, 270, -90"`
}

type ExtractPDFInput struct {
	File  string `json:"file,omitempty" jsonschema:"Absolute path to the PDF file to extract from"`
	Data  string `json:"data,omitempty" jsonschema:"Base64-encoded PDF data. Alternative to file path."`
	Pages string `json:"pages,omitempty" jsonschema:"Limit extraction to these pages, e.g. '1-5' or '2,4'. 1-indexed."`
}

// --- Validation ---

var validFormats = map[string]bool{
	"A3": true, "A4": true, "A5": true,
	"Letter": true, "Legal": true, "Tabloid": true,
}

var validThemes = map[string]bool{
	"github": true, "minimal": true, "print": true,
}

var validWatermarkScopes = map[string]bool{
	"all-pages": true, "first-page": true,
}

var validOperations = map[string]bool{
	"merge": true, "split": true, "rotate": true,
}

func validateFormat(format string) error {
	if format != "" && !validFormats[format] {
		return fmt.Errorf("invalid format %q (use: A3, A4, A5, Letter, Legal, Tabloid)", format)
	}
	return nil
}

func validatePageRange(pages string) error {
	if pages == "" {
		return nil
	}
	parts := strings.FieldsFunc(pages, func(r rune) bool { return r == ',' || r == '-' })
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 {
			return fmt.Errorf("invalid page range %q: each value must be a positive integer", pages)
		}
	}
	return nil
}

func validateEnum(value, name string, valid map[string]bool) error {
	if value != "" && !valid[value] {
		keys := make([]string, 0, len(valid))
		for k := range valid {
			keys = append(keys, k)
		}
		return fmt.Errorf("invalid %s %q (use: %s)", name, value, strings.Join(keys, ", "))
	}
	return nil
}

// --- File handling ---

func validateFilePath(path string) error {
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed: %s", path)
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return fmt.Errorf("relative paths not allowed, use an absolute path: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return fmt.Errorf("cannot resolve path %s: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", path)
	}
	if info.Size() > maxFileSizeBytes {
		return fmt.Errorf("file too large: %s is %s (max %s)",
			path, formatSize(int(info.Size())), formatSize(maxFileSizeBytes))
	}
	return nil
}

func readFileBytes(path string) ([]byte, error) {
	if err := validateFilePath(path); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Clean(path))
}

// --- PDF delivery ---

var pdfOutputDirOverride string

func pdfOutputDir() (string, error) {
	if pdfOutputDirOverride != "" {
		if err := os.MkdirAll(pdfOutputDirOverride, 0755); err != nil {
			return "", err
		}
		return pdfOutputDirOverride, nil
	}
	return os.TempDir(), nil
}

func deliverPDF(data []byte, label string, outputPath string) *mcp.CallToolResult {
	sizeStr := formatSize(len(data))

	// Determine save path
	var path string
	if outputPath != "" {
		// User-specified path — ensure parent directory exists
		dir := filepath.Dir(outputPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return errorResult("Failed to create output directory: " + err.Error())
		}
		path = outputPath
	} else {
		dir, err := pdfOutputDir()
		if err != nil {
			return errorResult("Failed to create output directory: " + err.Error())
		}
		path = filepath.Join(dir, fmt.Sprintf("foliopdf-%d.pdf", time.Now().UnixMilli()))
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return errorResult("Failed to write PDF: " + err.Error())
	}

	// Count pages for metadata
	pageCount := 0
	if pr, err := pdfPageCount(data); err == nil {
		pageCount = pr
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	var msg strings.Builder
	fmt.Fprintf(&msg, "%s PDF (%s, %d page(s)). Saved to: %s", label, sizeStr, pageCount, path)
	fmt.Fprintf(&msg, "\n\nBase64 (decode to .pdf):\n%s", b64)
	return textResult(msg.String())
}

func formatSize(bytes int) string {
	switch {
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(bytes)/1024/1024)
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}

// --- Template resolution ---

func resolveTemplate(tmplStr string, data map[string]any) (string, error) {
	if data == nil || len(data) == 0 {
		return tmplStr, nil
	}
	t, err := template.New("render").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execution error: %w", err)
	}
	return buf.String(), nil
}

// --- Handler Factories ---

func handleRenderHTML(logger *slog.Logger) mcp.ToolHandlerFor[RenderHTMLInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input RenderHTMLInput) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(input.HTML) == "" {
			return errorResult("HTML content cannot be empty."), nil, nil
		}
		if err := validateFormat(input.Format); err != nil {
			return errorResult(err.Error()), nil, nil
		}

		logger.Info("render_html called", "format", input.Format, "tailwind", input.Tailwind)

		// Resolve template variables if data provided
		htmlStr := input.HTML
		if len(input.Data) > 0 {
			resolved, err := resolveTemplate(htmlStr, input.Data)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			htmlStr = resolved
		}

		if input.Tailwind {
			htmlStr = injectTailwind(htmlStr, tailwindCSS)
		}

		if input.HeaderHTML != "" || input.FooterHTML != "" {
			htmlStr = injectHeaderFooter(htmlStr, input.HeaderHTML, input.FooterHTML)
		}

		pdfBytes, err := renderHTML(htmlStr, renderOpts{
			Format:       input.Format,
			MarginTop:    input.MarginTop,
			MarginRight:  input.MarginRight,
			MarginBottom: input.MarginBottom,
			MarginLeft:   input.MarginLeft,
			Watermark:    input.Watermark,
		})
		if err != nil {
			logger.Error("render_html failed", "error", err)
			return errorResult("Render failed: " + err.Error()), nil, nil
		}

		return deliverPDF(pdfBytes, "Generated", input.OutputPath), nil, nil
	}
}

func handleRenderMarkdown(logger *slog.Logger) mcp.ToolHandlerFor[RenderMarkdownInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input RenderMarkdownInput) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(input.Markdown) == "" {
			return errorResult("Markdown content cannot be empty."), nil, nil
		}
		if err := validateFormat(input.Format); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		if err := validateEnum(input.Theme, "theme", validThemes); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		if err := validateEnum(input.WatermarkScope, "watermark_scope", validWatermarkScopes); err != nil {
			return errorResult(err.Error()), nil, nil
		}

		logger.Info("render_markdown called", "format", input.Format, "theme", input.Theme)

		// Resolve template variables
		mdStr := input.Markdown
		if len(input.Data) > 0 {
			resolved, err := resolveTemplate(mdStr, input.Data)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			mdStr = resolved
		}

		// Convert markdown to HTML
		htmlStr, err := convertMarkdown(mdStr, markdownOpts{
			Theme:       input.Theme,
			SyntaxTheme: input.SyntaxTheme,
		})
		if err != nil {
			return errorResult("Markdown conversion failed: " + err.Error()), nil, nil
		}

		if input.Tailwind {
			htmlStr = injectTailwind(htmlStr, tailwindCSS)
		}

		pdfBytes, err := renderHTML(htmlStr, renderOpts{
			Format:       input.Format,
			MarginTop:    input.MarginTop,
			MarginRight:  input.MarginRight,
			MarginBottom: input.MarginBottom,
			MarginLeft:   input.MarginLeft,
			Watermark:    input.Watermark,
		})
		if err != nil {
			logger.Error("render_markdown failed", "error", err)
			return errorResult("Render failed: " + err.Error()), nil, nil
		}

		return deliverPDF(pdfBytes, "Generated", input.OutputPath), nil, nil
	}
}

func handleManipulatePDF(logger *slog.Logger) mcp.ToolHandlerFor[ManipulatePDFInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input ManipulatePDFInput) (*mcp.CallToolResult, any, error) {
		if err := validateEnum(input.Operation, "operation", validOperations); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		if err := validatePageRange(input.Pages); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		if input.Degrees != 0 && input.Degrees%90 != 0 {
			return errorResult("Invalid rotation degrees: must be a multiple of 90."), nil, nil
		}

		logger.Info("manipulate_pdf called", "operation", input.Operation)

		// Collect PDF byte inputs
		var inputs [][]byte

		if len(input.Files) > 0 {
			for _, f := range input.Files {
				data, err := readFileBytes(f)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				inputs = append(inputs, data)
			}
		} else if len(input.Data) > 0 {
			for _, d := range input.Data {
				decoded, err := base64.StdEncoding.DecodeString(d)
				if err != nil {
					return errorResult("Invalid base64 data: " + err.Error()), nil, nil
				}
				inputs = append(inputs, decoded)
			}
		} else {
			return errorResult("No PDF inputs provided. Supply file paths in 'files' or base64 data in 'data'."), nil, nil
		}

		pdfBytes, err := manipulatePDF(input.Operation, inputs, input.Pages, input.Degrees)
		if err != nil {
			logger.Error("manipulate_pdf failed", "error", err)
			return errorResult("Operation failed: " + err.Error()), nil, nil
		}

		return deliverPDF(pdfBytes, "Manipulated", ""), nil, nil
	}
}

func handleExtractPDF(logger *slog.Logger) mcp.ToolHandlerFor[ExtractPDFInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input ExtractPDFInput) (*mcp.CallToolResult, any, error) {
		if err := validatePageRange(input.Pages); err != nil {
			return errorResult(err.Error()), nil, nil
		}

		logger.Info("extract_pdf called", "has_file", input.File != "", "has_data", input.Data != "")

		var pdfBytes []byte

		if input.File != "" {
			data, err := readFileBytes(input.File)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			pdfBytes = data
		} else if input.Data != "" {
			decoded, err := base64.StdEncoding.DecodeString(input.Data)
			if err != nil {
				return errorResult("Invalid base64 data: " + err.Error()), nil, nil
			}
			pdfBytes = decoded
		} else {
			return errorResult("No PDF input provided. Supply a file path in 'file' or base64 data in 'data'."), nil, nil
		}

		result, err := extractPDF(pdfBytes)
		if err != nil {
			logger.Error("extract_pdf failed", "error", err)
			return errorResult("Extraction failed: " + err.Error()), nil, nil
		}

		// Format output
		output := formatExtractResult(result)
		return textResult(output), nil, nil
	}
}

// --- Extract response types ---

type extractResult struct {
	Pages    []extractPage   `json:"pages"`
	Text     string          `json:"text"`
	Metadata extractMetadata `json:"metadata"`
}

type extractPage struct {
	PageNumber int     `json:"page_number"`
	Text       string  `json:"text"`
	Width      float64 `json:"width"`
	Height     float64 `json:"height"`
}

type extractMetadata struct {
	PageCount int    `json:"page_count"`
	Title     string `json:"title,omitempty"`
	Author    string `json:"author,omitempty"`
	Subject   string `json:"subject,omitempty"`
	Creator   string `json:"creator,omitempty"`
	Producer  string `json:"producer,omitempty"`
	Version   string `json:"version"`
}

func formatExtractResult(r *extractResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Pages: %d\n", r.Metadata.PageCount)
	if r.Metadata.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", r.Metadata.Title)
	}
	if r.Metadata.Author != "" {
		fmt.Fprintf(&b, "Author: %s\n", r.Metadata.Author)
	}
	b.WriteString("\n")

	for _, p := range r.Pages {
		fmt.Fprintf(&b, "--- Page %d ---\n", p.PageNumber)
		b.WriteString(p.Text)
		b.WriteString("\n\n")
	}

	return strings.TrimSpace(b.String())
}

// --- MCP helpers ---

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}
}

func textResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}
}

// --- Server Builder ---

func buildServer(logger *slog.Logger) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "foliopdf",
		Version: mcpVersion,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "render_html",
		Description: "Generate a PDF from HTML. Response includes base64 PDF data — " +
			"decode and save to make it downloadable. " +
			"Supports Tailwind CSS v3 (bundled), headers/footers with page numbers, " +
			"and Go template variables ({{.key}} with 'data' param).",
	}, handleRenderHTML(logger))

	mcp.AddTool(server, &mcp.Tool{
		Name: "render_markdown",
		Description: "Generate a PDF from Markdown. Response includes base64 PDF data. " +
			"Themes: github (clean, readable), minimal (serif, no decoration), print (formal, justified). " +
			"Supports code blocks with syntax highlighting and Go template variables ({{.key}}).",
	}, handleRenderMarkdown(logger))

	mcp.AddTool(server, &mcp.Tool{
		Name: "manipulate_pdf",
		Description: "Merge, split, or rotate existing PDF files. Response includes base64 PDF data. " +
			"Provide file paths in 'files' or base64 data in 'data'.",
	}, handleManipulatePDF(logger))

	mcp.AddTool(server, &mcp.Tool{
		Name: "extract_pdf",
		Description: "Read text and metadata from a PDF. Provide either a 'file' path or base64 'data'. " +
			"Returns page count, title, author, and the full text of each page.",
	}, handleExtractPDF(logger))

	return server
}
