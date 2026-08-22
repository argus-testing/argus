package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ace-foundry/argus-testing/argus/internal/domain"
)

func TestValidateSecretBindingsUsesOneSharedContract(t *testing.T) {
	if err := domain.ValidateSecretBindings(map[string]string{"login_password": "private"}); err != nil {
		t.Fatal(err)
	}
	for name, bindings := range map[string]map[string]string{
		"invalid name":    {"not a binding": "private"},
		"empty value":     {"binding": ""},
		"oversized value": {"binding": strings.Repeat("x", domain.MaxSecretBindingBytes+1)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := domain.ValidateSecretBindings(bindings); !errors.Is(err, domain.ErrInvalidSecretBindings) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
