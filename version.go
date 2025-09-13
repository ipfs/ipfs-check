package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
	"time"
	"unicode"
)

//go:embed version.json
var versionJSON []byte

var name = "ipfs-check"
var version = buildVersion()
var userAgent = name + "/" + version

func buildVersion() string {
	// Read version from embedded JSON file.
	var verMap map[string]string
	json.Unmarshal(versionJSON, &verMap)
	release := verMap["version"]

	var revision string
	var day string
	var dirty bool

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return release + " dev-build"
	}
	for _, kv := range info.Settings {
		switch kv.Key {
		case "vcs.revision":
			revision = kv.Value[:7]
		case "vcs.time":
			t, _ := time.Parse(time.RFC3339, kv.Value)
			day = t.UTC().Format("2006-01-02")
		case "vcs.modified":
			dirty = kv.Value == "true"
		}
	}
	if dirty {
		revision += "-dirty"
	}
	if revision != "" {
		return fmt.Sprintf("%s %s-%s", release, day, revision)
	}
	return release + " dev-build"
}

const maxAgentVersionLen = 128

// sanitizeAgentVersion sanitizes untrusted agent version strings from remote peers
// to prevent display issues across web UIs, terminals, and logs. It replaces control
// characters, format characters, and surrogate characters with U+FFFD (�), then enforces
// a maximum length of 128 runes. Per RFC 9839, replacing problematic code points with
// U+FFFD is preferred over silently deleting them, as deletion is a known security risk.
// See https://github.com/libp2p/specs/pull/491 and https://www.rfc-editor.org/rfc/rfc9839.html for context.
func sanitizeAgentVersion(version string) string {
	// Build sanitized result
	var result []rune
	for _, r := range version {
		// Replace control characters (Cc) with U+FFFD - prevents terminal escapes, CR, LF, etc.
		if unicode.Is(unicode.Cc, r) {
			result = append(result, '\uFFFD')
			continue
		}
		// Replace format characters (Cf) with U+FFFD - prevents RTL/LTR overrides, zero-width chars
		if unicode.Is(unicode.Cf, r) {
			result = append(result, '\uFFFD')
			continue
		}
		// Replace surrogate characters (Cs) with U+FFFD - invalid in UTF-8
		if unicode.Is(unicode.Cs, r) {
			result = append(result, '\uFFFD')
			continue
		}
		// Preserve legitimate Unicode including private use characters (Co)
		result = append(result, r)
	}

	// Convert to string and trim whitespace
	sanitized := strings.TrimSpace(string(result))

	// Enforce maximum length (128 runes, not bytes)
	runes := []rune(sanitized)
	if len(runes) > maxAgentVersionLen {
		return string(runes[:maxAgentVersionLen])
	}

	return sanitized
}
