package policy_test

import (
	"errors"
	"testing"

	"github.com/ace-foundry/argus-testing/argus/internal/domain"
	"github.com/ace-foundry/argus-testing/argus/internal/policy"
)

func TestPolicyRestrictsOriginsAndMutations(t *testing.T) {
	p, err := policy.New("https://app.example.test/start", domain.RunAuthorization{
		AllowedOrigins: []string{"https://accounts.example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, allowed := range []string{"https://app.example.test/a", "https://accounts.example.test/oauth"} {
		if err := p.CheckNavigation(allowed); err != nil {
			t.Errorf("CheckNavigation(%q) = %v", allowed, err)
		}
	}
	for _, denied := range []string{"http://169.254.169.254/latest/meta-data", "https://app.example.test.evil.invalid"} {
		if err := p.CheckNavigation(denied); !errors.Is(err, policy.ErrOriginDenied) {
			t.Errorf("CheckNavigation(%q) = %v, want ErrOriginDenied", denied, err)
		}
	}
	if err := p.CheckAction(policy.Action{Kind: policy.ActionSubmit}); !errors.Is(err, policy.ErrMutationDenied) {
		t.Fatalf("CheckAction() = %v, want ErrMutationDenied", err)
	}
}

func TestPolicyAllowsAuthorizedMutation(t *testing.T) {
	p, err := policy.New("https://app.example.test", domain.RunAuthorization{AllowMutations: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.CheckAction(policy.Action{Kind: policy.ActionSubmit}); err != nil {
		t.Fatalf("CheckAction() = %v", err)
	}
}

func TestPolicyRequiresExplicitAuthorityForDestructiveAction(t *testing.T) {
	p, err := policy.New("https://app.example.test", domain.RunAuthorization{AllowMutations: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.CheckAction(policy.Action{Kind: policy.ActionClick, Destructive: true}); !errors.Is(err, policy.ErrDestructiveDenied) {
		t.Fatalf("CheckAction() = %v, want ErrDestructiveDenied", err)
	}

	p, err = policy.New("https://app.example.test", domain.RunAuthorization{AllowMutations: true, AllowDestructive: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.CheckAction(policy.Action{Kind: policy.ActionClick, Destructive: true}); err != nil {
		t.Fatalf("explicit destructive CheckAction() = %v", err)
	}
}

func TestPolicyRejectsDestructiveAuthorityWithoutMutationAuthority(t *testing.T) {
	_, err := policy.New("https://app.example.test", domain.RunAuthorization{AllowDestructive: true})
	if !errors.Is(err, policy.ErrInvalidAuthorization) {
		t.Fatalf("New() = %v, want ErrInvalidAuthorization", err)
	}
}

func TestNewRejectsInvalidAllowedOrigin(t *testing.T) {
	for _, origin := range []string{"javascript:alert(1)", "https://user@example.test", "https://example.test/path", "https://example.test?x=1"} {
		if _, err := policy.New("https://app.example.test", domain.RunAuthorization{AllowedOrigins: []string{origin}}); !errors.Is(err, policy.ErrInvalidOrigin) {
			t.Errorf("New(%q) = %v, want ErrInvalidOrigin", origin, err)
		}
	}
}
