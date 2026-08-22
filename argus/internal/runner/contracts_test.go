package runner

import (
	"context"
	"testing"

	"github.com/ace-foundry/argus-testing/argus/internal/agent"
	"github.com/ace-foundry/argus-testing/argus/internal/domain"
)

func TestPassWithoutEvidenceBecomesInconclusive(t *testing.T) {
	report := normalizeVerdict(
		CriticResult{Verdict: "passed", Summary: "looks good"},
		ExecutionResult{Cases: []CaseResult{{ID: "T1", Status: "passed"}}},
		EvidenceIndex{},
	)
	if report.Verdict != domain.ReportVerdictInconclusive {
		t.Fatalf("report = %#v", report)
	}
}

func TestEveryPassedCaseNeedsPositiveEvidence(t *testing.T) {
	evidence := EvidenceIndex{
		"/screenshots/run/shot-1.png": {Kind: "screenshot", Path: "/screenshots/run/shot-1.png"},
	}
	execution := ExecutionResult{Cases: []CaseResult{
		{ID: "T1", Status: "passed", Evidence: []string{"/screenshots/run/shot-1.png"}},
		{ID: "T2", Status: "passed"},
	}}
	if got := evidenceVerdict(execution, evidence); got != domain.ReportVerdictInconclusive {
		t.Fatalf("got = %s", got)
	}
	execution.Cases[1].Evidence = []string{"/screenshots/run/missing.png"}
	if got := evidenceVerdict(execution, evidence); got != domain.ReportVerdictInconclusive {
		t.Fatalf("missing evidence verdict = %s", got)
	}
}

func TestFailedExecutionCannotBeOverriddenByCritic(t *testing.T) {
	report := normalizeVerdict(
		CriticResult{Verdict: "passed", Summary: "looks good"},
		ExecutionResult{Cases: []CaseResult{{ID: "T1", Status: "failed", Findings: []string{"broken"}}}},
		EvidenceIndex{},
	)
	if report.Verdict != domain.ReportVerdictFailed {
		t.Fatalf("report = %#v", report)
	}
}

func TestStageContractsRejectUnknownFieldsAndDuplicateCases(t *testing.T) {
	var plan TestPlan
	if err := decodeContract("{\"cases\":[],\"unexpected\":true}", &plan); err == nil {
		t.Fatal("accepted unknown plan field")
	}
	var execution ExecutionResult
	if err := decodeContract("{\"cases\":[{\"id\":\"T1\",\"status\":\"passed\",\"steps\":[],\"findings\":[],\"evidence\":[]},{\"id\":\"T1\",\"status\":\"passed\",\"steps\":[],\"findings\":[],\"evidence\":[]}]}", &execution); err == nil {
		t.Fatal("accepted duplicate execution case IDs")
	}
}

func TestCompleteContractRepairsMalformedOutputOnce(t *testing.T) {
	provider := &scriptedProvider{responses: []agent.ModelResponse{
		response("{\"objective\":3}"),
		response("{\"objective\":\"Verify search\",\"features\":[\"Search\"],\"constraints\":[\"Read-only\"]}"),
	}}
	runner := New(nil, nil, Options{Provider: provider})
	var brief TestBrief
	encoded, err := runner.completeContract(
		context.Background(),
		spec("comprehender", comprehenderInstruction, runner.model, nil, true),
		"repair", textMessage("test"), "", &brief, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 || brief.Objective != "Verify search" || encoded == "" {
		t.Fatalf("requests/brief/encoded = %d/%#v/%q", len(provider.requests), brief, encoded)
	}
	lastMessage := provider.requests[1].Messages[len(provider.requests[1].Messages)-1]
	if lastMessage.Role != agent.RoleUser || lastMessage.Parts[0].Text == nil {
		t.Fatalf("repair message = %#v", lastMessage)
	}
}

func TestExecutionMustMatchPlannedCaseIDs(t *testing.T) {
	plan := TestPlan{Cases: []TestCase{{ID: "T1"}, {ID: "T2"}}}
	if err := validateExecutionAgainstPlan(plan, ExecutionResult{Cases: []CaseResult{{ID: "T1"}, {ID: "T3"}}}); err == nil {
		t.Fatal("accepted unplanned execution case")
	}
}
