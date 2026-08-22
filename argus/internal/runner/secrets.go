package runner

import (
	"bytes"
	"sort"
	"sync"

	"github.com/ace-foundry/argus-testing/argus/internal/domain"
)

const (
	maxSecretBytes = domain.MaxSecretBindingBytes
)

var errInvalidSecretBinding = domain.ErrInvalidSecretBindings

// secretSet owns a private copy of the ephemeral bindings supplied for one run.
// Values are never exposed as a collection and are zeroed when the run exits.
type secretSet struct {
	mu              sync.RWMutex
	values          map[string][]byte
	redactionValues [][]byte
	closed          bool
}

func newSecretSet(bindings map[string]string) (*secretSet, error) {
	if err := domain.ValidateSecretBindings(bindings); err != nil {
		return nil, errInvalidSecretBinding
	}
	set := &secretSet{values: make(map[string][]byte, len(bindings))}
	for name, value := range bindings {
		copied := append([]byte(nil), value...)
		set.values[name] = copied
		set.redactionValues = append(set.redactionValues, copied)
	}
	sort.Slice(set.redactionValues, func(left, right int) bool {
		return len(set.redactionValues[left]) > len(set.redactionValues[right])
	})
	return set, nil
}

func (s *secretSet) Resolve(name string) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", false
	}
	value, ok := s.values[name]
	if !ok {
		return "", false
	}
	return string(value), true
}

func (s *secretSet) Redact(value string) string {
	if s == nil || value == "" {
		return value
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return value
	}
	redacted := []byte(value)
	for _, secret := range s.redactionValues {
		redacted = bytes.ReplaceAll(redacted, secret, []byte("[REDACTED]"))
	}
	return string(redacted)
}

func (s *secretSet) Names() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil
	}
	names := make([]string, 0, len(s.values))
	for name := range s.values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *secretSet) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	for _, value := range s.values {
		clear(value)
	}
	clear(s.redactionValues)
	s.redactionValues = nil
	s.values = nil
	s.closed = true
}
