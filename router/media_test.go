package router

import (
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

func vulnerableValidation(name string) string {
	return "data/upload/" + path.Clean(name)
}

func TestVulnerableCode_PathTraversal(t *testing.T) {
	tests := []struct {
		input      string
		result     string
		vulnerable bool
	}{
		{"test.jpg", "data/upload/test.jpg", false},
		{"../etc/passwd", "data/upload/../etc/passwd", true},
		{"../../etc/passwd", "data/upload/../../etc/passwd", true},
		{"../../../etc/passwd", "data/upload/../../../etc/passwd", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := vulnerableValidation(tt.input)
			assert.Equal(t, tt.result, got)
			if tt.vulnerable {
				t.Logf("VULNERABLE: input %q produces path %q (escapes upload dir)", tt.input, got)
			}
		})
	}
}

func TestValidateMediaFilename(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		wantClean   string
		description string
	}{
		{"empty", "", true, "", "reject empty filename"},
		{"valid", "test.jpg", false, "test.jpg", "accept valid filename"},
		{"traversal_single", "../etc/passwd", true, "", "block ../ traversal"},
		{"traversal_double", "../../etc/passwd", true, "", "block ../../ traversal"},
		{"subdirectory", "subdir/file.jpg", true, "", "block subdirectory"},
		{"absolute", "/etc/passwd", true, "", "block absolute path"},
		{"dot", ".", true, "", "block single dot"},
		{"dotdot", "..", true, "", "block double dot"},
		{"hidden", ".htaccess", false, ".htaccess", "accept hidden files"},
		{"with_spaces", "my file.jpg", false, "my file.jpg", "accept spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateMediaFilename(tt.input)
			if tt.wantErr {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, tt.wantClean, got)
			}
		})
	}
}
