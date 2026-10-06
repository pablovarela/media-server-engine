package create

import (
	"fmt"
	"regexp"
)

var validName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func ValidName(name string) error {
	if validName.MatchString(name) {
		return nil
	}
	return fmt.Errorf("the installation name %q must start with a lowercase letter and have only lowercase letters, digits and -, at most 32 characters", name)
}
