package portforward

import (
	"errors"
	"fmt"
)

const (
	Setting = "VPN_PORT_FORWARDING"
	yes     = "yes"
)

func CheckEnabled(value string) error {
	if value != yes && value != "no" {
		return errors.New("must be yes or no")
	}
	return nil
}

func Enabled(settings map[string]string) (bool, error) {
	value := settings[Setting]
	if value == "" {
		return false, nil
	}
	if err := CheckEnabled(value); err != nil {
		return false, fmt.Errorf("%s: %w", Setting, err)
	}
	return value == yes, nil
}
