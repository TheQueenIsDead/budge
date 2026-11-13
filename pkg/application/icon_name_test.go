package application

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIconName(t *testing.T) {
	tests := []struct {
		name, icon, want string
	}{
		{"drops the prefix and capitalises", "bi-house", "House"},
		{"reads dashes as spaces", "bi-music-note-beamed", "Music note beamed"},
		{"leaves a name without the prefix as it is", "tools", "Tools"},
		{"says nothing for nothing", "", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, iconName(test.icon))
		})
	}
}
