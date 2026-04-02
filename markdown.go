package main

import (
	"bytes"
	"fmt"

	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

type markdownTheme string

const (
	themeGitHub  markdownTheme = "github"
	themeMinimal markdownTheme = "minimal"
	themePrint   markdownTheme = "print"
)

type markdownOpts struct {
	Theme       string
	SyntaxTheme string
}

// convertMarkdown transforms Markdown into a complete HTML document.
func convertMarkdown(md string, opts markdownOpts) (string, error) {
	if md == "" {
		return "", fmt.Errorf("markdown content is empty")
	}

	theme := markdownTheme(opts.Theme)
	if theme == "" {
		theme = themeGitHub
	}

	syntaxTheme := opts.SyntaxTheme
	if syntaxTheme == "" {
		syntaxTheme = "github"
	}
	if styles.Get(syntaxTheme) == nil {
		syntaxTheme = "github"
	}

	converter := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Table,
			extension.Strikethrough,
			extension.TaskList,
			extension.Footnote,
			highlighting.NewHighlighting(
				highlighting.WithStyle(syntaxTheme),
			),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			goldmarkhtml.WithHardWraps(),
			goldmarkhtml.WithXHTML(),
			goldmarkhtml.WithUnsafe(),
		),
	)

	var buf bytes.Buffer
	if err := converter.Convert([]byte(md), &buf); err != nil {
		return "", fmt.Errorf("markdown conversion failed: %w", err)
	}

	themeCSS := getThemeCSS(theme)
	return buildMarkdownDocument(buf.String(), themeCSS), nil
}

func buildMarkdownDocument(body, themeCSS string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<style>
%s
</style>
</head>
<body>
<div class="markdown-body">
%s
</div>
</body>
</html>`, themeCSS, body)
}

func getThemeCSS(theme markdownTheme) string {
	switch theme {
	case themeMinimal:
		return minimalCSS
	case themePrint:
		return printCSS
	default:
		return githubCSS
	}
}

const githubCSS = `
.markdown-body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
  font-size: 16px;
  line-height: 1.6;
  color: #24292e;
  word-wrap: break-word;
}
.markdown-body h1, .markdown-body h2, .markdown-body h3,
.markdown-body h4, .markdown-body h5, .markdown-body h6 {
  margin-top: 24px; margin-bottom: 16px; font-weight: 600; line-height: 1.25;
}
.markdown-body h1 { font-size: 2em; padding-bottom: 0.3em; border-bottom: 1px solid #eaecef; }
.markdown-body h2 { font-size: 1.5em; padding-bottom: 0.3em; border-bottom: 1px solid #eaecef; }
.markdown-body h3 { font-size: 1.25em; }
.markdown-body p { margin-top: 0; margin-bottom: 16px; }
.markdown-body a { color: #0366d6; text-decoration: none; }
.markdown-body strong { font-weight: 600; }
.markdown-body ul, .markdown-body ol { padding-left: 2em; margin-bottom: 16px; }
.markdown-body li + li { margin-top: 0.25em; }
.markdown-body blockquote {
  margin: 0 0 16px 0; padding: 0 1em; color: #6a737d; border-left: 0.25em solid #dfe2e5;
}
.markdown-body code {
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
  font-size: 85%; background-color: #f6f8fa; padding: 0.2em 0.4em; border-radius: 3px;
}
.markdown-body pre {
  margin-bottom: 16px; padding: 16px; font-size: 85%; line-height: 1.45;
  background-color: #f6f8fa; border-radius: 6px; overflow: auto;
}
.markdown-body pre code { display: block; padding: 0; background-color: transparent; border: 0; }
.markdown-body table { border-collapse: collapse; margin-bottom: 16px; width: 100%; }
.markdown-body table th, .markdown-body table td { padding: 6px 13px; border: 1px solid #dfe2e5; }
.markdown-body table th { font-weight: 600; background-color: #f6f8fa; }
.markdown-body table tr:nth-child(2n) { background-color: #f6f8fa; }
.markdown-body img { max-width: 100%; height: auto; }
.markdown-body hr { height: 0.25em; padding: 0; margin: 24px 0; background-color: #e1e4e8; border: 0; }
.markdown-body .footnotes { font-size: 85%; color: #6a737d; border-top: 1px solid #eaecef; margin-top: 32px; padding-top: 16px; }
`

const minimalCSS = `
.markdown-body {
  font-family: Georgia, "Times New Roman", serif; font-size: 12pt; line-height: 1.8; color: #000;
}
.markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4 {
  margin-top: 1.5em; margin-bottom: 0.5em; font-weight: bold; line-height: 1.3;
}
.markdown-body h1 { font-size: 1.8em; }
.markdown-body h2 { font-size: 1.4em; }
.markdown-body h3 { font-size: 1.2em; }
.markdown-body p { margin-bottom: 1em; }
.markdown-body a { color: #000; text-decoration: underline; }
.markdown-body ul, .markdown-body ol { padding-left: 2em; margin-bottom: 1em; }
.markdown-body blockquote { margin: 0 0 1em 0; padding-left: 1.5em; font-style: italic; color: #333; }
.markdown-body code { font-family: "Courier New", Courier, monospace; font-size: 90%; }
.markdown-body pre { margin-bottom: 1em; padding: 12px; font-size: 90%; line-height: 1.5; overflow: auto; }
.markdown-body table { border-collapse: collapse; margin-bottom: 1em; width: 100%; }
.markdown-body table th, .markdown-body table td { padding: 8px 12px; text-align: left; }
.markdown-body table th { font-weight: bold; border-top: 2px solid #000; border-bottom: 1px solid #000; }
.markdown-body table tr:last-child td { border-bottom: 2px solid #000; }
.markdown-body img { max-width: 100%; height: auto; }
.markdown-body hr { border: none; border-top: 1px solid #ccc; margin: 2em 0; }
.markdown-body .footnotes { font-size: 85%; color: #555; margin-top: 2em; padding-top: 1em; border-top: 1px solid #ccc; }
`

const printCSS = `
.markdown-body {
  font-family: "Palatino Linotype", Palatino, "Book Antiqua", serif; font-size: 11pt; line-height: 1.5; color: #000;
}
.markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4 {
  margin-top: 1.5em; margin-bottom: 0.5em; font-weight: bold; line-height: 1.3;
}
.markdown-body h1 { font-size: 1.8em; text-transform: uppercase; letter-spacing: 0.05em; page-break-before: always; }
.markdown-body h1:first-child { page-break-before: auto; }
.markdown-body h2 { font-size: 1.4em; font-variant: small-caps; }
.markdown-body h3 { font-size: 1.2em; }
.markdown-body p { margin-bottom: 0.8em; text-align: justify; }
.markdown-body a { color: #000; text-decoration: underline; }
.markdown-body ul, .markdown-body ol { padding-left: 2em; margin-bottom: 0.8em; }
.markdown-body blockquote { margin: 0 0 0.8em 1em; padding-left: 1em; border-left: 2px solid #999; font-style: italic; color: #333; }
.markdown-body code { font-family: "Courier New", Courier, monospace; font-size: 10pt; }
.markdown-body pre { margin-bottom: 1em; padding: 12px; font-size: 10pt; line-height: 1.4; border: 1px solid #ccc; background-color: #f9f9f9; overflow: auto; page-break-inside: avoid; }
.markdown-body table { border-collapse: collapse; margin-bottom: 1em; width: 100%; page-break-inside: avoid; }
.markdown-body table th, .markdown-body table td { padding: 6px 10px; border: 1px solid #999; text-align: left; }
.markdown-body table th { font-weight: bold; background-color: #f0f0f0; }
.markdown-body img { max-width: 100%; height: auto; }
.markdown-body hr { border: none; border-top: 1px solid #000; margin: 1.5em 0; }
.markdown-body .footnotes { font-size: 9pt; color: #333; margin-top: 2em; padding-top: 1em; border-top: 1px solid #999; }
`
