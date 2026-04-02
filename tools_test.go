package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var fakePDF = []byte("%PDF-1.4 fake pdf content for testing")

var nopLogger = slog.New(slog.DiscardHandler)

func TestMain(m *testing.M) {
	// Redirect PDF output to a temp dir so tests don't write to ~/Documents.
	dir, err := os.MkdirTemp("", "foliopdf-test-*")
	if err != nil {
		panic(err)
	}
	pdfOutputDirOverride = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func testAPI(t *testing.T, handler http.HandlerFunc) *apiClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &apiClient{
		baseURL:    srv.URL,
		httpClient: srv.Client(),
	}
}

// ============================================================
// render_html tests
// ============================================================

func TestRenderHTML_FreeMode(t *testing.T) {
	var gotPath, gotAuth string
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Write(fakePDF)
	})

	result, _, _ := handleRenderHTML(api, "", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<h1>Test</h1>"})

	assertNoError(t, result)
	assertEqual(t, "/try", gotPath)
	assertEqual(t, "", gotAuth)
	assertContains(t, resultText(t, result), "Saved to:")
	assertContains(t, resultText(t, result), "watermark")
}

func TestRenderHTML_WithAPIKey(t *testing.T) {
	var gotPath, gotAuth string
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Write(fakePDF)
	})

	result, _, _ := handleRenderHTML(api, "sk_live_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<h1>Test</h1>", Format: "A4"})

	assertNoError(t, result)
	assertEqual(t, "/render", gotPath)
	assertEqual(t, "Bearer sk_live_test", gotAuth)
	assertNotContains(t, resultText(t, result), "watermark")
}

func TestRenderHTML_RequestBody(t *testing.T) {
	var body renderHTMLBody
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write(fakePDF)
	})

	handleRenderHTML(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{
		HTML: "<h1>Test</h1>", Format: "Letter", MarginTop: "1in", MarginLeft: "2cm", Tailwind: true,
	})

	assertEqual(t, "<h1>Test</h1>", body.HTML)
	assertEqual(t, "Letter", body.Options["format"])
	margin := body.Options["margin"].(map[string]any)
	assertEqual(t, "1in", margin["top"])
}

func TestRenderHTML_RequestBody_AllMargins(t *testing.T) {
	var body map[string]any
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write(fakePDF)
	})

	handleRenderHTML(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{
		HTML: "<h1>Test</h1>", MarginTop: "25mm", MarginRight: "20mm", MarginBottom: "25mm", MarginLeft: "20mm",
		HeaderHTML: "<div>Header</div>", FooterHTML: "<div>Footer</div>",
	})

	opts := body["options"].(map[string]any)
	margin := opts["margin"].(map[string]any)
	assertEqual(t, "25mm", margin["top"])
	assertEqual(t, "20mm", margin["right"])
	assertEqual(t, "25mm", margin["bottom"])
	assertEqual(t, "20mm", margin["left"])
	assertEqual(t, "<div>Header</div>", opts["header_html"])
	assertEqual(t, "<div>Footer</div>", opts["footer_html"])
}

func TestRenderHTML_EmptyHTML(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call API with empty HTML")
	})
	result, _, _ := handleRenderHTML(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: ""})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "cannot be empty")
}

func TestRenderHTML_InvalidFormat(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call API with invalid format")
	})
	result, _, _ := handleRenderHTML(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<h1>x</h1>", Format: "Huge"})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "invalid format")
}

// ============================================================
// render_markdown tests
// ============================================================

func TestRenderMarkdown_FreeMode(t *testing.T) {
	var gotPath string
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write(fakePDF)
	})

	result, _, _ := handleRenderMarkdown(api, "", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{Markdown: "# Hello"})

	assertNoError(t, result)
	assertEqual(t, "/try/markdown", gotPath)
	assertContains(t, resultText(t, result), "watermark")
}

func TestRenderMarkdown_WithAPIKey(t *testing.T) {
	var gotPath string
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write(fakePDF)
	})

	result, _, _ := handleRenderMarkdown(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{Markdown: "# Hello", Theme: "github"})

	assertNoError(t, result)
	assertEqual(t, "/render/markdown", gotPath)
}

func TestRenderMarkdown_RequestBody_AllFields(t *testing.T) {
	var body map[string]any
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write(fakePDF)
	})

	handleRenderMarkdown(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{
		Markdown: "# Report\n{{summary}}", Data: map[string]any{"summary": "Q1"},
		Theme: "elegant", SyntaxTheme: "dracula", Format: "Letter",
		MarginTop: "2cm", MarginRight: "1.5cm", MarginBottom: "2cm", MarginLeft: "1.5cm",
		Watermark: "DRAFT", WatermarkScope: "first-page", Tailwind: true,
	})

	opts := body["options"].(map[string]any)
	assertEqual(t, "elegant", opts["theme"])
	assertEqual(t, "dracula", opts["syntax_theme"])
	assertEqual(t, "DRAFT", opts["watermark"])
	assertEqual(t, "first-page", opts["watermark_scope"])
	margin := opts["margin"].(map[string]any)
	assertEqual(t, "2cm", margin["top"])
}

func TestRenderMarkdown_EmptyMarkdown(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleRenderMarkdown(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{Markdown: "  "})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "cannot be empty")
}

func TestRenderMarkdown_InvalidTheme(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleRenderMarkdown(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{Markdown: "# X", Theme: "comic-sans"})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "invalid theme")
}


func TestRenderMarkdown_InvalidWatermarkScope(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleRenderMarkdown(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{Markdown: "# X", WatermarkScope: "last-page"})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "invalid watermark_scope")
}

// ============================================================
// render_template tests
// ============================================================

func TestRenderTemplate_NoAPIKey(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleRenderTemplate(api, "", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderTemplateInput{TemplateID: "invoice", Data: map[string]any{"x": 1}})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "API key")
}

func TestRenderTemplate_WithAPIKey(t *testing.T) {
	var gotPath string
	var body renderTemplateBody
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&body)
		w.Write(fakePDF)
	})

	result, _, _ := handleRenderTemplate(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderTemplateInput{TemplateID: "invoice", Data: map[string]any{"client": "Acme"}})
	assertNoError(t, result)
	assertEqual(t, "/render/template", gotPath)
	assertEqual(t, "invoice", body.TemplateID)
}

// ============================================================
// manipulate_pdf tests
// ============================================================

func TestManipulatePDF_StdioMode_Files(t *testing.T) {
	tmpDir := t.TempDir()
	f1, f2 := filepath.Join(tmpDir, "a.pdf"), filepath.Join(tmpDir, "b.pdf")
	os.WriteFile(f1, fakePDF, 0644)
	os.WriteFile(f2, fakePDF, 0644)

	var body manipulateBody
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write(fakePDF)
	})

	result, _, _ := handleManipulatePDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "merge", Files: []string{f1, f2}})

	assertNoError(t, result)
	assertEqual(t, "merge", body.Operation)
	if len(body.Inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(body.Inputs))
	}
	decoded, _ := base64.StdEncoding.DecodeString(body.Inputs[0].Data)
	assertEqual(t, string(fakePDF), string(decoded))
}

func TestManipulatePDF_HostedMode_RejectsFiles(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleManipulatePDF(api, "sk_test", ModeSSE, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "merge", Files: []string{"/tmp/a.pdf"}})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "hosted mode")
}

func TestManipulatePDF_NoAPIKey(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleManipulatePDF(api, "", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "merge", Files: []string{"/tmp/a.pdf"}})
	assertIsError(t, result)
}

func TestManipulatePDF_SplitWithPages(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "doc.pdf")
	os.WriteFile(f, fakePDF, 0644)
	var body map[string]any
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write(fakePDF)
	})

	handleManipulatePDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "split", Files: []string{f}, Pages: "1-3,5"})
	opts := body["options"].(map[string]any)
	assertEqual(t, "1-3,5", opts["pages"])
}

func TestManipulatePDF_RotateWithDegrees(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "doc.pdf")
	os.WriteFile(f, fakePDF, 0644)
	var body map[string]any
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write(fakePDF)
	})

	handleManipulatePDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "rotate", Files: []string{f}, Degrees: 90})
	opts := body["options"].(map[string]any)
	assertEqual(t, float64(90), opts["degrees"])
}

func TestManipulatePDF_NoInputs(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleManipulatePDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "merge"})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "No PDF inputs")
}

func TestManipulatePDF_HostedMode_NoData(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleManipulatePDF(api, "sk_test", ModeSSE, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "merge"})
	assertIsError(t, result)
}

func TestManipulatePDF_Base64Fallback(t *testing.T) {
	var body map[string]any
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write(fakePDF)
	})
	result, _, _ := handleManipulatePDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "merge", Data: []string{"b64a", "b64b"}})
	assertNoError(t, result)
	inputs := body["inputs"].([]any)
	if len(inputs) != 2 {
		t.Errorf("expected 2 inputs, got %d", len(inputs))
	}
}

func TestManipulatePDF_FileNotFound(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleManipulatePDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "merge", Files: []string{"/nonexistent/file.pdf"}})
	assertIsError(t, result)
}

func TestManipulatePDF_InvalidOperation(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleManipulatePDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "shred"})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "invalid operation")
}

func TestManipulatePDF_InvalidDegrees(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleManipulatePDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "rotate", Degrees: 45})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "multiple of 90")
}

func TestManipulatePDF_ValidDegrees(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "doc.pdf")
	os.WriteFile(f, fakePDF, 0644)
	var body map[string]any
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write(fakePDF)
	})

	// -90 and 360 should be accepted (any multiple of 90)
	for _, deg := range []int{90, 180, 270, -90, 360} {
		result, _, _ := handleManipulatePDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "rotate", Files: []string{f}, Degrees: deg})
		assertNoError(t, result)
	}
}

func TestManipulatePDF_InvalidPageRange(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleManipulatePDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "split", Pages: "abc"})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "invalid page range")
}

func TestManipulatePDF_PathTraversal(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleManipulatePDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{
		Operation: "merge", Files: []string{"/tmp/../etc/passwd"},
	})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "traversal")
}

// ============================================================
// extract_pdf tests
// ============================================================

func TestExtractPDF_StdioMode(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "test.pdf")
	os.WriteFile(f, fakePDF, 0644)

	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"Hello","metadata":{"page_count":1,"title":"Test Doc","author":"Author"},"pages":[{"page_number":1,"text":"Hello"}]}`))
	})

	result, _, _ := handleExtractPDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{File: f})

	assertNoError(t, result)
	assertContains(t, resultText(t, result), "Pages: 1")
	assertContains(t, resultText(t, result), "Title: Test Doc")
	assertContains(t, resultText(t, result), "Hello")
}

func TestExtractPDF_NoAPIKey(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleExtractPDF(api, "", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{File: "/tmp/test.pdf"})
	assertIsError(t, result)
}

func TestExtractPDF_HostedMode_RejectsFile(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleExtractPDF(api, "sk_test", ModeSSE, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{File: "/tmp/test.pdf"})
	assertIsError(t, result)
}

func TestExtractPDF_HostedMode_Base64Data(t *testing.T) {
	var body map[string]any
	b64 := base64.StdEncoding.EncodeToString(fakePDF)
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"text":"X","metadata":{"page_count":2},"pages":[{"page_number":1,"text":"P1"},{"page_number":2,"text":"P2"}]}`))
	})

	result, _, _ := handleExtractPDF(api, "sk_test", ModeSSE, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{Data: b64})
	assertNoError(t, result)
	assertEqual(t, b64, body["data"])
	assertContains(t, resultText(t, result), "Pages: 2")
}

func TestExtractPDF_NoInput(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleExtractPDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "No PDF input")
}

func TestExtractPDF_FileNotFound(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleExtractPDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{File: "/nonexistent/file.pdf"})
	assertIsError(t, result)
}

func TestExtractPDF_WithPages(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "t.pdf")
	os.WriteFile(f, fakePDF, 0644)
	var body map[string]any
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"text":"","metadata":{"page_count":1},"pages":[]}`))
	})

	handleExtractPDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{File: f, Pages: "1-3"})
	opts := body["options"].(map[string]any)
	assertEqual(t, "1-3", opts["pages"])
}

func TestExtractPDF_HostedMode_NoData(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleExtractPDF(api, "sk_test", ModeSSE, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{})
	assertIsError(t, result)
}

func TestExtractPDF_InvalidPageRange(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleExtractPDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{File: "/tmp/x.pdf", Pages: "abc"})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "invalid page range")
}

func TestExtractPDF_PathTraversal(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not call API") })
	result, _, _ := handleExtractPDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ExtractPDFInput{File: "/tmp/../../etc/passwd"})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "traversal")
}

// ============================================================
// API error tests
// ============================================================

func TestRenderHTML_APIError_RateLimited(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "rate_limited", "message": "Trial rate limit: 5 renders per minute."}})
	})
	result, _, _ := handleRenderHTML(api, "", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<h1>Test</h1>"})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "rate limit")
}

func TestRenderHTML_APIError_QuotaExceeded(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "quota_exceeded", "message": "Exceeded."}})
	})
	result, _, _ := handleRenderHTML(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<h1>Test</h1>"})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "render limit")
}

func TestRenderHTML_APIError_BadRequest(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "invalid_request", "message": "HTML is required."}})
	})
	result, _, _ := handleRenderHTML(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<p>x</p>"})
	assertIsError(t, result)
	assertContains(t, resultText(t, result), "HTML is required")
}

// ============================================================
// Hosted mode store tests
// ============================================================

func storedHandler(t *testing.T, gotBody *map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"token": "tok_abc", "url": "https://api.foliopdf.dev/downloads/tok_abc",
			"expires_at": "2026-04-01T22:15:00Z", "size_bytes": 91234, "filename": "doc.pdf",
		})
	}
}

func TestRenderHTML_HostedMode_StoredDownload(t *testing.T) {
	var body map[string]any
	api := testAPI(t, storedHandler(t, &body))
	result, _, _ := handleRenderHTML(api, "sk_test", ModeSSE, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<h1>Test</h1>"})
	assertEqual(t, true, body["store"])
	assertNoError(t, result)
	assertContains(t, resultText(t, result), "downloads/tok_abc")
	assertContains(t, resultText(t, result), "single-use")
}

func TestRenderHTML_StdioMode_NoStore(t *testing.T) {
	var body map[string]any
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write(fakePDF)
	})
	handleRenderHTML(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<h1>Test</h1>"})
	if body["store"] == true {
		t.Error("store should not be true in stdio mode")
	}
}

func TestRenderMarkdown_HostedMode_StoredDownload(t *testing.T) {
	var body map[string]any
	api := testAPI(t, storedHandler(t, &body))
	result, _, _ := handleRenderMarkdown(api, "sk_test", ModeSSE, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{Markdown: "# Test"})
	assertEqual(t, true, body["store"])
	assertNoError(t, result)
}

func TestRenderMarkdown_StdioMode_NoStore(t *testing.T) {
	var body map[string]any
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write(fakePDF)
	})
	handleRenderMarkdown(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderMarkdownInput{Markdown: "# Test"})
	if body["store"] == true {
		t.Error("store should not be true in stdio mode")
	}
}

func TestRenderTemplate_HostedMode_StoredDownload(t *testing.T) {
	var body map[string]any
	api := testAPI(t, storedHandler(t, &body))
	result, _, _ := handleRenderTemplate(api, "sk_test", ModeSSE, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderTemplateInput{TemplateID: "inv", Data: map[string]any{"x": 1}})
	assertEqual(t, true, body["store"])
	assertNoError(t, result)
}

func TestRenderTemplate_StdioMode_NoStore(t *testing.T) {
	var body map[string]any
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write(fakePDF)
	})
	handleRenderTemplate(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, RenderTemplateInput{TemplateID: "inv", Data: map[string]any{"x": 1}})
	if body["store"] == true {
		t.Error("store should not be true in stdio mode")
	}
}

func TestManipulatePDF_HostedMode_StoredDownload(t *testing.T) {
	var body map[string]any
	api := testAPI(t, storedHandler(t, &body))
	result, _, _ := handleManipulatePDF(api, "sk_test", ModeSSE, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "merge", Data: []string{"b64a", "b64b"}})
	assertEqual(t, true, body["store"])
	assertNoError(t, result)
}

func TestManipulatePDF_StdioMode_NoStore(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "a.pdf")
	os.WriteFile(f, fakePDF, 0644)
	var body map[string]any
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write(fakePDF)
	})
	handleManipulatePDF(api, "sk_test", ModeStdio, nopLogger)(context.Background(), &mcp.CallToolRequest{}, ManipulatePDFInput{Operation: "merge", Files: []string{f}})
	if body["store"] == true {
		t.Error("store should not be true in stdio mode")
	}
}

// ============================================================
// transport.go tests
// ============================================================

func TestDetectMode(t *testing.T) {
	for _, tt := range []struct{ env string; want TransportMode }{
		{"", ModeStdio}, {"sse", ModeSSE}, {"SSE", ModeSSE},
		{"streamable", ModeStreamableHTTP}, {"Streamable", ModeStreamableHTTP}, {"unknown", ModeStdio},
	} {
		t.Run("MCP_TRANSPORT="+tt.env, func(t *testing.T) {
			t.Setenv("MCP_TRANSPORT", tt.env)
			assertEqual(t, tt.want, detectMode())
		})
	}
}

func TestExtractBearerToken_Header(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer sk_live_abc")
	assertEqual(t, "sk_live_abc", extractBearerToken(r))
}

func TestExtractBearerToken_QueryParam(t *testing.T) {
	r := httptest.NewRequest("GET", "/?api_key=sk_query", nil)
	assertEqual(t, "sk_query", extractBearerToken(r))
}

func TestExtractBearerToken_HeaderPrecedence(t *testing.T) {
	r := httptest.NewRequest("GET", "/?api_key=sk_query", nil)
	r.Header.Set("Authorization", "Bearer sk_header")
	assertEqual(t, "sk_header", extractBearerToken(r))
}

func TestExtractBearerToken_Empty(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	assertEqual(t, "", extractBearerToken(r))
}

func TestExtractBearerToken_NonBearer(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	assertEqual(t, "", extractBearerToken(r))
}

func TestIsHostedMode(t *testing.T) {
	if isHostedMode(ModeStdio) {
		t.Error("stdio should not be hosted")
	}
	if !isHostedMode(ModeSSE) {
		t.Error("SSE should be hosted")
	}
	if !isHostedMode(ModeStreamableHTTP) {
		t.Error("StreamableHTTP should be hosted")
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("TEST_SET", "val")
	assertEqual(t, "val", envOr("TEST_SET", "fb"))
	assertEqual(t, "fb", envOr("TEST_UNSET_XYZ", "fb"))
}

// ============================================================
// Client IP forwarding
// ============================================================

func TestClientIP_ForwardedToAPI(t *testing.T) {
	var gotXFF string
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		gotXFF = r.Header.Get("X-Forwarded-For")
		w.Write(fakePDF)
	})

	// Simulate hosted mode: inject client IP into context (as middleware would)
	ctx := context.WithValue(context.Background(), clientIPKey, "203.0.113.42")
	handler := handleRenderHTML(api, "sk_test", ModeSSE, nopLogger)
	handler(ctx, &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<h1>Test</h1>"})

	assertEqual(t, "203.0.113.42", gotXFF)
}

func TestClientIP_NotForwardedInStdio(t *testing.T) {
	var gotXFF string
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		gotXFF = r.Header.Get("X-Forwarded-For")
		w.Write(fakePDF)
	})

	// Stdio mode: no client IP in context
	handler := handleRenderHTML(api, "sk_test", ModeStdio, nopLogger)
	handler(context.Background(), &mcp.CallToolRequest{}, RenderHTMLInput{HTML: "<h1>Test</h1>"})

	assertEqual(t, "", gotXFF)
}

func TestClientIP_FromReqExtraHeader(t *testing.T) {
	// Fallback: resolve from req.Extra.Header when context has no IP
	req := &mcp.CallToolRequest{}
	req.Extra = &mcp.RequestExtra{
		Header: http.Header{"X-Forwarded-For": []string{"198.51.100.1, 10.0.0.1"}},
	}
	ip := resolveClientIP(context.Background(), req)
	assertEqual(t, "198.51.100.1", ip)
}

func TestClientIP_ContextTakesPrecedence(t *testing.T) {
	ctx := context.WithValue(context.Background(), clientIPKey, "203.0.113.1")
	req := &mcp.CallToolRequest{}
	req.Extra = &mcp.RequestExtra{
		Header: http.Header{"X-Forwarded-For": []string{"198.51.100.99"}},
	}
	ip := resolveClientIP(ctx, req)
	assertEqual(t, "203.0.113.1", ip)
}

func TestClientIP_NilExtra(t *testing.T) {
	ip := resolveClientIP(context.Background(), &mcp.CallToolRequest{})
	assertEqual(t, "", ip)
}

func TestExtractClientIP_RemoteAddr(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.1:12345"
	assertEqual(t, "192.0.2.1", extractClientIP(r))
}

func TestExtractClientIP_XForwardedFor(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.1")
	assertEqual(t, "203.0.113.50", extractClientIP(r))
}

func TestExtractClientIP_XRealIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Real-Ip", "198.51.100.7")
	assertEqual(t, "198.51.100.7", extractClientIP(r))
}

// ============================================================
// api.go tests
// ============================================================

func TestAPIClient_RetryOn5xx(t *testing.T) {
	attempts := 0
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(502)
			w.Write([]byte("bad"))
			return
		}
		w.Write(fakePDF)
	})
	resp, err := api.doJSON(context.Background(), "/render", "sk_test", map[string]any{"html": "x"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 2, attempts)
	assertEqual(t, 200, resp.Status)
	assertEqual(t, string(fakePDF), string(resp.Data))
}

func TestAPIClient_NoRetryOn4xx(t *testing.T) {
	attempts := 0
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"code":"bad"}}`))
	})
	resp, _ := api.doJSON(context.Background(), "/render", "sk_test", map[string]any{})
	assertEqual(t, 1, attempts)
	assertEqual(t, 400, resp.Status)
}

func TestAPIClient_AuthHeader(t *testing.T) {
	var got string
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Write(fakePDF)
	})
	api.doJSON(context.Background(), "/render", "sk_xyz", map[string]any{})
	assertEqual(t, "Bearer sk_xyz", got)
}

func TestAPIClient_NoAuthWhenEmpty(t *testing.T) {
	var got string
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Write(fakePDF)
	})
	api.doJSON(context.Background(), "/try", "", map[string]any{})
	assertEqual(t, "", got)
}

func TestAPIClient_UserAgent(t *testing.T) {
	var got string
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.Write(fakePDF)
	})
	api.doJSON(context.Background(), "/render", "sk_test", map[string]any{})
	assertEqual(t, "foliopdf-mcp/0.1.0", got)
}

func TestAPIClient_ContextCancelled(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("err"))
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := api.doJSON(ctx, "/render", "sk_test", map[string]any{})
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestAPIClient_RateLimitHeaders(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "100")
		w.Header().Set("X-RateLimit-Remaining", "73")
		w.Header().Set("X-RateLimit-Reset", "2026-05-01T00:00:00Z")
		w.Header().Set("X-Plan", "free")
		w.Write(fakePDF)
	})
	resp, err := api.doJSON(context.Background(), "/render", "sk_test", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.RateLimit == nil {
		t.Fatal("expected rate limit info")
	}
	assertEqual(t, "100", resp.RateLimit.Limit)
	assertEqual(t, "73", resp.RateLimit.Remaining)
	assertEqual(t, "2026-05-01T00:00:00Z", resp.RateLimit.Reset)
	assertEqual(t, "free", resp.RateLimit.Plan)
}

func TestAPIClient_NoRateLimitHeaders(t *testing.T) {
	api := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(fakePDF)
	})
	resp, _ := api.doJSON(context.Background(), "/render", "sk_test", map[string]any{})
	if resp.RateLimit != nil {
		t.Error("expected no rate limit info when headers absent")
	}
}

func TestParseAPIError_ValidJSON(t *testing.T) {
	d, _ := parseAPIError(429, []byte(`{"error":{"code":"rate_limited","message":"Too fast"}}`))
	assertEqual(t, "rate_limited", d.Code)
	assertEqual(t, "Too fast", d.Message)
}

func TestParseAPIError_InvalidJSON(t *testing.T) {
	d, _ := parseAPIError(500, []byte("not json"))
	assertEqual(t, "unknown_error", d.Code)
}

func TestFormatToolError_QuotaExceeded(t *testing.T) {
	r := formatToolError(apiErrorDetail{Code: "quota_exceeded", Message: "x"})
	assertContains(t, resultText(t, r), "render limit")
}

func TestFormatToolError_RateLimited(t *testing.T) {
	r := formatToolError(apiErrorDetail{Code: "rate_limited", Message: "5/min"})
	assertContains(t, resultText(t, r), "5/min")
	assertContains(t, resultText(t, r), "foliopdf.dev")
}

func TestFormatToolError_Passthrough(t *testing.T) {
	r := formatToolError(apiErrorDetail{Code: "invalid_request", Message: "HTML is required"})
	assertEqual(t, "HTML is required", resultText(t, r))
}

// ============================================================
// Stored response + formatSize
// ============================================================

func TestDeliverStoredPDF_Malformed(t *testing.T) {
	r := deliverStoredPDF([]byte("nope"), "Gen")
	assertIsError(t, r)
	assertContains(t, resultText(t, r), "Failed to parse")
}

func TestDeliverStoredPDF_Valid(t *testing.T) {
	data, _ := json.Marshal(storedResponse{URL: "https://x.dev/dl/abc", SizeBytes: 1048576, ExpiresAt: "2026-04-01T23:00:00Z"})
	r := deliverStoredPDF(data, "Gen")
	assertNoError(t, r)
	assertContains(t, resultText(t, r), "1.0 MB")
	assertContains(t, resultText(t, r), "dl/abc")
}

func TestDeliverStoredPDF_EmptyURL(t *testing.T) {
	data, _ := json.Marshal(storedResponse{URL: "", SizeBytes: 100, ExpiresAt: "2026-04-01T23:00:00Z"})
	r := deliverStoredPDF(data, "Gen")
	assertIsError(t, r)
	assertContains(t, resultText(t, r), "no download URL")
}

func TestDeliverStoredPDF_ZeroSize(t *testing.T) {
	data, _ := json.Marshal(storedResponse{URL: "https://x.dev/dl/abc", SizeBytes: 0, ExpiresAt: "2026-04-01T23:00:00Z"})
	r := deliverStoredPDF(data, "Gen")
	assertIsError(t, r)
	assertContains(t, resultText(t, r), "invalid size")
}

func TestDeliverStoredPDF_NegativeSize(t *testing.T) {
	data, _ := json.Marshal(storedResponse{URL: "https://x.dev/dl/abc", SizeBytes: -1, ExpiresAt: "2026-04-01T23:00:00Z"})
	r := deliverStoredPDF(data, "Gen")
	assertIsError(t, r)
	assertContains(t, resultText(t, r), "invalid size")
}

func TestFormatSize(t *testing.T) {
	for _, tt := range []struct{ b int; want string }{
		{0, "0 bytes"}, {500, "500 bytes"}, {1024, "1.0 KB"}, {91234, "89.1 KB"}, {1048576, "1.0 MB"},
	} {
		assertEqual(t, tt.want, formatSize(tt.b))
	}
}

// ============================================================
// Path validation
// ============================================================

func TestValidateFilePath_Traversal(t *testing.T) {
	err := validateFilePath("/tmp/../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestValidateFilePath_RelativePath(t *testing.T) {
	err := validateFilePath("relative/path.pdf")
	if err == nil {
		t.Error("expected error for relative path")
	}
}

func TestValidateFilePath_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "ok.pdf")
	os.WriteFile(f, fakePDF, 0644)
	err := validateFilePath(f)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateFilePath_Directory(t *testing.T) {
	err := validateFilePath(os.TempDir())
	if err == nil {
		t.Error("expected error for directory")
	}
}

func TestValidateFilePath_NonexistentFile(t *testing.T) {
	err := validateFilePath("/nonexistent/file.pdf")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

// ============================================================
// helpers
// ============================================================

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

func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("expected %q to NOT contain %q", s, substr)
	}
}
