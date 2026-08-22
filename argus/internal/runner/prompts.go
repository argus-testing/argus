package runner

import (
	"encoding/json"

	"github.com/ace-foundry/argus-testing/argus/internal/agent"
)

// These are the public QA prompt contracts. They deliberately prohibit handling
// credentials and destructive actions even when supplied in user instructions.
const validatorInstruction = `You are a Request Validator for a web-app QA testing agent. Decide whether the request is to test a web application. Greetings, small talk, general questions, off-topic or meaningless requests are not testable. A URL alone is testable and borderline actionable requests are testable. Respond ONLY with JSON: {"testable":true,"reason":"short reason"}.`

const comprehenderInstruction = `You are a QA Intent Comprehender. Produce a structured JSON test brief covering explicit features, navigation needs, scope, implicit verification requirements, and every hard user constraint (verification_override, method_hint, scope_limit, avoid). Respond ONLY with JSON. Never extract, retain, request, enter, or use credentials, passwords, API keys, tokens, or other secrets. If credentials are supplied, state that authenticated testing requires a safe preconfigured session and test only public flows.`

const explorerInstruction = `You are a QA App Explorer. Use inspect_page before actions to build a focused JSON app map of relevant pages, forms, features, navigation, feedback patterns, mapped and unmapped features. Budget at most 8 interactions/navigation actions. Do not log in, enter credentials or secrets, submit forms, or perform destructive actions. Respect all hard user constraints. Respond ONLY with JSON.`

const strategistInstruction = `You are a QA Test Strategist. Produce ONLY JSON test plan grounded in the app map. Cover requested features, use real observed element references and pages, include preconditions and a success goal for each test. Carry every applicable user constraint as a hard requirement: verification_override, method_hint, scope_limit and avoid. Never plan credential entry, destructive actions, or irreversible state changes.`

const executorInstruction = `You are a QA Test Executor. Execute only safe, authorized QA steps with browser tools and return ONLY JSON test results. Inspect before any action. Never enter or store credentials, passwords, API keys, tokens or secrets; never submit destructive or irreversible actions. Respect every hard user constraint. After every significant action (click or navigation), inspect_page or screenshot to verify it. Capture screenshots after significant actions. A pass requires positive evidence; no error alone is inconclusive.`

const criticInstruction = `You are an independent QA Critic. Review the request, brief, app map, plan and execution evidence. Use inspect_page and screenshot if needed. Return ONLY JSON: {"verdict":"passed|failed|inconclusive","summary":"...","findings":[{"severity":"...","title":"...","detail":"..."}],"recommendations":["..."]}. Passed requires all requested features to be meaningfully tested and pass; failed requires core failure; otherwise inconclusive. Never expose credentials, secrets, or page contents beyond the final report.`

func spec(name, instruction string, model agent.ModelRef, tools []agent.Tool, jsonMode bool) agent.AgentSpec {
	return agent.AgentSpec{Name: name, Model: model, Instruction: instruction, Tools: tools, Generation: agent.GenerationOptions{JSONMode: jsonMode}}
}

func browserTools(adapter *browserAdapter, includeActions bool) []agent.Tool {
	inspect := agent.Tool{Name: "inspect_page", Description: "Inspect the current page URL, title, visible text, and interactive elements.", InputSchema: schema(`{"type":"object","properties":{},"additionalProperties":false}`), Invoke: adapter.inspect}
	screenshot := agent.Tool{Name: "screenshot", Description: "Capture a screenshot for evidence. Use after significant actions.", InputSchema: schema(`{"type":"object","properties":{"label":{"type":"string","maxLength":80}},"additionalProperties":false}`), Invoke: adapter.screenshot}
	if !includeActions {
		return []agent.Tool{inspect, screenshot}
	}
	return []agent.Tool{
		inspect,
		{Name: "click", Description: "Click one inspected element by its current reference.", InputSchema: schema(`{"type":"object","properties":{"ref":{"type":"string","minLength":1,"maxLength":100}},"required":["ref"],"additionalProperties":false}`), Invoke: adapter.click},
		{Name: "navigate", Description: "Navigate to an HTTP(S) URL without credentials.", InputSchema: schema(`{"type":"object","properties":{"url":{"type":"string","minLength":1,"maxLength":2000}},"required":["url"],"additionalProperties":false}`), Invoke: adapter.navigate},
		screenshot,
	}
}

func schema(value string) json.RawMessage { return json.RawMessage(value) }
