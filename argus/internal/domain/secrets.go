package domain

import (
	"errors"
	"regexp"
)

const (
	MaxSecretBindings     = 20
	MaxSecretBindingBytes = 4 * 1024
)

var (
	ErrInvalidSecretBindings = errors.New("invalid secret bindings")
	secretBindingName        = regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_.-]{0,99}$")
)

func ValidSecretBindingName(name string) bool {
	return secretBindingName.MatchString(name)
}

func ValidateSecretBindings(bindings map[string]string) error {
	if len(bindings) > MaxSecretBindings {
		return ErrInvalidSecretBindings
	}
	for name, value := range bindings {
		if !ValidSecretBindingName(name) || value == "" || len(value) > MaxSecretBindingBytes {
			return ErrInvalidSecretBindings
		}
	}
	return nil
}
