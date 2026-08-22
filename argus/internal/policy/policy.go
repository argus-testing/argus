// Package policy enforces run-scoped navigation and mutation authority below
// the model/tool layer.
package policy

import (
	"errors"
	"net/url"
	"strings"

	"github.com/ace-foundry/argus-testing/argus/internal/domain"
)

var (
	ErrInvalidOrigin        = errors.New("invalid allowed origin")
	ErrInvalidAuthorization = errors.New("invalid run authorization")
	ErrOriginDenied         = errors.New("navigation origin is not authorized")
	ErrMutationDenied       = errors.New("state-changing action is not authorized")
	ErrDestructiveDenied    = errors.New("destructive action is not explicitly authorized")
)

type ActionKind string

const (
	ActionClick  ActionKind = "click"
	ActionType   ActionKind = "type"
	ActionSubmit ActionKind = "submit"
	ActionSelect ActionKind = "select"
)

type Action struct {
	Kind        ActionKind
	Mutating    bool
	Destructive bool
}

type Policy struct {
	origins          map[string]struct{}
	allowMutations   bool
	allowDestructive bool
}

func New(target string, authorization domain.RunAuthorization) (*Policy, error) {
	if authorization.AllowDestructive && !authorization.AllowMutations {
		return nil, ErrInvalidAuthorization
	}
	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, ErrInvalidOrigin
	}
	initial, ok := canonicalOrigin(targetURL)
	if !ok {
		return nil, ErrInvalidOrigin
	}
	origins := map[string]struct{}{initial: {}}
	for _, value := range authorization.AllowedOrigins {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, ErrInvalidOrigin
		}
		origin, ok := canonicalOrigin(parsed)
		if !ok {
			return nil, ErrInvalidOrigin
		}
		origins[origin] = struct{}{}
	}
	return &Policy{
		origins:          origins,
		allowMutations:   authorization.AllowMutations,
		allowDestructive: authorization.AllowDestructive,
	}, nil
}

func (p *Policy) CheckNavigation(target string) error {
	parsed, err := url.Parse(target)
	if err != nil {
		return ErrOriginDenied
	}
	origin, ok := canonicalOrigin(parsed)
	if !ok {
		return ErrOriginDenied
	}
	if _, ok := p.origins[origin]; !ok {
		return ErrOriginDenied
	}
	return nil
}

func (p *Policy) CheckAction(action Action) error {
	destructive := action.Destructive
	mutating := action.Mutating || action.Kind == ActionSubmit || destructive
	if mutating && !p.allowMutations {
		return ErrMutationDenied
	}
	if destructive && !p.allowDestructive {
		return ErrDestructiveDenied
	}
	return nil
}

func canonicalOrigin(value *url.URL) (string, bool) {
	if value == nil || value.User != nil || value.Hostname() == "" || value.Scheme != "http" && value.Scheme != "https" {
		return "", false
	}
	host := strings.ToLower(value.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := value.Port()
	if port != "" && !(value.Scheme == "http" && port == "80") && !(value.Scheme == "https" && port == "443") {
		host += ":" + port
	}
	return strings.ToLower(value.Scheme) + "://" + host, true
}
