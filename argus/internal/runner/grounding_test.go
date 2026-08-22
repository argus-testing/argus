package runner

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ace-foundry/argus-testing/argus/internal/agent"
	"github.com/ace-foundry/argus-testing/argus/internal/browser"
	"github.com/ace-foundry/argus-testing/argus/internal/domain"
	"github.com/ace-foundry/argus-testing/argus/internal/policy"
)

type fakeGrounder struct {
	requests []GroundingRequest
	matches  []VisualMatch
	err      error
}

func (g *fakeGrounder) Locate(_ context.Context, request GroundingRequest) ([]VisualMatch, error) {
	g.requests = append(g.requests, request)
	return append([]VisualMatch(nil), g.matches...), g.err
}

func TestGroundingRejectsInvalidAndLowConfidenceMatches(t *testing.T) {
	for _, match := range []VisualMatch{
		{X: -1, Y: 10, Confidence: .9},
		{X: 20, Y: 20, Confidence: .4},
		{X: 900, Y: 20, Confidence: .9},
		{X: 20, Y: 700, Confidence: .9},
	} {
		if _, err := validateMatch(match, 800, 600, .70); err == nil {
			t.Fatalf("accepted %#v", match)
		}
	}
	got, err := validateMatch(VisualMatch{X: 20, Y: 30, Confidence: .91, Description: "Search"}, 800, 600, .70)
	if err != nil || got.X != 20 || got.Description != "Search" {
		t.Fatalf("got = %#v, %v", got, err)
	}
}

func TestGroundingBoundsDescriptionsAndResultCounts(t *testing.T) {
	matches := make([]VisualMatch, 11)
	for index := range matches {
		matches[index] = VisualMatch{X: index, Y: index, Confidence: .9}
	}
	if _, err := validateMatches(matches, 800, 600, .70, 10); err == nil {
		t.Fatal("accepted too many visual matches")
	}
	if _, err := validateMatch(VisualMatch{X: 1, Y: 1, Confidence: .9, Description: string(make([]rune, 501))}, 800, 600, .70); err == nil {
		t.Fatal("accepted oversized match description")
	}
}

func TestFindElementsReturnsValidatedMatchesWithFreshImage(t *testing.T) {
	db, _ := newTestStore(t)
	run, err := db.CreateRun("https://example.com", "find search", domain.RunPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	grounder := &fakeGrounder{matches: []VisualMatch{{X: 25, Y: 30, Confidence: .94, Description: "Search"}}}
	session := &fakeSession{screenshotData: [][]byte{[]byte("fresh-png")}}
	runner := &Runner{store: db, screenshotDir: filepath.Join(t.TempDir(), "screenshots"), grounder: grounder}
	adapter := &browserAdapter{runner: runner, runID: run.ID, session: session}

	value, err := adapter.findElements(context.Background(), map[string]any{"description": "Search", "limit": float64(3)}, agent.ToolContext{})
	if err != nil {
		t.Fatal(err)
	}
	output, ok := value.(agent.ToolOutput)
	if !ok || len(output.Followup) != 2 || output.Followup[1].Image == nil || string(output.Followup[1].Image.Data) != "fresh-png" {
		t.Fatalf("tool output = %#v", value)
	}
	encoded, _ := json.Marshal(output.Result)
	if !strings.Contains(string(encoded), "\"x\":25") || !strings.Contains(string(encoded), "\"confidence\":0.94") {
		t.Fatalf("result = %s", encoded)
	}
	if len(grounder.requests) != 1 || grounder.requests[0].Width != 800 || grounder.requests[0].Height != 600 || grounder.requests[0].Limit != 3 {
		t.Fatalf("requests = %#v", grounder.requests)
	}
}

func TestVisualClickCannotBypassElementPolicy(t *testing.T) {
	db, _ := newTestStore(t)
	run, err := db.CreateRun("https://example.com", "click save", domain.RunPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	grounder := &fakeGrounder{matches: []VisualMatch{{X: 25, Y: 30, Confidence: .94, Description: "Save"}}}
	session := &fakeSession{
		screenshotData: [][]byte{[]byte("fresh-png")},
		pointElement:   browser.Element{Name: "Save", Mutating: true},
	}
	runPolicy, err := policy.New("https://example.com", domain.RunAuthorization{})
	if err != nil {
		t.Fatal(err)
	}
	runner := &Runner{store: db, screenshotDir: filepath.Join(t.TempDir(), "screenshots"), grounder: grounder}
	adapter := &browserAdapter{runner: runner, runID: run.ID, session: session, policy: runPolicy}

	if _, err := adapter.visualClick(context.Background(), map[string]any{"description": "Save"}, agent.ToolContext{}); !errors.Is(err, policy.ErrMutationDenied) {
		t.Fatalf("visual click error = %v", err)
	}
	if session.pointClicks != 0 {
		t.Fatalf("coordinate click calls = %d", session.pointClicks)
	}
}

func TestVisualClickRejectsLowConfidenceWithoutClicking(t *testing.T) {
	db, _ := newTestStore(t)
	run, err := db.CreateRun("https://example.com", "click search", domain.RunPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	grounder := &fakeGrounder{matches: []VisualMatch{{X: 25, Y: 30, Confidence: .2, Description: "Search"}}}
	session := &fakeSession{screenshotData: [][]byte{[]byte("fresh-png")}}
	runner := &Runner{store: db, screenshotDir: filepath.Join(t.TempDir(), "screenshots"), grounder: grounder}
	adapter := &browserAdapter{runner: runner, runID: run.ID, session: session}
	if _, err := adapter.visualClick(context.Background(), map[string]any{"description": "Search"}, agent.ToolContext{}); err == nil {
		t.Fatal("visual click accepted low-confidence match")
	}
	if session.pointClicks != 0 {
		t.Fatalf("coordinate click calls = %d", session.pointClicks)
	}
}

func TestVisualClickReturnsPostActionScreenshotForSafeTarget(t *testing.T) {
	db, _ := newTestStore(t)
	run, err := db.CreateRun("https://example.com", "click search", domain.RunPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	grounder := &fakeGrounder{matches: []VisualMatch{{X: 25, Y: 30, Confidence: .94, Description: "Search"}}}
	session := &fakeSession{
		screenshotData: [][]byte{[]byte("target-png"), []byte("after-png")},
		pointElement:   browser.Element{Name: "Search"},
	}
	runPolicy, err := policy.New("https://example.com", domain.RunAuthorization{})
	if err != nil {
		t.Fatal(err)
	}
	runner := &Runner{store: db, screenshotDir: filepath.Join(t.TempDir(), "screenshots"), grounder: grounder}
	adapter := &browserAdapter{runner: runner, runID: run.ID, session: session, policy: runPolicy}
	value, err := adapter.visualClick(context.Background(), map[string]any{"description": "Search"}, agent.ToolContext{})
	if err != nil {
		t.Fatal(err)
	}
	output, ok := value.(agent.ToolOutput)
	if !ok || len(output.Followup) != 2 || output.Followup[1].Image == nil || string(output.Followup[1].Image.Data) != "after-png" {
		t.Fatalf("tool output = %#v", value)
	}
	if session.pointClicks != 1 || session.screenshotCount != 2 {
		t.Fatalf("clicks/screenshots = %d/%d", session.pointClicks, session.screenshotCount)
	}
}

func TestGroundingToolsAreAdvertisedOnlyWhenConfigured(t *testing.T) {
	configured := browserTools(&browserAdapter{runner: &Runner{grounder: &fakeGrounder{}}}, true)
	names := make(map[string]bool, len(configured))
	for _, tool := range configured {
		names[tool.Name] = true
	}
	if !names["find_elements"] || !names["visual_click"] {
		t.Fatalf("configured tools = %#v", names)
	}
	unconfigured := browserTools(&browserAdapter{}, true)
	for _, tool := range unconfigured {
		if tool.Name == "find_elements" || tool.Name == "visual_click" {
			t.Fatalf("unconfigured grounding tool = %q", tool.Name)
		}
	}
}
