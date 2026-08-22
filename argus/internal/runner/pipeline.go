package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/ace-foundry/argus-testing/argus/internal/agent"
	"github.com/ace-foundry/argus-testing/argus/internal/browser"
	"github.com/ace-foundry/argus-testing/argus/internal/domain"
	"github.com/ace-foundry/argus-testing/argus/internal/server"
)

const maxReportText = 4000

type browserAdapter struct {
	runner  *Runner
	runID   string
	session browser.Session
}

func (a *browserAdapter) inspect(ctx context.Context, _ map[string]any, _ agent.ToolContext) (any, error) {
	value, err := a.session.Inspect(ctx)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return map[string]any{"url": value.URL, "title": bounded(value.Title, 1000), "text": value.Text, "width": value.Width, "height": value.Height, "elements": value.Elements}, nil
}
func (a *browserAdapter) click(ctx context.Context, values map[string]any, _ agent.ToolContext) (any, error) {
	reference, err := requiredString(values, "ref", 100)
	if err != nil {
		return nil, err
	}
	result, err := a.session.Click(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return map[string]any{"url": result.URL, "result": "clicked"}, nil
}
func (a *browserAdapter) navigate(ctx context.Context, values map[string]any, _ agent.ToolContext) (any, error) {
	target, err := requiredString(values, "url", 2000)
	if err != nil {
		return nil, err
	}
	if err := server.ValidateURL(target); err != nil {
		return nil, err
	}
	value, err := a.session.Navigate(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return map[string]any{"url": value.URL, "title": bounded(value.Title, 1000)}, nil
}
func (a *browserAdapter) screenshot(ctx context.Context, values map[string]any, _ agent.ToolContext) (any, error) {
	label := "Evidence"
	if value, ok := values["label"]; ok {
		text, ok := value.(string)
		if !ok || utf8.RuneCountInString(text) > 80 {
			return nil, fmt.Errorf("invalid screenshot label")
		}
		label = text
	}
	capture, err := a.capture(ctx, label, true)
	if err != nil {
		return nil, err
	}
	return capture.result, nil
}

type screenshotCapture struct {
	result map[string]any
	data   []byte
}

func (a *browserAdapter) capture(ctx context.Context, label string, emitEvent bool) (screenshotCapture, error) {
	publicPath, diskPath, err := NextScreenshotPath(a.runner.screenshotDir, a.runID, label)
	if err != nil {
		return screenshotCapture{}, fmt.Errorf("browser: %w", err)
	}
	if err := a.session.Screenshot(ctx, diskPath); err != nil {
		return screenshotCapture{}, fmt.Errorf("browser: %w", err)
	}
	data, err := os.ReadFile(diskPath)
	if err != nil {
		return screenshotCapture{}, fmt.Errorf("browser: %w", err)
	}
	if _, err := a.runner.store.AddScreenshot(a.runID, publicPath); err != nil {
		return screenshotCapture{}, err
	}
	result := map[string]any{"path": publicPath, "label": bounded(label, 80)}
	if emitEvent {
		if err := a.runner.addEvent(a.runID, domain.EventBrowserScreenshot, result); err != nil {
			return screenshotCapture{}, err
		}
	}
	return screenshotCapture{result: result, data: data}, nil
}

func requiredString(values map[string]any, name string, maximum int) (string, error) {
	value, ok := values[name].(string)
	if !ok || value == "" || utf8.RuneCountInString(value) > maximum {
		return "", fmt.Errorf("invalid %s", name)
	}
	return value, nil
}
func parseValidator(text string) (bool, string) {
	var value struct {
		Testable any    `json:"testable"`
		Reason   string `json:"reason"`
	}
	if json.Unmarshal([]byte(unfence(text)), &value) != nil {
		return true, ""
	}
	if testable, ok := value.Testable.(bool); ok && !testable {
		return false, bounded(value.Reason, maxReportText)
	}
	return true, ""
}

func parseReport(text, plan, observations string) *domain.RunReport {
	var input map[string]any
	if err := json.Unmarshal([]byte(unfence(text)), &input); err != nil {
		return normalizedReport(text, plan, observations, "")
	}
	verdict, _ := input["verdict"].(string)
	if !validVerdict(verdict) {
		return normalizedReport(text, plan, observations, "")
	}
	summary, ok := input["summary"].(string)
	if !ok {
		summary = observations
	}
	return &domain.RunReport{Verdict: domain.ReportVerdict(verdict), Summary: bounded(summary, maxReportText), Plan: stringPointer(bounded(plan, 12000)), Findings: normalizeFindings(input["findings"]), Recommendations: normalizeRecommendations(input["recommendations"])}
}
func normalizedReport(text, plan, observations, reason string) *domain.RunReport {
	summary := reason
	if summary == "" {
		summary = text
	}
	if summary == "" {
		summary = observations
	}
	if summary == "" {
		summary = "No conclusive QA result was returned."
	}
	return &domain.RunReport{Verdict: domain.ReportVerdictInconclusive, Summary: bounded(summary, maxReportText), Plan: stringPointer(bounded(plan, 12000)), Findings: []domain.Finding{}, Recommendations: []string{}}
}
func validVerdict(value string) bool {
	return value == string(domain.ReportVerdictPassed) || value == string(domain.ReportVerdictFailed) || value == string(domain.ReportVerdictInconclusive)
}
func normalizeFindings(value any) []domain.Finding {
	values, _ := value.([]any)
	out := make([]domain.Finding, 0, len(values))
	for _, value := range values {
		finding, ok := value.(map[string]any)
		if !ok || len(out) == 50 {
			continue
		}
		severity, severityOK := finding["severity"].(string)
		title, titleOK := finding["title"].(string)
		detail, detailOK := finding["detail"].(string)
		if !severityOK || !titleOK || !detailOK {
			continue
		}
		out = append(out, domain.Finding{Severity: bounded(severity, 80), Title: bounded(title, 500), Detail: bounded(detail, maxReportText)})
	}
	return out
}
func normalizeRecommendations(value any) []string {
	values, _ := value.([]any)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if recommendation, ok := value.(string); ok && len(out) < 50 {
			out = append(out, bounded(recommendation, 1000))
		}
	}
	return out
}
func unfence(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "```") {
		if line := strings.IndexByte(value, '\n'); line >= 0 {
			value = value[line+1:]
		}
		if end := strings.LastIndex(value, "```"); end >= 0 {
			value = value[:end]
		}
	}
	return strings.TrimSpace(value)
}
func bounded(value string, maximum int) string {
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	return string([]rune(value)[:maximum])
}
func stringPointer(value string) *string { return &value }
