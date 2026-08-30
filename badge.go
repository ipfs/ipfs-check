package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

const (
	// Badge colors (shields.io style)
	colorGreen  = "#4c1"    // Bright green - highly available (2+ providers)
	colorYellow = "#dfb317" // Yellow - low availability (1 provider)
	colorRed    = "#e05d44" // Red - unavailable (0 providers)
	colorGray   = "#9f9f9f" // Gray - error/unknown state

	// Badge dimensions
	labelWidth  = 32 // Width for "IPFS" label
	badgeHeight = 20
)

// generateBadgeSVG creates a shields.io-style SVG badge showing provider count and CID suffix.
func generateBadgeSVG(providerCount int, cidSuffix string) []byte {
	var color, status string

	switch {
	case providerCount == 0:
		color = colorRed
		status = fmt.Sprintf("unavailable ...%s", cidSuffix)
	case providerCount == 1:
		color = colorYellow
		status = fmt.Sprintf("1 provider ...%s", cidSuffix)
	default:
		color = colorGreen
		status = fmt.Sprintf("%d providers ...%s", providerCount, cidSuffix)
	}

	return buildBadgeSVG("IPFS", status, color)
}

// getCIDSuffix returns the last 6 characters of a CID string.
func getCIDSuffix(cidStr string) string {
	if len(cidStr) <= 6 {
		return cidStr
	}
	return cidStr[len(cidStr)-6:]
}

// generateErrorBadgeSVG creates a badge indicating an error occurred.
func generateErrorBadgeSVG(cidSuffix string) []byte {
	return buildBadgeSVG("IPFS", fmt.Sprintf("error ...%s", cidSuffix), colorGray)
}

// generatePendingBadgeSVG creates a badge indicating the check is in progress.
func generatePendingBadgeSVG(cidSuffix string) []byte {
	return buildBadgeSVG("IPFS", fmt.Sprintf("checking ...%s", cidSuffix), colorGray)
}

// buildBadgeSVG constructs the SVG markup for a badge with given label, status, and color.
func buildBadgeSVG(label, status, color string) []byte {
	// Calculate widths based on text length (approximate)
	statusWidth := len(status)*7 + 10
	totalWidth := labelWidth + statusWidth
	labelX := labelWidth / 2
	statusX := labelWidth + statusWidth/2

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">
  <linearGradient id="b" x2="0" y2="100%%">
    <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
    <stop offset="1" stop-opacity=".1"/>
  </linearGradient>
  <clipPath id="a">
    <rect width="%d" height="%d" rx="3" fill="#fff"/>
  </clipPath>
  <g clip-path="url(#a)">
    <rect width="%d" height="%d" fill="#555"/>
    <rect x="%d" width="%d" height="%d" fill="%s"/>
    <rect width="%d" height="%d" fill="url(#b)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="DejaVu Sans,Verdana,Geneva,sans-serif" font-size="11">
    <text x="%d" y="15" fill="#010101" fill-opacity=".3">%s</text>
    <text x="%d" y="14">%s</text>
    <text x="%d" y="15" fill="#010101" fill-opacity=".3">%s</text>
    <text x="%d" y="14">%s</text>
  </g>
</svg>`,
		totalWidth, badgeHeight,
		totalWidth, badgeHeight,
		labelWidth, badgeHeight,
		labelWidth, statusWidth, badgeHeight, color,
		totalWidth, badgeHeight,
		labelX, label,
		labelX, label,
		statusX, status,
		statusX, status,
	)

	return []byte(svg)
}

// countWorkingProviders counts providers that are connected and have data available.
func countWorkingProviders(providers *[]providerOutput) int {
	if providers == nil {
		return 0
	}

	count := 0
	for _, p := range *providers {
		if p.ConnectionError == "" &&
			(p.DataAvailableOverBitswap.Found || p.DataAvailableOverHTTP.Found) {
			count++
		}
	}
	return count
}

// BadgeHandler handles HTTP requests for badge images.
type BadgeHandler struct {
	daemon       *daemon
	cache        *BadgeCache
	checkTimeout time.Duration
}

// NewBadgeHandler creates a new badge handler.
func NewBadgeHandler(d *daemon, cache *BadgeCache, checkTimeout time.Duration) *BadgeHandler {
	return &BadgeHandler{
		daemon:       d,
		cache:        cache,
		checkTimeout: checkTimeout,
	}
}

// ServeHTTP handles badge requests.
// For uncached CIDs, it returns a "checking..." badge immediately and triggers
// a background check. Subsequent requests will receive the cached result.
func (h *BadgeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	cidStr := r.URL.Query().Get("cid")
	if cidStr == "" {
		http.Error(w, "missing 'cid' query parameter", http.StatusBadRequest)
		return
	}

	cidSuffix := getCIDSuffix(cidStr)

	// Try to get from cache first (includes completed results)
	if entry, ok := h.cache.Get(cidStr); ok {
		if !entry.Pending {
			// Completed result - serve it
			h.serveSVG(w, entry.SVG)
			return
		}
		// Still pending - serve the pending badge
		h.serveSVG(w, entry.SVG)
		return
	}

	// Check if already pending (avoid duplicate background checks)
	if h.cache.IsPending(cidStr) {
		svg := generatePendingBadgeSVG(cidSuffix)
		h.serveSVG(w, svg)
		return
	}

	// Not in cache and not pending - start background check
	pendingSVG := generatePendingBadgeSVG(cidSuffix)
	h.cache.SetPending(cidStr, cidSuffix, pendingSVG)

	// Trigger background check
	go h.runBackgroundCheck(cidStr, cidSuffix)

	// Return pending badge immediately
	h.serveSVG(w, pendingSVG)
}

// runBackgroundCheck performs the CID check in the background and caches the result.
func (h *BadgeHandler) runBackgroundCheck(cidStr, cidSuffix string) {
	ctx, cancel := context.WithTimeout(context.Background(), h.checkTimeout)
	defer cancel()

	cidKey, _, err := resolveInput(ctx, h.daemon.ns, cidStr)
	if err != nil {
		log.Printf("Badge: failed to resolve CID %s: %v", cidStr, err)
		svg := generateErrorBadgeSVG(cidSuffix)
		h.cache.Set(cidStr, 0, cidSuffix, svg)
		return
	}

	result, err := h.daemon.runCidCheck(ctx, cidKey, defaultIndexerURL, false)
	if err != nil {
		log.Printf("Badge: failed to check CID %s: %v", cidStr, err)
		svg := generateErrorBadgeSVG(cidSuffix)
		h.cache.Set(cidStr, 0, cidSuffix, svg)
		return
	}

	// Count working providers and generate badge
	providerCount := countWorkingProviders(result)
	svg := generateBadgeSVG(providerCount, cidSuffix)

	// Cache the result
	h.cache.Set(cidStr, providerCount, cidSuffix, svg)
	log.Printf("Badge: cached result for %s: %d providers", cidStr, providerCount)
}

// serveSVG writes an SVG response with appropriate headers.
func (h *BadgeHandler) serveSVG(w http.ResponseWriter, svg []byte) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600") // Browser cache for 1 hour
	w.Write(svg)
}
