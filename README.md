# foliopdf-mcp

MCP server that gives AI agents the ability to generate, manipulate, and extract PDFs using [FolioPDF](https://foliopdf.dev).

No Chrome. No Node. No external APIs. Renders natively as a single Go binary.

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

## Claude Desktop setup

**Free mode** (PDFs will have a watermark):
```json
{
  "mcpServers": {
    "foliopdf": {
      "command": "foliopdf-mcp"
    }
  }
}
```

**With API key** (no watermark):
```json
{
  "mcpServers": {
    "foliopdf": {
      "command": "foliopdf-mcp",
      "env": {
        "FOLIOPDF_API_KEY": "sk_live_..."
      }
    }
  }
}
```

Get a free API key at [foliopdf.dev/keys](https://foliopdf.dev/keys).

Generated PDFs are saved to a temporary directory. Claude Desktop can display them directly in the chat.

## Tools

| Tool | What it does | API key required |
|------|-------------|-----------------|
| `render_html` | HTML to PDF with Tailwind CSS, headers/footers | No (watermarked) |
| `render_markdown` | Markdown to PDF with themes and syntax highlighting | No (watermarked) |
| `render_template` | Render a stored template with data variables | Yes |
| `manipulate_pdf` | Merge, split, or rotate PDFs | Yes |
| `extract_pdf` | Extract text and metadata from a PDF | Yes |

### Template syntax

All rendering tools support Go `html/template` syntax:

```
{{.variable}}
{{range .items}}...{{end}}
{{if .condition}}...{{end}}
```

## Hosted mode

For remote agents (ChatGPT, custom agents):

```bash
MCP_TRANSPORT=sse foliopdf-mcp
# or
MCP_TRANSPORT=streamable foliopdf-mcp
```

Listens on `:8080` by default (override with `MCP_ADDR`).

**Docker:**
```bash
docker build -t foliopdf-mcp .
docker run -p 8080:8080 foliopdf-mcp
```

**Health checks:**
- `GET /health/live` — liveness probe (always 200)
- `GET /health/ready` — readiness probe (checks upstream API)

Auth via `Authorization: Bearer sk_live_...` header per request.

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `FOLIOPDF_API_KEY` | _(empty = free mode)_ | API key for authenticated endpoints |
| `FOLIOPDF_API_URL` | `https://api.foliopdf.dev/v1` | API base URL |
| `MCP_TRANSPORT` | `stdio` | Transport mode: `sse` or `streamable` |
| `MCP_ADDR` | `:8080` | Listen address for hosted modes |

## Development

```bash
go build ./...
go test ./...
go vet ./...
./foliopdf-mcp --version
```

## Terms

By using this tool with the FolioPDF API, you agree to the [Terms of Service](https://foliopdf.dev/terms) and [Privacy Policy](https://foliopdf.dev/privacy).

## License

MIT
