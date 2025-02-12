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
