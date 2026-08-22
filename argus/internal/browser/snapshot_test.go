package browser

import (
	"errors"
	"testing"
)

func TestElementRegistryUsesGenerationScopedReferences(t *testing.T) {
	registry := newElementRegistry()
	first := registry.replace([]elementTarget{{selector: `[data-argus-ref="one"]`}, {selector: `[data-argus-ref="two"]`}})
	if first[0] != "e1-1" || first[1] != "e1-2" {
		t.Fatalf("first references = %#v", first)
	}
	if target, err := registry.resolve("e1-2"); err != nil || target.selector != `[data-argus-ref="two"]` {
		t.Fatalf("resolved = %#v, %v", target, err)
	}

	second := registry.replace([]elementTarget{{selector: `[data-argus-ref="three"]`}})
	if second[0] != "e2-1" {
		t.Fatalf("second references = %#v", second)
	}
	if _, err := registry.resolve("e1-1"); !errors.Is(err, ErrStaleElement) {
		t.Fatalf("stale resolve = %v", err)
	}
}

func TestElementRegistryRejectsUnknownReference(t *testing.T) {
	registry := newElementRegistry()
	registry.replace(nil)
	if _, err := registry.resolve("e1-99"); !errors.Is(err, ErrUnknownElement) {
		t.Fatalf("resolve = %v", err)
	}
}
