package main

import "testing"

func TestExtractAPIToken(t *testing.T) {
	tests := []struct {
		body     string
		expected string
	}{
		{
			body:     `)}],v=(t("8e6e"),t("ac6a"),t("456d"),t("bd86")),w=(t("a481"),t("6b54"),t("bc3a")),R=t.n(w),y="https://skyadmin.io/api/",S="https://dev.skyadmin.io/api/",E="6dbb801a63dcec89d06e9ccdbce7948a",C=R.a.create({baseU`,
			expected: "6dbb801a63dcec89d06e9ccdbce7948a",
		},
		{
			body:     `no token here`,
			expected: "",
		},
		{
			body:     `E="0191a4fdcc8a99e388dc3830e25ced43"`,
			expected: "0191a4fdcc8a99e388dc3830e25ced43",
		},
		{
			body:     `E="12345"`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			result := extractAPIToken(tt.body)
			if result != tt.expected {
				t.Errorf("extractAPIToken(%q) = %q, want %q", tt.body, result, tt.expected)
			}
		})
	}
}

func TestExtractJSPath(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "Webpack hash file",
			html:     `<script src="/js/app.e360d181.js"></script>`,
			expected: "/js/app.e360d181.js",
		},
		{
			name:     "Different hash",
			html:     `<script src="/js/app.abc123def.js"></script>`,
			expected: "/js/app.abc123def.js",
		},
		{
			name:     "Real splash page format",
			html:     `<script src="/js/app.3e21a4a7.js"></script>`,
			expected: "/js/app.3e21a4a7.js",
		},
		{
			name:     "Real splash page with preload",
			html:     `<link href="/js/app.3e21a4a7.js" rel="preload" as="script"><script src="/js/app.3e21a4a7.js"></script>`,
			expected: "/js/app.3e21a4a7.js",
		},
		{
			name:     "Fallback JS file",
			html:     `<script src="/js/vendor.min.js"></script>`,
			expected: "/js/vendor.min.js",
		},
		{
			name:     "Multiple JS files - should get first app file",
			html:     `<script src="/js/vendor.js"></script><script src="/js/app.e360d181.js"></script>`,
			expected: "/js/app.e360d181.js",
		},
		{
			name:     "No JS files",
			html:     `<html><body>No JS here</body></html>`,
			expected: "",
		},
		{
			name:     "JS file not in /js/ directory",
			html:     `<script src="/assets/main.js"></script>`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSPath(tt.html)
			if result != tt.expected {
				t.Errorf("extractJSPath() = %q, want %q", result, tt.expected)
			}
		})
	}
}
