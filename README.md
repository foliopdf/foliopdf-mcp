# FolioPDF MCP Server

[![Go Reference](https://pkg.go.dev/badge/github.com/foliopdf/foliopdf-mcp.svg)](https://pkg.go.dev/github.com/foliopdf/foliopdf-mcp)
[![CI](https://github.com/foliopdf/foliopdf-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/foliopdf/foliopdf-mcp/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/foliopdf/foliopdf-mcp)](https://github.com/foliopdf/foliopdf-mcp/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

PDF generation, manipulation, and extraction for AI agents. Single binary, renders locally, works offline.

## Install

**Homebrew**
```bash
brew install foliopdf/tap/foliopdf-mcp
```

**Go**
```bash
go install github.com/foliopdf/foliopdf-mcp@latest
```

**Direct download**
```bash
# macOS (Apple Silicon)
curl -L https://github.com/foliopdf/foliopdf-mcp/releases/latest/download/foliopdf-mcp-darwin-arm64.tar.gz | tar xz
sudo mv foliopdf-mcp /usr/local/bin/

# macOS (Intel)
curl -L https://github.com/foliopdf/foliopdf-mcp/releases/latest/download/foliopdf-mcp-darwin-amd64.tar.gz | tar xz
sudo mv foliopdf-mcp /usr/local/bin/

# Linux
curl -L https://github.com/foliopdf/foliopdf-mcp/releases/latest/download/foliopdf-mcp-linux-amd64.tar.gz | tar xz
sudo mv foliopdf-mcp /usr/local/bin/
```

## Setup

Add to Claude Desktop (Settings → Developer → Edit Config):

```json
{
  "mcpServers": {
    "foliopdf": {
      "command": "foliopdf-mcp"
    }
  }
}
```

Restart Claude Desktop. Ask it to generate a PDF.

## Tools

| Tool | What it does |
|------|-------------|
| `render_html` | HTML to PDF with Tailwind CSS v3, headers/footers, template variables |
| `render_markdown` | Markdown to PDF with themes and syntax highlighting |
| `manipulate_pdf` | Merge, split, or rotate PDFs |
| `extract_pdf` | Extract text and metadata from a PDF |

Every response includes the PDF as base64 data and a local file path.

## Features

### Tailwind CSS v3

Set `tailwind: true`. The full stylesheet is bundled in the binary — works offline.

```json
{"html": "<div class='bg-blue-500 text-white p-8'>Hello</div>", "tailwind": true}
```

Tailwind v4 is not supported.

### Template variables

Go `html/template` syntax in both HTML and Markdown:

```json
{
  "html": "<h1>Invoice for {{.company}}</h1><p>Amount: {{.amount}}</p>",
  "data": {"company": "Acme Corp", "amount": "$1,234"}
}
```

Supports `{{.variable}}`, `{{range .items}}...{{end}}`, `{{if .condition}}...{{end}}`.

### Headers and footers

Repeating content on every page. Use `<span class='pageNumber'></span>` and `<span class='totalPages'></span>` for page numbers.

```json
{
  "header_html": "<div style='text-align:right;font-size:10px'>{{.company}}</div>",
  "footer_html": "<div style='text-align:center;font-size:9px'>Page <span class='pageNumber'></span> of <span class='totalPages'></span></div>"
}
```

### Markdown themes

Three built-in themes for `render_markdown`:

- **github** — clean sans-serif, colored links, code backgrounds
- **minimal** — serif typography, maximum readability
- **print** — formal layout, justified text, optimized for printing

### Watermarks

Diagonal text overlay on every page:

```json
{"watermark": "DRAFT"}
```

## Development

```bash
go build ./...
go test ./...
./foliopdf-mcp --version
```

## License

MIT
