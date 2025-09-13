package main

import (
	"strings"
	"testing"
)

func TestSanitizeAgentVersion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal version string",
			input:    "kubo/0.18.0",
			expected: "kubo/0.18.0",
		},
		{
			name:     "version with emoji preserved",
			input:    "my-client/1.0.0 🚀",
			expected: "my-client/1.0.0 🚀",
		},
		{
			name:     "international characters preserved",
			input:    "клиент/версия-1.0 测试",
			expected: "клиент/версия-1.0 测试",
		},
		{
			name:     "control characters replaced with U+FFFD",
			input:    "client\x00\x01\x02/1.0",
			expected: "client���/1.0",
		},
		{
			name:     "newlines and tabs replaced",
			input:    "client\n\r\t/1.0",
			expected: "client���/1.0",
		},
		{
			name:     "zero-width characters replaced",
			input:    "client\u200B\u200C\u200D/1.0",
			expected: "client���/1.0",
		},
		{
			name:     "RTL/LTR overrides replaced",
			input:    "client\u202A\u202B\u202C/1.0",
			expected: "client���/1.0",
		},
		{
			name:     "bidi isolates replaced",
			input:    "client\u2066\u2067\u2068\u2069/1.0",
			expected: "client����/1.0",
		},
		{
			name:     "BOM replaced",
			input:    "\uFEFFclient/1.0",
			expected: "�client/1.0",
		},
		{
			name:     "whitespace trimmed",
			input:    "  client/1.0  ",
			expected: "client/1.0",
		},
		{
			name:     "whitespace trimmed after sanitization",
			input:    "\x00  client/1.0  \x00",
			expected: "�  client/1.0  �",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: "",
		},
		{
			name:     "only control characters become replacement characters",
			input:    "\x00\x01\x02",
			expected: "���",
		},
		{
			name:     "private use characters preserved",
			input:    "client\uE000\uF8FF/1.0",
			expected: "client\uE000\uF8FF/1.0",
		},
		{
			name:     "length limited to 128 runes",
			input:    strings.Repeat("a", 150),
			expected: strings.Repeat("a", 128),
		},
		{
			name:     "length limit preserves multibyte runes",
			input:    strings.Repeat("世", 150),
			expected: strings.Repeat("世", 128),
		},
		{
			name:     "length limit after sanitization",
			input:    "\x00" + strings.Repeat("a", 150),
			expected: "�" + strings.Repeat("a", 127),
		},
		{
			name:     "terminal escape sequences replaced",
			input:    "client\x1b[31m/1.0\x1b[0m",
			expected: "client�[31m/1.0�[0m",
		},
		{
			name:     "mixed problematic and valid characters",
			input:    "my\x00client\u200B/версия\u202A-1.0🚀",
			expected: "my�client�/версия�-1.0🚀",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeAgentVersion(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeAgentVersion(%q) = %q, want %q", tt.input, result, tt.expected)
				// Print runes for debugging
				t.Logf("Result runes: %+q", result)
				t.Logf("Expected runes: %+q", tt.expected)
			}
		})
	}
}

func TestSanitizeAgentVersionMaxLength(t *testing.T) {
	// Test that length is counted in runes, not bytes
	// Each emoji is 1 rune but multiple bytes
	emojis := strings.Repeat("🚀", 130)
	result := sanitizeAgentVersion(emojis)
	runeCount := len([]rune(result))
	if runeCount != 128 {
		t.Errorf("Expected 128 runes, got %d", runeCount)
	}

	// Test that multibyte characters are counted correctly
	chinese := strings.Repeat("世", 130)
	result = sanitizeAgentVersion(chinese)
	runeCount = len([]rune(result))
	if runeCount != 128 {
		t.Errorf("Expected 128 runes, got %d", runeCount)
	}
}

func TestSanitizeAgentVersionSecurity(t *testing.T) {
	// Test that no control characters survive sanitization
	dangerousInputs := []string{
		"\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f",
		"\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f",
		"\u200B\u200C\u200D\u202A\u202B\u202C\u202D\u202E",
		"\u2066\u2067\u2068\u2069\uFEFF",
	}

	for _, input := range dangerousInputs {
		result := sanitizeAgentVersion("test/" + input + "/1.0")
		// Check that no control or format characters remain
		for _, r := range result {
			if r < 0x20 && r != '\uFFFD' {
				t.Errorf("Control character %U found in sanitized output", r)
			}
			if r >= 0x200B && r <= 0x200D && r != '\uFFFD' {
				t.Errorf("Zero-width character %U found in sanitized output", r)
			}
			if r >= 0x202A && r <= 0x202E && r != '\uFFFD' {
				t.Errorf("Bidi override character %U found in sanitized output", r)
			}
			if r >= 0x2066 && r <= 0x2069 && r != '\uFFFD' {
				t.Errorf("Bidi isolate character %U found in sanitized output", r)
			}
			if r == 0xFEFF && r != '\uFFFD' {
				t.Errorf("BOM character %U found in sanitized output", r)
			}
		}
	}
}
