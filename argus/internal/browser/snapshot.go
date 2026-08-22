package browser

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrStaleElement      = errors.New("element reference is stale")
	ErrUnknownElement    = errors.New("element reference is unknown")
	ErrNavigationBlocked = errors.New("browser navigation was blocked by run policy")
)

// Element is the bounded, model-visible description of one interactive node.
// Ref is meaningful only until the next page inspection or navigation.
type Element struct {
	Ref         string `json:"ref"`
	Tag         string `json:"tag"`
	Role        string `json:"role,omitempty"`
	Name        string `json:"name,omitempty"`
	Label       string `json:"label,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	InputType   string `json:"input_type,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	Checked     bool   `json:"checked,omitempty"`
	Selected    string `json:"selected,omitempty"`
	Mutating    bool   `json:"mutating,omitempty"`
	Destructive bool   `json:"destructive,omitempty"`
}

type PageSnapshot struct {
	URL      string    `json:"url"`
	Title    string    `json:"title"`
	Text     string    `json:"text"`
	Width    int       `json:"width"`
	Height   int       `json:"height"`
	Elements []Element `json:"elements"`
}

type ActionResult struct {
	URL string `json:"url"`
}

type WaitCondition struct {
	Text          string
	URL           string
	TimeoutMillis int
}

type NetworkError struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	Status int    `json:"status"`
}

type elementTarget struct {
	selector string
	element  Element
}

type elementRegistry struct {
	generation int
	targets    map[string]elementTarget
}

func newElementRegistry() *elementRegistry {
	return &elementRegistry{targets: make(map[string]elementTarget)}
}

func (r *elementRegistry) replace(targets []elementTarget) []string {
	r.generation++
	r.targets = make(map[string]elementTarget, len(targets))
	references := make([]string, len(targets))
	for index, target := range targets {
		reference := fmt.Sprintf("e%d-%d", r.generation, index+1)
		references[index] = reference
		target.element.Ref = reference
		r.targets[reference] = target
	}
	return references
}

func (r *elementRegistry) element(reference string) (Element, error) {
	target, err := r.resolve(reference)
	if err != nil {
		return Element{}, err
	}
	return target.element, nil
}

func (r *elementRegistry) invalidate() {
	r.generation++
	r.targets = make(map[string]elementTarget)
}

func (r *elementRegistry) resolve(reference string) (elementTarget, error) {
	target, ok := r.targets[reference]
	if ok {
		return target, nil
	}
	prefix := fmt.Sprintf("e%d-", r.generation)
	if reference != "" && !strings.HasPrefix(reference, prefix) {
		return elementTarget{}, ErrStaleElement
	}
	return elementTarget{}, ErrUnknownElement
}
