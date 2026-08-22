package runner

import (
	"fmt"
	"strings"
	"testing"
)

func TestSecretsResolveWithoutEnteringEventsOrReports(t *testing.T) {
	input := map[string]string{"login_password": "correct horse battery staple"}
	secrets, err := newSecretSet(input)
	if err != nil {
		t.Fatal(err)
	}
	input["login_password"] = "changed by caller"
	if value, ok := secrets.Resolve("login_password"); !ok || value != "correct horse battery staple" {
		t.Fatalf("resolved = %q, %v", value, ok)
	}
	if got := secrets.Redact("failed with correct horse battery staple"); got != "failed with [REDACTED]" {
		t.Fatalf("redacted = %q", got)
	}
}

func TestSecretsRedactLongestValuesFirstAndZeroOnClose(t *testing.T) {
	secrets, err := newSecretSet(map[string]string{"short": "token", "long": "token-value"})
	if err != nil {
		t.Fatal(err)
	}
	if got := secrets.Redact("token-value then token"); got != "[REDACTED] then [REDACTED]" {
		t.Fatalf("redacted = %q", got)
	}
	values := make([][]byte, 0, len(secrets.values))
	for _, value := range secrets.values {
		values = append(values, value)
	}
	secrets.Close()
	for _, value := range values {
		for _, octet := range value {
			if octet != 0 {
				t.Fatalf("secret bytes were not zeroed: %v", value)
			}
		}
	}
	if _, ok := secrets.Resolve("short"); ok {
		t.Fatal("closed secret set still resolves values")
	}
}

func TestSecretsRejectInvalidOrOversizedBindings(t *testing.T) {
	tooMany := make(map[string]string, 21)
	for index := range 21 {
		tooMany[fmt.Sprintf("secret_%d", index)] = "value"
	}
	for name, bindings := range map[string]map[string]string{
		"empty name":      {"": "value"},
		"invalid name":    {"not a binding": "value"},
		"empty value":     {"secret": ""},
		"oversized value": {"secret": strings.Repeat("x", maxSecretBytes+1)},
		"too many":        tooMany,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := newSecretSet(bindings); err == nil {
				t.Fatal("newSecretSet error = nil")
			}
		})
	}
}
