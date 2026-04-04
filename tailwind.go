package main

import (
	_ "embed"
	"regexp"
	"strings"
)

//go:embed assets/tailwind-v3.css
var rawTailwindCSS string

// tailwindCSS is the flattened Tailwind stylesheet, computed once at startup.
var tailwindCSS = flattenTailwindCSS(rawTailwindCSS)

// flattenTailwindCSS post-processes Tailwind v3 CSS to resolve the
// rgb(var(--tw-*) / opacity) pattern and convert space-separated rgb() to
// comma-separated form for Folio's CSS parser compatibility.
func flattenTailwindCSS(css string) string {
	// Step 1: rgb(R G B/var(--tw-*-opacity,FALLBACK)) → rgba(R,G,B,FALLBACK)
	reVarOpacity := regexp.MustCompile(`rgb\((\d+)\s+(\d+)\s+(\d+)\s*/\s*var\(--tw-[a-z-]+,\s*([0-9.]+)\)\)`)
	css = reVarOpacity.ReplaceAllString(css, "rgba($1,$2,$3,$4)")

	// Step 2: rgb(R G B/alpha) → rgba(R,G,B,alpha)
	reSpaceRGBAlpha := regexp.MustCompile(`rgb\((\d+)\s+(\d+)\s+(\d+)\s*/\s*([0-9.]+)\)`)
	css = reSpaceRGBAlpha.ReplaceAllString(css, "rgba($1,$2,$3,$4)")

	// Step 3: rgb(R G B) space-separated → rgb(R,G,B)
	reSpaceRGB := regexp.MustCompile(`rgb\((\d+)\s+(\d+)\s+(\d+)\)`)
	css = reSpaceRGB.ReplaceAllString(css, "rgb($1,$2,$3)")

	// Step 4: Resolve remaining var(--tw-*) with fallback values
	reRemainingVar := regexp.MustCompile(`var\(--tw-[a-z-]+(?:,\s*([^)]+))?\)`)
	css = reRemainingVar.ReplaceAllStringFunc(css, func(match string) string {
		sub := reRemainingVar.FindStringSubmatch(match)
		if len(sub) > 1 && sub[1] != "" {
			return strings.TrimSpace(sub[1])
		}
		if strings.Contains(match, "opacity") {
			return "1"
		}
		return "0"
	})

	// Step 5: Remove --tw-* custom property declarations
	reCustomProp := regexp.MustCompile(`--tw-[a-z-]+:[^;]+;?`)
	css = reCustomProp.ReplaceAllString(css, "")

	return css
}

// injectFontCSS injects @font-face declarations into the HTML.
func injectFontCSS(htmlStr, fontCSS string) string {
	return injectStyleBlock(htmlStr, fontCSS)
}

// injectStyleBlock inserts a <style> block into the HTML head.
func injectStyleBlock(htmlStr, css string) string {
	styleBlock := "<style>" + css + "</style>"

	if idx := strings.Index(htmlStr, "<head>"); idx != -1 {
		insertAt := idx + len("<head>")
		return htmlStr[:insertAt] + styleBlock + htmlStr[insertAt:]
	}
	if idx := strings.Index(htmlStr, "<head "); idx != -1 {
		closeIdx := strings.Index(htmlStr[idx:], ">")
		if closeIdx != -1 {
			insertAt := idx + closeIdx + 1
			return htmlStr[:insertAt] + styleBlock + htmlStr[insertAt:]
		}
	}
	if idx := strings.Index(htmlStr, "<html"); idx != -1 {
		closeIdx := strings.Index(htmlStr[idx:], ">")
		if closeIdx != -1 {
			insertAt := idx + closeIdx + 1
			return htmlStr[:insertAt] + "<head>" + styleBlock + "</head>" + htmlStr[insertAt:]
		}
	}
	return styleBlock + htmlStr
}

// injectTailwind inserts the Tailwind CSS as a <style> block into the HTML.
func injectTailwind(htmlStr, css string) string {
	return injectStyleBlock(htmlStr, css)
}

// injectHeaderFooter wraps header/footer HTML in position:fixed divs and
// inserts them after <body>.
func injectHeaderFooter(htmlStr, headerHTML, footerHTML string) string {
	var fixed strings.Builder

	if headerHTML != "" {
		fixed.WriteString(`<div style="position:fixed;top:0;left:0;right:0;">`)
		fixed.WriteString(headerHTML)
		fixed.WriteString(`</div>`)
	}
	if footerHTML != "" {
		fixed.WriteString(`<div style="position:fixed;bottom:0;left:0;right:0;">`)
		fixed.WriteString(footerHTML)
		fixed.WriteString(`</div>`)
	}

	injection := fixed.String()

	if idx := strings.Index(htmlStr, "<body>"); idx != -1 {
		insertAt := idx + len("<body>")
		return htmlStr[:insertAt] + injection + htmlStr[insertAt:]
	}
	if idx := strings.Index(htmlStr, "<body "); idx != -1 {
		closeIdx := strings.Index(htmlStr[idx:], ">")
		if closeIdx != -1 {
			insertAt := idx + closeIdx + 1
			return htmlStr[:insertAt] + injection + htmlStr[insertAt:]
		}
	}

	return injection + htmlStr
}
