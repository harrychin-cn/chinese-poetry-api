package handler

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ConsolePage returns the built-in customer console for API key, Qanlo billing,
// poem search, and user-provided Qanlo image-key generation.
func ConsolePage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", renderProductHTML(c, consoleHTML))
}

// HomePage returns the product landing page for first-time visitors.
func HomePage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", renderProductHTML(c, homeHTML))
}

// ConsolePlaceholderImage returns the built-in ink painting background used by
// the image preview placeholder.
func ConsolePlaceholderImage(c *gin.Context) {
	c.Data(http.StatusOK, "image/png", consolePlaceholderBG)
}

// ManifestJSON returns the PWA manifest for installing the console.
func ManifestJSON(c *gin.Context) {
	c.Data(http.StatusOK, "application/manifest+json; charset=utf-8", renderProductContent(c, string(manifestJSON)))
}

// ServiceWorkerJS returns the offline cache service worker.
func ServiceWorkerJS(c *gin.Context) {
	c.Data(http.StatusOK, "application/javascript; charset=utf-8", renderProductContent(c, string(serviceWorkerJS)))
}

// PWAIconSVG returns the app icon used by the web manifest.
func PWAIconSVG(c *gin.Context) {
	c.Data(http.StatusOK, "image/svg+xml; charset=utf-8", pwaIconSVG)
}

// DocsPage returns a minimal built-in developer docs page.
func DocsPage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", renderProductHTML(c, docsHTML))
}

// PricingPage returns a customer-facing pricing page for commercial validation.
func PricingPage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", renderProductHTML(c, pricingHTML))
}

//go:embed console.html
var consoleHTML string

//go:embed home.html
var homeHTML string

//go:embed console_placeholder_bg.png
var consolePlaceholderBG []byte

//go:embed manifest.json
var manifestJSON []byte

//go:embed service-worker.js
var serviceWorkerJS []byte

//go:embed pwa-icon.svg
var pwaIconSVG []byte

//go:embed docs.html
var docsHTML string

//go:embed pricing.html
var pricingHTML string
