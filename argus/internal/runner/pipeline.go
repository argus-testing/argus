package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/ace-foundry/argus-testing/argus/internal/agent"
	"github.com/ace-foundry/argus-testing/argus/internal/browser"
	"github.com/ace-foundry/argus-testing/argus/internal/domain"
	"github.com/ace-foundry/argus-testing/argus/internal/policy"
	"github.com/ace-foundry/argus-testing/argus/internal/server"
)

const maxReportText = 4000

type browserAdapter struct {
	runner  *Runner
	runID   string
	session browser.Session
	policy  *policy.Policy
	secrets *secretSet
}

func (a *browserAdapter) inspect(ctx context.Context, _ map[string]any, _ agent.ToolContext) (any, error) {
	value, err := a.session.Inspect(ctx)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return a.snapshot(value), nil
}
func (a *browserAdapter) click(ctx context.Context, values map[string]any, _ agent.ToolContext) (any, error) {
	reference, err := requiredString(values, "ref", 100)
	if err != nil {
		return nil, err
	}
	if err := a.checkElementAction(ctx, reference, policy.ActionClick); err != nil {
		return nil, err
	}
	result, err := a.session.Click(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return a.afterAction(ctx, "clicked", result)
}
func (a *browserAdapter) typeText(ctx context.Context, values map[string]any, _ agent.ToolContext) (any, error) {
	reference, err := requiredString(values, "ref", 100)
	if err != nil {
		return nil, err
	}
	value, err := a.inputValue(values)
	if err != nil {
		return nil, err
	}
	if err := a.checkElementAction(ctx, reference, policy.ActionType); err != nil {
		return nil, err
	}
	result, err := a.session.Type(ctx, reference, value)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return a.afterAction(ctx, "typed", result)
}
func (a *browserAdapter) fillForm(ctx context.Context, values map[string]any, _ agent.ToolContext) (any, error) {
	rawFields, ok := values["fields"].([]any)
	if !ok || len(rawFields) == 0 || len(rawFields) > 20 {
		return nil, errors.New("invalid fields")
	}
	fields := make(map[string]browser.InputValue, len(rawFields))
	for _, rawField := range rawFields {
		field, ok := rawField.(map[string]any)
		if !ok {
			return nil, errors.New("invalid field")
		}
		reference, err := requiredString(field, "ref", 100)
		if err != nil {
			return nil, err
		}
		if _, duplicate := fields[reference]; duplicate {
			return nil, errors.New("duplicate field reference")
		}
		value, err := a.inputValue(field)
		if err != nil {
			return nil, err
		}
		if err := a.checkElementAction(ctx, reference, policy.ActionType); err != nil {
			return nil, err
		}
		fields[reference] = value
	}
	result, err := a.session.FillForm(ctx, fields)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return a.afterAction(ctx, "form filled", result)
}
func (a *browserAdapter) submitForm(ctx context.Context, values map[string]any, _ agent.ToolContext) (any, error) {
	reference, err := requiredString(values, "ref", 100)
	if err != nil {
		return nil, err
	}
	if err := a.checkElementAction(ctx, reference, policy.ActionSubmit); err != nil {
		return nil, err
	}
	result, err := a.session.Submit(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return a.afterAction(ctx, "form submitted", result)
}
func (a *browserAdapter) selectOption(ctx context.Context, values map[string]any, _ agent.ToolContext) (any, error) {
	reference, err := requiredString(values, "ref", 100)
	if err != nil {
		return nil, err
	}
	value, err := requiredString(values, "value", 1_000)
	if err != nil {
		return nil, err
	}
	if err := a.checkElementAction(ctx, reference, policy.ActionSelect); err != nil {
		return nil, err
	}
	result, err := a.session.Select(ctx, reference, value)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return a.afterAction(ctx, "option selected", result)
}
func (a *browserAdapter) pressKey(ctx context.Context, values map[string]any, _ agent.ToolContext) (any, error) {
	key, err := requiredString(values, "key", 100)
	if err != nil {
		return nil, err
	}
	if a.policy != nil {
		action := policy.Action{Kind: policy.ActionClick, Mutating: !readOnlyKey(key)}
		if err := a.policy.CheckAction(action); err != nil {
			return nil, err
		}
	}
	result, err := a.session.Press(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return a.afterAction(ctx, "key pressed", result)
}
func (a *browserAdapter) scroll(ctx context.Context, values map[string]any, _ agent.ToolContext) (any, error) {
	delta, err := requiredInt(values, "delta_y")
	if err != nil {
		return nil, err
	}
	result, err := a.session.Scroll(ctx, delta)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return a.afterAction(ctx, "scrolled", result)
}
func (a *browserAdapter) resize(ctx context.Context, values map[string]any, _ agent.ToolContext) (any, error) {
	width, err := requiredInt(values, "width")
	if err != nil {
		return nil, err
	}
	height, err := requiredInt(values, "height")
	if err != nil {
		return nil, err
	}
	result, err := a.session.Resize(ctx, width, height)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return a.afterAction(ctx, "viewport resized", result)
}
func (a *browserAdapter) waitFor(ctx context.Context, values map[string]any, _ agent.ToolContext) (any, error) {
	condition := browser.WaitCondition{}
	if value, ok := values["text"]; ok {
		text, ok := value.(string)
		if !ok || utf8.RuneCountInString(text) > 1_000 {
			return nil, errors.New("invalid text")
		}
		condition.Text = text
	}
	if value, ok := values["url"]; ok {
		target, ok := value.(string)
		if !ok || utf8.RuneCountInString(target) > 2_000 {
			return nil, errors.New("invalid url")
		}
		condition.URL = target
	}
	if value, ok := values["timeout_ms"]; ok {
		timeout, err := integer(value)
		if err != nil {
			return nil, errors.New("invalid timeout_ms")
		}
		condition.TimeoutMillis = timeout
	}
	result, err := a.session.Wait(ctx, condition)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return a.afterAction(ctx, "wait completed", result)
}
func (a *browserAdapter) consoleErrors(ctx context.Context, _ map[string]any, _ agent.ToolContext) (any, error) {
	values, err := a.session.ConsoleErrors(ctx)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	for index := range values {
		values[index] = bounded(a.redact(values[index]), 1_000)
	}
	return map[string]any{"errors": values}, nil
}
func (a *browserAdapter) networkErrors(ctx context.Context, _ map[string]any, _ agent.ToolContext) (any, error) {
	values, err := a.session.NetworkErrors(ctx)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return map[string]any{"errors": values}, nil
}
func (a *browserAdapter) navigate(ctx context.Context, values map[string]any, _ agent.ToolContext) (any, error) {
	target, err := requiredString(values, "url", 2000)
	if err != nil {
		return nil, err
	}
	if err := server.ValidateURL(target); err != nil {
		return nil, err
	}
	if a.policy != nil {
		if err := a.policy.CheckNavigation(target); err != nil {
			return nil, err
		}
	}
	value, err := a.session.Navigate(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	snapshot, err := a.session.Inspect(ctx)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return map[string]any{"action": "navigated", "url": value.URL, "title": bounded(a.redact(value.Title), 1_000), "snapshot": a.snapshot(snapshot)}, nil
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

func (a *browserAdapter) checkElementAction(ctx context.Context, reference string, kind policy.ActionKind) error {
	element, err := a.session.Element(ctx, reference)
	if err != nil {
		return fmt.Errorf("browser: %w", err)
	}
	if a.policy == nil {
		return nil
	}
	return a.policy.CheckAction(policy.Action{Kind: kind, Mutating: element.Mutating, Destructive: element.Destructive})
}

func (a *browserAdapter) afterAction(ctx context.Context, action string, result browser.ActionResult) (any, error) {
	snapshot, err := a.session.Inspect(ctx)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	return map[string]any{"action": action, "url": result.URL, "snapshot": a.snapshot(snapshot)}, nil
}

func (a *browserAdapter) snapshot(value browser.PageSnapshot) map[string]any {
	elements := append([]browser.Element(nil), value.Elements...)
	for index := range elements {
		elements[index].Name = a.redact(elements[index].Name)
		elements[index].Label = a.redact(elements[index].Label)
		elements[index].Placeholder = a.redact(elements[index].Placeholder)
		elements[index].Selected = a.redact(elements[index].Selected)
	}
	return map[string]any{
		"url": value.URL, "title": bounded(a.redact(value.Title), 1_000),
		"text": a.redact(value.Text), "width": value.Width, "height": value.Height,
		"elements": elements,
	}
}

func (a *browserAdapter) inputValue(values map[string]any) (browser.InputValue, error) {
	text, hasText := values["text"]
	secret, hasSecret := values["secret"]
	if hasText == hasSecret {
		return browser.InputValue{}, errors.New("exactly one of text or secret is required")
	}
	if hasText {
		value, ok := text.(string)
		if !ok || len(value) > maxSecretBytes {
			return browser.InputValue{}, errors.New("invalid text")
		}
		return browser.InputValue{Text: value}, nil
	}
	name, ok := secret.(string)
	if !ok || !secretBindingName.MatchString(name) {
		return browser.InputValue{}, errors.New("invalid secret binding")
	}
	value, ok := a.secrets.Resolve(name)
	if !ok {
		return browser.InputValue{}, errors.New("unknown secret binding")
	}
	return browser.InputValue{Text: value, Sensitive: true}, nil
}

func (a *browserAdapter) redact(value string) string {
	if a.secrets == nil {
		return value
	}
	return a.secrets.Redact(value)
}

func readOnlyKey(key string) bool {
	switch key {
	case "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "PageUp", "PageDown", "Home", "End", "Tab", "Shift+Tab", "Escape":
		return true
	default:
		return false
	}
}

func requiredInt(values map[string]any, name string) (int, error) {
	value, ok := values[name]
	if !ok {
		return 0, fmt.Errorf("invalid %s", name)
	}
	result, err := integer(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return result, nil
}

func integer(value any) (int, error) {
	switch value := value.(type) {
	case int:
		return value, nil
	case float64:
		converted := int(value)
		if float64(converted) != value {
			return 0, errors.New("not an integer")
		}
		return converted, nil
	default:
		return 0, errors.New("not an integer")
	}
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
