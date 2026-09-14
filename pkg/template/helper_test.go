package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidResolverName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"camelCase", "databaseReady", false},
		{"PascalCase", "DatabaseReady", false},
		{"with digits", "database2Ready", false},
		{"underscore", "database_ready", false},
		{"multiple underscores", "database__ready", false},

		{"empty", "", true},
		{"leading digit", "2database", true},
		{"only digits", "123", true},
		{"leading underscore", "_database", true},
		{"hyphen", "database-ready", true},
		{"space", "database ready", true},
		{"hash", "#database", true},
		{"dot", "database.ready", true},
		{"slash", "database/ready", true},
		{"emoji", "database🚀", true},
		{"unicode", "dátabase", true},
		{"newline", "database\nready", true},
		{"tab", "database\tready", true},
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
