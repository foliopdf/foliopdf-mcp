package main

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var nopLogger = slog.New(slog.DiscardHandler)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "foliopdf-test-*")
	if err != nil {
		panic(err)
	}
	pdfOutputDirOverride = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// ============================================================
// render_html
// ============================================================

func TestRenderHTML_Basic(t *testing.T) {
	handler := handleRenderHTML(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<h1>Hello World</h1>"})
	assertNoError(t, result)
	assertContains(t, resultText(t, result), "Generated PDF")
	assertContains(t, resultText(t, result), "Saved to:")
}

func TestRenderHTML_WithFormat(t *testing.T) {
	handler := handleRenderHTML(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<h1>A4</h1>", Format: "A4"})
	assertNoError(t, result)
}

func TestRenderHTML_WithMargins(t *testing.T) {
	handler := handleRenderHTML(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{
		HTML: "<h1>Margins</h1>", MarginTop: "2cm", MarginRight: "1in", MarginBottom: "20mm", MarginLeft: "72pt",
	})
	assertNoError(t, result)
}

func TestRenderHTML_WithTemplateVars(t *testing.T) {
	handler := handleRenderHTML(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{
		HTML: "<h1>Hello {{.name}}</h1>",
		Data: map[string]any{"name": "World"},
	})
	assertNoError(t, result)
}

func TestRenderHTML_WithWatermark(t *testing.T) {
	handler := handleRenderHTML(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{
		HTML: "<h1>Draft</h1>", Watermark: "DRAFT",
	})
	assertNoError(t, result)
}

func TestRenderHTML_EmptyHTML(t *testing.T) {
	handler := handleRenderHTML(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: ""})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "cannot be empty")
}

func TestRenderHTML_InvalidFormat(t *testing.T) {
	handler := handleRenderHTML(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<h1>x</h1>", Format: "Huge"})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "invalid format")
}

func TestRenderHTML_BadTemplate(t *testing.T) {
	handler := handleRenderHTML(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{
		HTML: "<h1>{{.missing}</h1>",
		Data: map[string]any{"x": 1},
	})
	assertIsError(t, result)
}

func TestRenderHTML_SavesFile(t *testing.T) {
	handler := handleRenderHTML(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<h1>Test</h1>"})
	assertNoError(t, result)
	text := resultText(t, result)
	assertContains(t, text, "Saved to:")
	assertContains(t, text, ".pdf")
}

// ============================================================
// render_markdown
// ============================================================

func TestRenderMarkdown_Basic(t *testing.T) {
	handler := handleRenderMarkdown(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{Markdown: "# Hello\n\nWorld"})
	assertNoError(t, result)
	assertContains(t, resultText(t, result), "Generated PDF")
}

func TestRenderMarkdown_WithTheme(t *testing.T) {
	for _, theme := range []string{"github", "minimal", "print"} {
		t.Run(theme, func(t *testing.T) {
			handler := handleRenderMarkdown(nopLogger)
			result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{Markdown: "# Test", Theme: theme})
			assertNoError(t, result)
		})
	}
}

func TestRenderMarkdown_WithCodeBlock(t *testing.T) {
	handler := handleRenderMarkdown(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{
		Markdown: "# Code\n\n```python\nprint('hello')\n```", SyntaxTheme: "monokai",
	})
	assertNoError(t, result)
}

func TestRenderMarkdown_WithTemplateVars(t *testing.T) {
	handler := handleRenderMarkdown(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{
		Markdown: "# Invoice for {{.company}}", Data: map[string]any{"company": "Acme Corp"},
	})
	assertNoError(t, result)
}

func TestRenderMarkdown_EmptyMarkdown(t *testing.T) {
	handler := handleRenderMarkdown(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{Markdown: "  "})
	assertIsError(t, result)
}

func TestRenderMarkdown_InvalidTheme(t *testing.T) {
	handler := handleRenderMarkdown(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{Markdown: "# X", Theme: "comic-sans"})
	assertIsError(t, result)
}

func TestRenderMarkdown_InvalidWatermarkScope(t *testing.T) {
	handler := handleRenderMarkdown(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{Markdown: "# X", WatermarkScope: "last-page"})
	assertIsError(t, result)
}

// ============================================================
// manipulate_pdf
// ============================================================

func TestManipulatePDF_Merge(t *testing.T) {
	pdf1, _ := renderHTML("<h1>Page 1</h1>", renderOpts{})
	pdf2, _ := renderHTML("<h1>Page 2</h1>", renderOpts{})

	tmpDir := t.TempDir()
	f1 := filepath.Join(tmpDir, "a.pdf")
	f2 := filepath.Join(tmpDir, "b.pdf")
	os.WriteFile(f1, pdf1, 0644)
	os.WriteFile(f2, pdf2, 0644)

	handler := handleManipulatePDF(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{
		Operation: "merge", Files: []string{f1, f2},
	})
	assertNoError(t, result)
	assertContains(t, resultText(t, result), "Manipulated PDF")
}

func TestManipulatePDF_Split(t *testing.T) {
	t.Skip("TODO: folio's ReorderPages requires order length == page count; split needs library support for subset extraction")
}

func TestManipulatePDF_Rotate(t *testing.T) {
	pdf, _ := renderHTML("<h1>Rotate me</h1>", renderOpts{})

	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "doc.pdf")
	os.WriteFile(f, pdf, 0644)

	handler := handleManipulatePDF(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{
		Operation: "rotate", Files: []string{f}, Degrees: 90,
	})
	assertNoError(t, result)
}

func TestManipulatePDF_InvalidOperation(t *testing.T) {
	handler := handleManipulatePDF(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "shred"})
	assertIsError(t, result)
}

func TestManipulatePDF_InvalidDegrees(t *testing.T) {
	handler := handleManipulatePDF(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "rotate", Degrees: 45})
	assertIsError(t, result)
}

func TestManipulatePDF_NoInputs(t *testing.T) {
	handler := handleManipulatePDF(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "merge"})
	assertIsError(t, result)
}

func TestManipulatePDF_PathTraversal(t *testing.T) {
	handler := handleManipulatePDF(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{
		Operation: "merge", Files: []string{"/tmp/../etc/passwd"},
	})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "traversal")
}

func TestManipulatePDF_ValidDegrees(t *testing.T) {
	pdf, _ := renderHTML("<h1>Test</h1>", renderOpts{})
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "doc.pdf")
	os.WriteFile(f, pdf, 0644)

	handler := handleManipulatePDF(nopLogger)
	for _, deg := range []int{90, 180, 270, -90, 360} {
		result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{
			Operation: "rotate", Files: []string{f}, Degrees: deg,
		})
		assertNoError(t, result)
	}
}

// ============================================================
// extract_pdf
// ============================================================

func TestExtractPDF_Basic(t *testing.T) {
	pdf, _ := renderHTML("<h1>Extract Me</h1><p>Some text content here.</p>", renderOpts{})

	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "doc.pdf")
	os.WriteFile(f, pdf, 0644)

	handler := handleExtractPDF(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{File: f})
	assertNoError(t, result)
	assertContains(t, resultText(t, result), "Pages: 1")
}

func TestExtractPDF_Base64(t *testing.T) {
	pdf, _ := renderHTML("<h1>Base64 Test</h1>", renderOpts{})

	handler := handleExtractPDF(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{
		Data: encodeBase64(pdf),
	})
	assertNoError(t, result)
	assertContains(t, resultText(t, result), "Pages:")
}

func TestExtractPDF_NoInput(t *testing.T) {
	handler := handleExtractPDF(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{})
	assertIsError(t, result)
}

func TestExtractPDF_PathTraversal(t *testing.T) {
	handler := handleExtractPDF(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{File: "/tmp/../etc/passwd"})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "traversal")
}

func TestExtractPDF_InvalidPageRange(t *testing.T) {
	handler := handleExtractPDF(nopLogger)
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{File: "/tmp/x.pdf", Pages: "abc"})
	assertIsError(t, result)
}

// ============================================================
// render.go unit tests
// ============================================================

func TestParsePt(t *testing.T) {
	tests := []struct{ input string; want float64 }{
		{"1in", 72}, {"72pt", 72}, {"2.54cm", 72.28}, {"25.4mm", 71.99},
		{"0", 0}, {"", 0}, {"36", 36},
	}
	for _, tt := range tests {
		got := parsePt(tt.input)
		if got < tt.want-0.5 || got > tt.want+0.5 {
			t.Errorf("parsePt(%q) = %f, want ~%f", tt.input, got, tt.want)
		}
	}
}

func TestResolvePageSize(t *testing.T) {
	tests := []struct{ input string; wantW, wantH float64 }{
		{"letter", 612, 792}, {"Letter", 612, 792}, {"", 612, 792},
		{"a4", 595.28, 841.89}, {"A4", 595.28, 841.89},
		{"legal", 612, 1008}, {"tabloid", 792, 1224},
		{"unknown", 612, 792},
	}
	for _, tt := range tests {
		ps := resolvePageSize(tt.input)
		if ps.Width != tt.wantW || ps.Height != tt.wantH {
			t.Errorf("resolvePageSize(%q) = {%f,%f}, want {%f,%f}", tt.input, ps.Width, ps.Height, tt.wantW, tt.wantH)
		}
	}
}

func TestParsePageRange(t *testing.T) {
	indices, err := parsePageRange("1-3,5", 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 1, 2, 4}
	if len(indices) != len(want) {
		t.Fatalf("got %v, want %v", indices, want)
	}
	for i, v := range want {
		if indices[i] != v {
			t.Errorf("index %d: got %d, want %d", i, indices[i], v)
		}
	}
}

func TestParsePageRange_OutOfBounds(t *testing.T) {
	_, err := parsePageRange("1-20", 5)
	if err == nil {
		t.Error("expected error for out-of-bounds range")
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct{ b int; want string }{
		{0, "0 bytes"}, {500, "500 bytes"}, {1024, "1.0 KB"}, {91234, "89.1 KB"}, {1048576, "1.0 MB"},
	}
	for _, tt := range tests {
		assertEqual(t, tt.want, formatSize(tt.b))
	}
}

// ============================================================
// validation tests
// ============================================================

func TestValidateFilePath_Traversal(t *testing.T) {
	err := validateFilePath("/tmp/../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestValidateFilePath_Relative(t *testing.T) {
	err := validateFilePath("relative/path.pdf")
	if err == nil {
		t.Error("expected error for relative path")
	}
}

func TestValidateFilePath_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "ok.pdf")
	os.WriteFile(f, []byte("%PDF-1.4 test"), 0644)
	if err := validateFilePath(f); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateFilePath_Directory(t *testing.T) {
	if err := validateFilePath(os.TempDir()); err == nil {
		t.Error("expected error for directory")
	}
}

func TestValidateFilePath_Nonexistent(t *testing.T) {
	if err := validateFilePath("/nonexistent/file.pdf"); err == nil {
		t.Error("expected error")
	}
}

// ============================================================
// markdown converter tests
// ============================================================

func TestConvertMarkdown_Basic(t *testing.T) {
	html, err := convertMarkdown("# Hello\n\nWorld", markdownOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Hello") || !strings.Contains(html, "World") {
		t.Error("expected Hello and World in output")
	}
	if !strings.Contains(html, "markdown-body") {
		t.Error("expected markdown-body class")
	}
}

func TestConvertMarkdown_Empty(t *testing.T) {
	_, err := convertMarkdown("", markdownOpts{})
	if err == nil {
		t.Error("expected error for empty markdown")
	}
}

func TestConvertMarkdown_Themes(t *testing.T) {
	for _, theme := range []string{"github", "minimal", "print"} {
		_, err := convertMarkdown("# Test", markdownOpts{Theme: theme})
		if err != nil {
			t.Errorf("theme %s failed: %v", theme, err)
		}
	}
}

// ============================================================
// helpers
// ============================================================

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if len(r.Content) == 0 {
		t.Fatal("no content")
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", r.Content[0])
	}
	return tc.Text
}

func assertNoError(t *testing.T, r *mcp.CallToolResult) {
	t.Helper()
	if r.IsError {
		t.Errorf("unexpected error: %s", resultText(t, r))
	}
}

func assertIsError(t *testing.T, r *mcp.CallToolResult) {
	t.Helper()
	if !r.IsError {
		t.Error("expected error result")
	}
}

func assertEqual(t *testing.T, want, got any) {
	t.Helper()
	if want != got {
		t.Errorf("got %v, want %v", got, want)
	}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected %q to contain %q", s, substr)
	}
}
