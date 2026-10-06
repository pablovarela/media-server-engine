package create

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidName(t *testing.T) {
	for _, name := range []string{"gorgon", "a", "media-2", "a" + strings.Repeat("b", 31)} {
		assert.NoError(t, ValidName(name), name)
	}
	for _, name := range []string{"", "2gorgon", "-gorgon", "Gorgon", "gor_gon", "gor gon", "gor.gon", "a" + strings.Repeat("b", 32)} {
		assert.EqualError(t, ValidName(name), "the installation name "+quoted(name)+" must start with a lowercase letter and have only lowercase letters, digits and -, at most 32 characters", name)
	}
}

func quoted(name string) string { return "\"" + name + "\"" }
