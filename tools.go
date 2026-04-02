package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
	HTML         string `json:"html" jsonschema:"The HTML to render. Supports full CSS, Tailwind (if enabled), and Go template syntax like {{.name}}."`
	Format       string `json:"format,omitempty" jsonschema:"Page size: A4, Letter, Legal, A3, A5, Tabloid. Default: Letter"`
	MarginTop    string `json:"margin_top,omitempty" jsonschema:"Top margin, e.g. '1in', '20mm', '2cm'. Default: 1in"`
	MarginRight  string `json:"margin_right,omitempty" jsonschema:"Right margin, e.g. '1in', '20mm'. Default: 1in"`
	MarginBottom string `json:"margin_bottom,omitempty" jsonschema:"Bottom margin. Default: 1in"`
	MarginLeft   string `json:"margin_left,omitempty" jsonschema:"Left margin. Default: 1in"`
	HeaderHTML   string `json:"header_html,omitempty" jsonschema:"HTML rendered at the top of every page"`
	FooterHTML   string `json:"footer_html,omitempty" jsonschema:"HTML rendered at the bottom of every page"`
	Tailwind     bool   `json:"tailwind,omitempty" jsonschema:"Set true to enable Tailwind CSS v3 utility classes in the HTML. Tailwind v4 is not supported."`
}

type RenderMarkdownInput struct {
	Markdown       string         `json:"markdown" jsonschema:"The Markdown to render. Supports code blocks with syntax highlighting."`
	Data           map[string]any `json:"data,omitempty" jsonschema:"Variables for template placeholders. Use {{.key}} in markdown, pass values here, e.g. {\"name\": \"Acme\"}"`
	Theme          string         `json:"theme,omitempty" jsonschema:"Document theme: github or elegant. Default: github"`
	SyntaxTheme    string         `json:"syntax_theme,omitempty" jsonschema:"Code block color scheme, e.g. monokai, dracula, github"`
	Format         string         `json:"format,omitempty" jsonschema:"Page size: A4, Letter, Legal, A3, A5, Tabloid. Default: Letter"`
	MarginTop      string         `json:"margin_top,omitempty" jsonschema:"Top margin, e.g. '1in', '20mm'. Default: 1in"`
	MarginRight    string         `json:"margin_right,omitempty" jsonschema:"Right margin. Default: 1in"`
	MarginBottom   string         `json:"margin_bottom,omitempty" jsonschema:"Bottom margin. Default: 1in"`
	MarginLeft     string         `json:"margin_left,omitempty" jsonschema:"Left margin. Default: 1in"`
	Watermark      string         `json:"watermark,omitempty" jsonschema:"Diagonal text overlay, e.g. 'DRAFT' or 'CONFIDENTIAL'"`
	WatermarkScope string         `json:"watermark_scope,omitempty" jsonschema:"Where to show watermark: all-pages (default) or first-page"`
	Tailwind       bool           `json:"tailwind,omitempty" jsonschema:"Set true to enable Tailwind CSS v3 utility classes. Tailwind v4 is not supported."`
}

type RenderTemplateInput struct {
	TemplateID string         `json:"template_id" jsonschema:"The template ID as shown on foliopdf.dev/templates"`
	Data       map[string]any `json:"data" jsonschema:"Values to fill the template. Keys must match the template's {{.key}} placeholders."`
	Format     string         `json:"format,omitempty" jsonschema:"Page size. Default: Letter"`
	MarginTop  string         `json:"margin_top,omitempty" jsonschema:"Top margin override, e.g. '1in'"`
	Watermark  string         `json:"watermark,omitempty" jsonschema:"Diagonal text overlay, e.g. 'DRAFT'"`
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
	Data  string `json:"data,omitempty" jsonschema:"Base64-encoded PDF data. Alternative to file path for remote use."`
	Pages string `json:"pages,omitempty" jsonschema:"Limit extraction to these pages, e.g. '1-5' or '2,4'. 1-indexed."`
}

type ListTemplatesInput struct {
}

// --- API Request Bodies ---

type renderHTMLBody struct {
	HTML    string         `json:"html"`
	Options map[string]any `json:"options,omitempty"`
	Store   bool           `json:"store,omitempty"`
}

type renderMarkdownBody struct {
	Markdown string         `json:"markdown"`
	Data     map[string]any `json:"data,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
	Store    bool           `json:"store,omitempty"`
}

type renderTemplateBody struct {
	TemplateID string         `json:"template_id"`
	Data       map[string]any `json:"data,omitempty"`
	Options    map[string]any `json:"options,omitempty"`
	Store      bool           `json:"store,omitempty"`
}

type manipulateBody struct {
	Operation string           `json:"operation"`
	Inputs    []manipulateItem `json:"inputs"`
	Options   map[string]any   `json:"options,omitempty"`
	Store     bool             `json:"store,omitempty"`
}

type manipulateItem struct {
	Data string `json:"data"`
}

type extractBody struct {
	Data    string         `json:"data"`
	Options map[string]any `json:"options,omitempty"`
}

// storedResponse is returned by the API when store: true is set.
type storedResponse struct {
	Token     string `json:"token"`
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
	SizeBytes int    `json:"size_bytes"`
	Filename  string `json:"filename"`
}

// --- Helpers ---

func buildMargin(top, right, bottom, left string) map[string]string {
	m := make(map[string]string)
	if top != "" {
		m["top"] = top
	}
	if right != "" {
		m["right"] = right
	}
	if bottom != "" {
		m["bottom"] = bottom
	}
	if left != "" {
		m["left"] = left
	}
	return m
}

func buildRenderOptions(format, marginTop, marginRight, marginBottom, marginLeft, headerHTML, footerHTML string, tailwind bool) map[string]any {
	opts := make(map[string]any)
	if format != "" {
		opts["format"] = format
	}
	if m := buildMargin(marginTop, marginRight, marginBottom, marginLeft); len(m) > 0 {
		opts["margin"] = m
	}
	if headerHTML != "" {
		opts["header_html"] = headerHTML
	}
	if footerHTML != "" {
		opts["footer_html"] = footerHTML
	}
	if tailwind {
		opts["tailwind"] = true
	}
	return opts
}

// deliverStoredPDF parses a stored response (from store: true) and returns a download link.
func deliverStoredPDF(respData []byte, label string) *mcp.CallToolResult {
	var stored storedResponse
	if err := json.Unmarshal(respData, &stored); err != nil {
		return errorResult("Failed to parse stored response: " + err.Error())
	}

	if stored.URL == "" {
		return errorResult("API returned a stored response with no download URL.")
	}
	if stored.SizeBytes <= 0 {
		return errorResult("API returned a stored response with invalid size.")
	}

	sizeStr := formatSize(stored.SizeBytes)
	msg := fmt.Sprintf("%s PDF (%s).\nDownload: %s\nExpires: %s (single-use, 3 attempts)",
		label, sizeStr, stored.URL, stored.ExpiresAt)

	return textResult(msg)
}

// pdfOutputDirOverride is set by tests to redirect output to a temp dir.
var pdfOutputDirOverride string

// pdfOutputDir returns the output directory for generated PDFs.
func pdfOutputDir() (string, error) {
	if pdfOutputDirOverride != "" {
		if err := os.MkdirAll(pdfOutputDirOverride, 0755); err != nil {
			return "", err
		}
		return pdfOutputDirOverride, nil
	}
	return os.TempDir(), nil
}

// deliverPDFToFile writes PDF bytes to a temp file and returns both the file
// path (for the user) and an embedded base64 resource (so Claude Desktop can
// present the PDF inline).
func deliverPDFToFile(data []byte, label string) *mcp.CallToolResult {
	sizeStr := formatSize(len(data))

	dir, err := pdfOutputDir()
	if err != nil {
		return errorResult("Failed to create output directory: " + err.Error())
	}

	name := fmt.Sprintf("foliopdf-%d.pdf", time.Now().UnixMilli())
	path := filepath.Join(dir, name)

	if err := os.WriteFile(path, data, 0644); err != nil {
		return errorResult("Failed to write PDF: " + err.Error())
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("%s PDF (%s). Saved to: %s", label, sizeStr, path)},
			&mcp.EmbeddedResource{
				Resource: &mcp.ResourceContents{
					URI:      "file://" + path,
					MIMEType: "application/pdf",
					Blob:     data,
				},
			},
		},
	}
}

func appendRateLimitNote(result *mcp.CallToolResult, rl *rateLimitInfo) {
	if rl == nil {
		return
	}
	if len(result.Content) > 0 {
		if tc, ok := result.Content[0].(*mcp.TextContent); ok {
			tc.Text += fmt.Sprintf("\n[%s]", rl.String())
		}
	}
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

func appendWatermarkNote(result *mcp.CallToolResult) {
	if len(result.Content) > 0 {
		if tc, ok := result.Content[0].(*mcp.TextContent); ok {
			tc.Text += "\n\nNote: This PDF includes a FolioPDF watermark (free mode). " +
				"Remove it by adding your API key — get one at foliopdf.dev/keys, setup guide at foliopdf.dev/setup"
		}
	}
}

func requireAPIKey(apiKey, toolName string) *mcp.CallToolResult {
	if apiKey == "" {
		return errorResult(fmt.Sprintf(
			"%s requires an API key.\n\n"+
				"1. Get a free key at foliopdf.dev/keys\n"+
				"2. Add FOLIOPDF_API_KEY to your MCP server configuration\n"+
				"   Setup guide: foliopdf.dev/setup\n\n"+
				"render_html and render_markdown work without a key (with watermark).",
			toolName,
		))
	}
	return nil
}

func looksLikeFilePath(s string) bool {
	return strings.Contains(s, "/") || strings.Contains(s, "\\") || strings.HasPrefix(s, "~")
}

// validateFilePath checks for path traversal, symlinks, and ensures the file
// is a regular file within safe boundaries.
func validateFilePath(path string) error {
	// Reject any path containing traversal sequences before cleaning
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed: %s", path)
	}

	cleaned := filepath.Clean(path)

	// Require absolute path
	if !filepath.IsAbs(cleaned) {
		return fmt.Errorf("relative paths not allowed, use an absolute path: %s", path)
	}

	// Resolve symlinks to detect escapes
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return fmt.Errorf("cannot resolve path %s: %w", path, err)
	}

	// Verify it's a regular file (not a directory, device, etc.)
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", path)
	}

	// Check file size
	if info.Size() > maxFileSizeBytes {
		return fmt.Errorf("file too large: %s is %s (max %s)",
			path, formatSize(int(info.Size())), formatSize(maxFileSizeBytes))
	}

	return nil
}

func readFileAsBase64(path string) (string, error) {
	if err := validateFilePath(path); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// --- Input Validation ---

var validFormats = map[string]bool{
	"A3": true, "A4": true, "A5": true,
	"Letter": true, "Legal": true, "Tabloid": true,
}

var validThemes = map[string]bool{
	"github": true, "elegant": true,
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

// --- Handler Factories ---

func handleRenderHTML(api *apiClient, apiKey string, mode TransportMode, logger *slog.Logger) mcp.ToolHandlerFor[RenderHTMLInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input RenderHTMLInput) (*mcp.CallToolResult, any, error) {
		ctx = ctxWithClientIP(ctx, req)
		if strings.TrimSpace(input.HTML) == "" {
			return errorResult("HTML content cannot be empty."), nil, nil
		}
		if err := validateFormat(input.Format); err != nil {
			return errorResult(err.Error()), nil, nil
		}

		logger.Info("render_html called", "format", input.Format, "free_mode", apiKey == "", "mode", mode)

		opts := buildRenderOptions(input.Format, input.MarginTop, input.MarginRight, input.MarginBottom, input.MarginLeft, input.HeaderHTML, input.FooterHTML, input.Tailwind)

		hosted := isHostedMode(mode)
		body := renderHTMLBody{
			HTML:    input.HTML,
			Options: opts,
			Store:   hosted,
		}

		path := "/render"
		if apiKey == "" {
			path = "/try"
		}

		resp, err := api.doJSON(ctx, path, apiKey, body)
		if err != nil {
			logger.Error("render_html API call failed", "error", err)
			return errorResult("Failed to call FolioPDF API: " + err.Error()), nil, nil
		}

		if resp.Status >= 400 {
			detail, _ := parseAPIError(resp.Status, resp.Data)
			logger.Warn("render_html API error", "status", resp.Status, "code", detail.Code)
			return formatToolError(detail), nil, nil
		}

		var result *mcp.CallToolResult
		if hosted {
			result = deliverStoredPDF(resp.Data, "Generated")
		} else {
			result = deliverPDFToFile(resp.Data, "Generated")
		}
		if apiKey == "" {
			appendWatermarkNote(result)
		}
		appendRateLimitNote(result, resp.RateLimit)
		return result, nil, nil
	}
}

func handleRenderMarkdown(api *apiClient, apiKey string, mode TransportMode, logger *slog.Logger) mcp.ToolHandlerFor[RenderMarkdownInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input RenderMarkdownInput) (*mcp.CallToolResult, any, error) {
		ctx = ctxWithClientIP(ctx, req)
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

		logger.Info("render_markdown called", "format", input.Format, "theme", input.Theme, "free_mode", apiKey == "")

		opts := make(map[string]any)
		if input.Format != "" {
			opts["format"] = input.Format
		}
		if m := buildMargin(input.MarginTop, input.MarginRight, input.MarginBottom, input.MarginLeft); len(m) > 0 {
			opts["margin"] = m
		}
		if input.Theme != "" {
			opts["theme"] = input.Theme
		}
		if input.SyntaxTheme != "" {
			opts["syntax_theme"] = input.SyntaxTheme
		}
		if input.Watermark != "" {
			opts["watermark"] = input.Watermark
		}
		if input.WatermarkScope != "" {
			opts["watermark_scope"] = input.WatermarkScope
		}
		if input.Tailwind {
			opts["tailwind"] = true
		}

		hosted := isHostedMode(mode)
		body := renderMarkdownBody{
			Markdown: input.Markdown,
			Data:     input.Data,
			Options:  opts,
			Store:    hosted,
		}

		path := "/render/markdown"
		if apiKey == "" {
			path = "/try/markdown"
		}

		resp, err := api.doJSON(ctx, path, apiKey, body)
		if err != nil {
			logger.Error("render_markdown API call failed", "error", err)
			return errorResult("Failed to call FolioPDF API: " + err.Error()), nil, nil
		}

		if resp.Status >= 400 {
			detail, _ := parseAPIError(resp.Status, resp.Data)
			logger.Warn("render_markdown API error", "status", resp.Status, "code", detail.Code)
			return formatToolError(detail), nil, nil
		}

		var result *mcp.CallToolResult
		if hosted {
			result = deliverStoredPDF(resp.Data, "Generated")
		} else {
			result = deliverPDFToFile(resp.Data, "Generated")
		}
		if apiKey == "" {
			appendWatermarkNote(result)
		}
		appendRateLimitNote(result, resp.RateLimit)
		return result, nil, nil
	}
}

func handleRenderTemplate(api *apiClient, apiKey string, mode TransportMode, logger *slog.Logger) mcp.ToolHandlerFor[RenderTemplateInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input RenderTemplateInput) (*mcp.CallToolResult, any, error) {
		ctx = ctxWithClientIP(ctx, req)
		if r := requireAPIKey(apiKey, "Template rendering"); r != nil {
			return r, nil, nil
		}
		if err := validateFormat(input.Format); err != nil {
			return errorResult(err.Error()), nil, nil
		}

		logger.Info("render_template called", "template_id", input.TemplateID)

		opts := make(map[string]any)
		if input.Format != "" {
			opts["format"] = input.Format
		}
		if input.MarginTop != "" {
			opts["margin"] = map[string]string{"top": input.MarginTop}
		}
		if input.Watermark != "" {
			opts["watermark"] = input.Watermark
		}

		hosted := isHostedMode(mode)
		body := renderTemplateBody{
			TemplateID: input.TemplateID,
			Data:       input.Data,
			Options:    opts,
			Store:      hosted,
		}

		resp, err := api.doJSON(ctx, "/render/template", apiKey, body)
		if err != nil {
			logger.Error("render_template API call failed", "error", err)
			return errorResult("Failed to call FolioPDF API: " + err.Error()), nil, nil
		}

		if resp.Status >= 400 {
			detail, _ := parseAPIError(resp.Status, resp.Data)
			logger.Warn("render_template API error", "status", resp.Status, "code", detail.Code)
			return formatToolError(detail), nil, nil
		}

		label := fmt.Sprintf("Generated from template %q", input.TemplateID)
		var result *mcp.CallToolResult
		if hosted {
			result = deliverStoredPDF(resp.Data, label)
		} else {
			result = deliverPDFToFile(resp.Data, label)
		}
		appendRateLimitNote(result, resp.RateLimit)
		return result, nil, nil
	}
}

func handleManipulatePDF(api *apiClient, apiKey string, mode TransportMode, logger *slog.Logger) mcp.ToolHandlerFor[ManipulatePDFInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input ManipulatePDFInput) (*mcp.CallToolResult, any, error) {
		ctx = ctxWithClientIP(ctx, req)
		if r := requireAPIKey(apiKey, "PDF manipulation"); r != nil {
			return r, nil, nil
		}
		if err := validateEnum(input.Operation, "operation", validOperations); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		if err := validatePageRange(input.Pages); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		if input.Degrees != 0 && input.Degrees%90 != 0 {
			return errorResult("Invalid rotation degrees: must be a multiple of 90."), nil, nil
		}

		logger.Info("manipulate_pdf called", "operation", input.Operation, "mode", mode)

		var inputs []manipulateItem

		if isHostedMode(mode) {
			if len(input.Files) > 0 {
				return errorResult("File paths are not supported in hosted mode. Provide base64-encoded PDF data instead."), nil, nil
			}
			if len(input.Data) == 0 {
				return errorResult("No PDF data provided. Supply base64-encoded PDF data in the 'data' field."), nil, nil
			}
			for _, d := range input.Data {
				inputs = append(inputs, manipulateItem{Data: d})
			}
		} else {
			if len(input.Files) == 0 && len(input.Data) == 0 {
				return errorResult("No PDF inputs provided. Supply file paths in 'files' or base64 data in 'data'."), nil, nil
			}
			if len(input.Files) > 0 {
				for _, f := range input.Files {
					encoded, err := readFileAsBase64(f)
					if err != nil {
						return errorResult(err.Error()), nil, nil
					}
					inputs = append(inputs, manipulateItem{Data: encoded})
				}
			} else {
				for _, d := range input.Data {
					inputs = append(inputs, manipulateItem{Data: d})
				}
			}
		}

		opts := make(map[string]any)
		if input.Pages != "" {
			opts["pages"] = input.Pages
		}
		if input.Degrees != 0 {
			opts["degrees"] = input.Degrees
		}

		hosted := isHostedMode(mode)
		body := manipulateBody{
			Operation: input.Operation,
			Inputs:    inputs,
			Options:   opts,
			Store:     hosted,
		}

		resp, err := api.doJSON(ctx, "/manipulate", apiKey, body)
		if err != nil {
			logger.Error("manipulate_pdf API call failed", "error", err)
			return errorResult("Failed to call FolioPDF API: " + err.Error()), nil, nil
		}

		if resp.Status >= 400 {
			detail, _ := parseAPIError(resp.Status, resp.Data)
			logger.Warn("manipulate_pdf API error", "status", resp.Status, "code", detail.Code)
			return formatToolError(detail), nil, nil
		}

		var result *mcp.CallToolResult
		if hosted {
			result = deliverStoredPDF(resp.Data, "Manipulated")
		} else {
			result = deliverPDFToFile(resp.Data, "Manipulated")
		}
		appendRateLimitNote(result, resp.RateLimit)
		return result, nil, nil
	}
}

func handleExtractPDF(api *apiClient, apiKey string, mode TransportMode, logger *slog.Logger) mcp.ToolHandlerFor[ExtractPDFInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input ExtractPDFInput) (*mcp.CallToolResult, any, error) {
		ctx = ctxWithClientIP(ctx, req)
		if r := requireAPIKey(apiKey, "PDF extraction"); r != nil {
			return r, nil, nil
		}
		if err := validatePageRange(input.Pages); err != nil {
			return errorResult(err.Error()), nil, nil
		}

		logger.Info("extract_pdf called", "mode", mode, "has_file", input.File != "", "has_data", input.Data != "")

		var pdfData string

		if isHostedMode(mode) {
			if input.File != "" && looksLikeFilePath(input.File) {
				return errorResult("File paths are not supported in hosted mode. Provide base64-encoded PDF data instead."), nil, nil
			}
			if input.Data == "" {
				return errorResult("No PDF data provided. Supply base64-encoded PDF data in the 'data' field."), nil, nil
			}
			pdfData = input.Data
		} else {
			if input.File != "" {
				encoded, err := readFileAsBase64(input.File)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				pdfData = encoded
			} else if input.Data != "" {
				pdfData = input.Data
			} else {
				return errorResult("No PDF input provided. Supply a file path in 'file' or base64 data in 'data'."), nil, nil
			}
		}

		opts := make(map[string]any)
		if input.Pages != "" {
			opts["pages"] = input.Pages
		}

		body := extractBody{
			Data:    pdfData,
			Options: opts,
		}

		resp, err := api.doJSON(ctx, "/extract", apiKey, body)
		if err != nil {
			logger.Error("extract_pdf API call failed", "error", err)
			return errorResult("Failed to call FolioPDF API: " + err.Error()), nil, nil
		}

		if resp.Status >= 400 {
			detail, _ := parseAPIError(resp.Status, resp.Data)
			logger.Warn("extract_pdf API error", "status", resp.Status, "code", detail.Code)
			return formatToolError(detail), nil, nil
		}

		var result extractResult
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			return errorResult("Failed to parse extract response: " + err.Error()), nil, nil
		}

		output := formatExtractResult(&result)
		tr := textResult(output)
		appendRateLimitNote(tr, resp.RateLimit)
		return tr, nil, nil
	}
}

// --- Extract response types ---

type extractResult struct {
	Pages    []extractPage    `json:"pages"`
	Text     string           `json:"text"`
	Metadata extractMetadata  `json:"metadata"`
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

// --- List Templates ---

type templatesResponse struct {
	Templates []templateInfo `json:"templates"`
	HasMore   bool           `json:"has_more"`
}

type templateInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Variables   []string `json:"variables,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
}

func handleListTemplates(api *apiClient, apiKey string, logger *slog.Logger) mcp.ToolHandlerFor[ListTemplatesInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input ListTemplatesInput) (*mcp.CallToolResult, any, error) {
		ctx = ctxWithClientIP(ctx, req)
		if r := requireAPIKey(apiKey, "Listing templates"); r != nil {
			return r, nil, nil
		}

		logger.Info("list_templates called")

		// GET request — use doJSON with nil body won't work, call the API directly
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, api.baseURL+"/templates", nil)
		if err != nil {
			return errorResult("Failed to create request: " + err.Error()), nil, nil
		}
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		httpReq.Header.Set("User-Agent", userAgent)
		if clientIP := clientIPFromCtx(ctx); clientIP != "" {
			httpReq.Header.Set("X-Forwarded-For", clientIP)
		}

		resp, err := api.httpClient.Do(httpReq)
		if err != nil {
			logger.Error("list_templates API call failed", "error", err)
			return errorResult("Failed to call FolioPDF API: " + err.Error()), nil, nil
		}
		defer resp.Body.Close()

		respData, err := io.ReadAll(resp.Body)
		if err != nil {
			return errorResult("Failed to read response: " + err.Error()), nil, nil
		}

		if resp.StatusCode >= 400 {
			detail, _ := parseAPIError(resp.StatusCode, respData)
			logger.Warn("list_templates API error", "status", resp.StatusCode, "code", detail.Code)
			return formatToolError(detail), nil, nil
		}

		var result templatesResponse
		if err := json.Unmarshal(respData, &result); err != nil {
			return errorResult("Failed to parse templates response: " + err.Error()), nil, nil
		}

		if len(result.Templates) == 0 {
			return textResult("No templates found. Create templates at foliopdf.dev/templates."), nil, nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Found %d template(s):\n\n", len(result.Templates))
		for _, t := range result.Templates {
			fmt.Fprintf(&b, "- %s", t.ID)
			if t.Name != "" {
				fmt.Fprintf(&b, " (%s)", t.Name)
			}
			if t.Description != "" {
				fmt.Fprintf(&b, " — %s", t.Description)
			}
			if len(t.Variables) > 0 {
				fmt.Fprintf(&b, "\n  Variables: %s", strings.Join(t.Variables, ", "))
			}
			b.WriteString("\n")
		}

		return textResult(strings.TrimSpace(b.String())), nil, nil
	}
}

// --- Server Builder ---

func buildServer(api *apiClient, apiKey string, mode TransportMode, logger *slog.Logger) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "foliopdf",
		Version: mcpVersion,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "render_html",
		Description: "Generate a PDF from HTML with full CSS control. " +
			"Supports Tailwind CSS v3 (set tailwind: true), headers/footers, and Go template variables ({{.key}}). " +
			"Works without an API key (adds a watermark).",
	}, handleRenderHTML(api, apiKey, mode, logger))

	mcp.AddTool(server, &mcp.Tool{
		Name: "render_markdown",
		Description: "Generate a styled PDF from Markdown with automatic syntax highlighting. " +
			"Supports Go template variables: use {{.key}} in markdown and pass values in 'data'. " +
			"Works without an API key (adds a watermark).",
	}, handleRenderMarkdown(api, apiKey, mode, logger))

	mcp.AddTool(server, &mcp.Tool{
		Name: "render_template",
		Description: "Generate a PDF from a saved template with data variables. " +
			"Best for recurring documents (invoices, contracts) where layout is fixed and only data changes.",
	}, handleRenderTemplate(api, apiKey, mode, logger))

	mcp.AddTool(server, &mcp.Tool{
		Name: "manipulate_pdf",
		Description: "Merge, split, or rotate existing PDF files.",
	}, handleManipulatePDF(api, apiKey, mode, logger))

	mcp.AddTool(server, &mcp.Tool{
		Name: "extract_pdf",
		Description: "Read text and metadata from a PDF. Provide either a 'file' path or base64 'data'. " +
			"Returns page count, title, author, and the full text of each page.",
	}, handleExtractPDF(api, apiKey, mode, logger))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_templates",
		Description: "List available PDF templates and their placeholder variables. Use before render_template to see what templates exist.",
	}, handleListTemplates(api, apiKey, logger))

	return server
}
