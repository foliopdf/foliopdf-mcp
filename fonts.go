package main

import (
	"embed"
	"os"
	"path/filepath"
	"sync"
)

//go:embed assets/fonts/*.ttf
var embeddedFonts embed.FS

var (
	fontsDir     string
	fontsOnce    sync.Once
	fontsInitErr error
)

// ensureFonts extracts embedded fonts to a cache directory and returns the path.
// Called once, cached for the lifetime of the process.
func ensureFonts() (string, error) {
	fontsOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			fontsInitErr = err
			return
		}

		dir := filepath.Join(home, ".cache", "foliopdf", "fonts")
		if err := os.MkdirAll(dir, 0755); err != nil {
			fontsInitErr = err
			return
		}

		entries, err := embeddedFonts.ReadDir("assets/fonts")
		if err != nil {
			fontsInitErr = err
			return
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			dst := filepath.Join(dir, entry.Name())

			// Skip if already cached and same size
			if info, err := os.Stat(dst); err == nil {
				eInfo, _ := entry.Info()
				if info.Size() == eInfo.Size() {
					continue
				}
			}

			data, err := embeddedFonts.ReadFile("assets/fonts/" + entry.Name())
			if err != nil {
				fontsInitErr = err
				return
			}
			if err := os.WriteFile(dst, data, 0644); err != nil {
				fontsInitErr = err
				return
			}
		}

		fontsDir = dir
	})
	return fontsDir, fontsInitErr
}

// fontFaceCSS returns @font-face declarations for the bundled fonts.
func fontFaceCSS(fontsPath string) string {
	return `
@font-face {
  font-family: 'Inter';
  font-weight: 400;
  font-style: normal;
  src: url('` + filepath.Join(fontsPath, "Inter-Regular.ttf") + `');
}
@font-face {
  font-family: 'Inter';
  font-weight: 700;
  font-style: normal;
  src: url('` + filepath.Join(fontsPath, "Inter-Bold.ttf") + `');
}
@font-face {
  font-family: 'Inter';
  font-weight: 400;
  font-style: italic;
  src: url('` + filepath.Join(fontsPath, "Inter-Italic.ttf") + `');
}
@font-face {
  font-family: 'Inter';
  font-weight: 700;
  font-style: italic;
  src: url('` + filepath.Join(fontsPath, "Inter-BoldItalic.ttf") + `');
}
@font-face {
  font-family: 'Geist';
  font-weight: 400;
  font-style: normal;
  src: url('` + filepath.Join(fontsPath, "Geist-Regular.ttf") + `');
}
@font-face {
  font-family: 'Geist';
  font-weight: 700;
  font-style: normal;
  src: url('` + filepath.Join(fontsPath, "Geist-Bold.ttf") + `');
}
@font-face {
  font-family: 'Geist';
  font-weight: 400;
  font-style: italic;
  src: url('` + filepath.Join(fontsPath, "Geist-Italic.ttf") + `');
}
@font-face {
  font-family: 'Geist';
  font-weight: 700;
  font-style: italic;
  src: url('` + filepath.Join(fontsPath, "Geist-BoldItalic.ttf") + `');
}
`
}
