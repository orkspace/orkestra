package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidEventName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"camelCase", "databaseReady", false},
		{"PascalCase", "DatabaseReady", false},
		{"with digits", "database2Ready", false},
		{"empty", "", true},
		{"hyphen", "database-ready", true},
		{"space", "database ready", true},
		{"hash", "#database", true},
		{"dot", "database.ready", true},
		{"underscore", "database_ready", true},
		{"leading digit", "2database", true},
		{"only digits", "123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidResolverName(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
