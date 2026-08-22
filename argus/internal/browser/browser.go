// Package browser provides the run-scoped browser interface used by the runner.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/ace-foundry/argus-testing/argus/internal/server"
	"github.com/mxschmitt/playwright-go"
)

const (
	navigationTimeout = 30_000.0
	actionTimeout     = 10_000.0
)

// Factory creates isolated browser sessions. Implementations must not reuse a
// BrowserContext between runs.
type Factory interface {
	Open(context.Context, ...SessionOptions) (Session, error)
}

type SessionOptions struct {
	AllowMutations  bool
	AllowNavigation func(string) bool
}

type InputValue struct {
	Text      string
	Sensitive bool
}

// Session is a single run's browser context and page.
type Session interface {
	Navigate(context.Context, string) (Navigation, error)
	Inspect(context.Context) (PageSnapshot, error)
	Element(context.Context, string) (Element, error)
	ElementAt(context.Context, int, int) (Element, error)
	Click(context.Context, string) (ActionResult, error)
	Type(context.Context, string, InputValue) (ActionResult, error)
	FillForm(context.Context, map[string]InputValue) (ActionResult, error)
	Submit(context.Context, string) (ActionResult, error)
	Select(context.Context, string, string) (ActionResult, error)
	Press(context.Context, string) (ActionResult, error)
	Scroll(context.Context, int) (ActionResult, error)
	Resize(context.Context, int, int) (ActionResult, error)
	Wait(context.Context, WaitCondition) (ActionResult, error)
	ConsoleErrors(context.Context) ([]string, error)
	NetworkErrors(context.Context) ([]NetworkError, error)
	ClickPoint(context.Context, int, int) (ActionResult, error)
	Screenshot(context.Context, string) error
	ScreenshotViewport(context.Context, string) error
	Close() error
}

type Navigation struct {
	URL   string
	Title string
}

type playwrightFactory struct{}

// NewPlaywrightFactory returns the production Chromium adapter.
func NewPlaywrightFactory() Factory { return playwrightFactory{} }

// Install downloads the Playwright driver and browsers. It is intentionally
// only called by the explicit install-browser CLI command, never at startup.
func Install() error {
	return playwright.Install(&playwright.RunOptions{Browsers: []string{"chromium"}})
}

func (playwrightFactory) Open(ctx context.Context, requested ...SessionOptions) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(requested) > 1 {
		return nil, errors.New("at most one browser session option is allowed")
	}
	options := SessionOptions{}
	if len(requested) == 1 {
		options = requested[0]
	}
	playwrightInstance, err := playwright.Run()
	if err != nil {
		return nil, err
	}
	cleanup := func(err error) (Session, error) {
		return nil, errors.Join(err, playwrightInstance.Stop())
	}
	if err := ctx.Err(); err != nil {
		return cleanup(err)
	}
	headless := true
	instance, err := playwrightInstance.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: &headless})
	if err != nil {
		return cleanup(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, instance.Close(), playwrightInstance.Stop())
	}
	contextInstance, err := instance.NewContext(playwright.BrowserNewContextOptions{Viewport: &playwright.Size{Width: 1440, Height: 900}})
	if err != nil {
		return nil, errors.Join(err, instance.Close(), playwrightInstance.Stop())
	}
	page, err := contextInstance.NewPage()
	if err != nil {
		return nil, errors.Join(err, contextInstance.Close(), instance.Close(), playwrightInstance.Stop())
	}
	session := &playwrightSession{playwright: playwrightInstance, browser: instance, context: contextInstance, page: page, done: make(chan struct{}), elements: newElementRegistry()}
	if err := session.installRequestPolicy(options); err != nil {
		session.close()
		return nil, errors.Join(err, session.closeErr)
	}
	session.observePage()
	go func() {
		select {
		case <-ctx.Done():
			session.close()
		case <-session.done:
		}
	}()
	return session, nil
}

type playwrightSession struct {
	playwright        *playwright.Playwright
	browser           playwright.Browser
	context           playwright.BrowserContext
	page              playwright.Page
	closeOnce         sync.Once
	closeErr          error
	done              chan struct{}
	elements          *elementRegistry
	eventsMu          sync.Mutex
	console           []string
	network           []NetworkError
	policyMu          sync.Mutex
	blockedNavigation bool
	lastAuthorizedURL string
}

func (s *playwrightSession) Navigate(ctx context.Context, target string) (Navigation, error) {
	if err := ctx.Err(); err != nil {
		return Navigation{}, err
	}
	if err := server.ValidateURL(target); err != nil {
		return Navigation{}, err
	}
	s.elements.invalidate()
	if _, err := s.page.Goto(target, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(navigationTimeout)}); err != nil {
		if s.restoreBlockedNavigation() {
			return Navigation{}, ErrNavigationBlocked
		}
		return Navigation{}, err
	}
	if s.restoreBlockedNavigation() {
		return Navigation{}, ErrNavigationBlocked
	}
	if err := ctx.Err(); err != nil {
		return Navigation{}, err
	}
	title, err := s.page.Title()
	if err != nil {
		return Navigation{}, err
	}
	s.rememberAuthorizedURL(s.page.URL())
	return Navigation{URL: server.SanitizeURL(s.page.URL()), Title: title}, nil
}

type pagePayload struct {
	Title    string           `json:"title"`
	Text     string           `json:"text"`
	Width    int              `json:"width"`
	Height   int              `json:"height"`
	Elements []elementPayload `json:"elements"`
}

type elementPayload struct {
	Element
	Selector string `json:"selector"`
}

func (s *playwrightSession) Inspect(ctx context.Context) (PageSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return PageSnapshot{}, err
	}
	prefix := fmt.Sprintf("argus-%d", s.elements.generation+1)
	raw, err := s.page.Evaluate(snapshotJavaScript, prefix)
	if err != nil {
		return PageSnapshot{}, err
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return PageSnapshot{}, err
	}
	var payload pagePayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return PageSnapshot{}, fmt.Errorf("decode page snapshot: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return PageSnapshot{}, err
	}
	targets := make([]elementTarget, len(payload.Elements))
	for index, element := range payload.Elements {
		targets[index] = elementTarget{selector: element.Selector, element: element.Element}
	}
	references := s.elements.replace(targets)
	elements := make([]Element, len(payload.Elements))
	for index, element := range payload.Elements {
		elements[index] = element.Element
		elements[index].Ref = references[index]
	}
	return PageSnapshot{URL: server.SanitizeURL(s.page.URL()), Title: limit(payload.Title, 1_000), Text: limit(payload.Text, 12_000), Width: payload.Width, Height: payload.Height, Elements: elements}, nil
}

func (s *playwrightSession) Element(ctx context.Context, reference string) (Element, error) {
	if err := ctx.Err(); err != nil {
		return Element{}, err
	}
	return s.elements.element(reference)
}

func (s *playwrightSession) ElementAt(ctx context.Context, x, y int) (Element, error) {
	if err := ctx.Err(); err != nil {
		return Element{}, err
	}
	viewport := s.page.ViewportSize()
	if viewport == nil || x < 0 || y < 0 || x >= viewport.Width || y >= viewport.Height {
		return Element{}, errors.New("point is outside the viewport")
	}
	raw, err := s.page.Evaluate(elementAtJavaScript, []int{x, y})
	if err != nil {
		return Element{}, err
	}
	if raw == nil {
		return Element{}, ErrUnknownElement
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return Element{}, err
	}
	var element Element
	if err := json.Unmarshal(encoded, &element); err != nil {
		return Element{}, fmt.Errorf("decode point element: %w", err)
	}
	return element, nil
}

func (s *playwrightSession) Click(ctx context.Context, reference string) (ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return ActionResult{}, err
	}
	locator, err := s.locator(reference)
	if err != nil {
		return ActionResult{}, err
	}
	if err := locator.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(actionTimeout)}); err != nil {
		return ActionResult{}, err
	}
	return s.result(ctx)
}

func (s *playwrightSession) Type(ctx context.Context, reference string, value InputValue) (ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return ActionResult{}, err
	}
	locator, err := s.locator(reference)
	if err != nil {
		return ActionResult{}, err
	}
	if _, err := locator.Evaluate("(element, sensitive) => { if (sensitive) element.setAttribute('data-argus-sensitive', 'true'); else element.removeAttribute('data-argus-sensitive'); }", value.Sensitive); err != nil {
		return ActionResult{}, err
	}
	if err := locator.Fill(value.Text, playwright.LocatorFillOptions{Timeout: playwright.Float(actionTimeout)}); err != nil {
		return ActionResult{}, err
	}
	return s.result(ctx)
}

func (s *playwrightSession) FillForm(ctx context.Context, values map[string]InputValue) (ActionResult, error) {
	references := make([]string, 0, len(values))
	for reference := range values {
		references = append(references, reference)
	}
	sort.Strings(references)
	for _, reference := range references {
		if _, err := s.Type(ctx, reference, values[reference]); err != nil {
			return ActionResult{}, err
		}
	}
	return s.result(ctx)
}

func (s *playwrightSession) Submit(ctx context.Context, reference string) (ActionResult, error) {
	return s.Click(ctx, reference)
}

func (s *playwrightSession) Select(ctx context.Context, reference, value string) (ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return ActionResult{}, err
	}
	locator, err := s.locator(reference)
	if err != nil {
		return ActionResult{}, err
	}
	values := []string{value}
	if _, err := locator.SelectOption(playwright.SelectOptionValues{ValuesOrLabels: &values}, playwright.LocatorSelectOptionOptions{Timeout: playwright.Float(actionTimeout)}); err != nil {
		return ActionResult{}, err
	}
	return s.result(ctx)
}

func (s *playwrightSession) Press(ctx context.Context, key string) (ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return ActionResult{}, err
	}
	if err := s.page.Keyboard().Press(key); err != nil {
		return ActionResult{}, err
	}
	return s.result(ctx)
}

func (s *playwrightSession) Scroll(ctx context.Context, delta int) (ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return ActionResult{}, err
	}
	if delta < -10_000 || delta > 10_000 {
		return ActionResult{}, errors.New("scroll delta is out of range")
	}
	if _, err := s.page.Evaluate(`delta => window.scrollBy(0, delta)`, delta); err != nil {
		return ActionResult{}, err
	}
	return s.result(ctx)
}

func (s *playwrightSession) Resize(ctx context.Context, width, height int) (ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return ActionResult{}, err
	}
	if width < 240 || width > 3_840 || height < 240 || height > 3_840 {
		return ActionResult{}, errors.New("viewport is out of range")
	}
	if err := s.page.SetViewportSize(width, height); err != nil {
		return ActionResult{}, err
	}
	return s.result(ctx)
}

func (s *playwrightSession) Wait(ctx context.Context, condition WaitCondition) (ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return ActionResult{}, err
	}
	timeout := condition.TimeoutMillis
	if timeout <= 0 {
		timeout = 5_000
	}
	if timeout > 30_000 {
		timeout = 30_000
	}
	options := playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(float64(timeout))}
	if condition.Text != "" {
		if _, err := s.page.WaitForFunction(`text => document.body?.innerText.includes(text) === true`, condition.Text, options); err != nil {
			return ActionResult{}, err
		}
	}
	if condition.URL != "" {
		if _, err := s.page.WaitForFunction(`expected => window.location.href.includes(expected)`, condition.URL, options); err != nil {
			return ActionResult{}, err
		}
	}
	if condition.Text == "" && condition.URL == "" {
		return ActionResult{}, errors.New("wait condition is empty")
	}
	return s.result(ctx)
}

func (s *playwrightSession) ConsoleErrors(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	return append([]string(nil), s.console...), nil
}

func (s *playwrightSession) NetworkErrors(ctx context.Context) ([]NetworkError, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	return append([]NetworkError(nil), s.network...), nil
}

func (s *playwrightSession) ClickPoint(ctx context.Context, x, y int) (ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return ActionResult{}, err
	}
	viewport := s.page.ViewportSize()
	if viewport == nil || x < 0 || y < 0 || x >= viewport.Width || y >= viewport.Height {
		return ActionResult{}, errors.New("click point is outside the viewport")
	}
	if err := s.page.Mouse().Click(float64(x), float64(y)); err != nil {
		return ActionResult{}, err
	}
	return s.result(ctx)
}

func (s *playwrightSession) Screenshot(ctx context.Context, path string) error {
	return s.screenshot(ctx, path, true)
}

func (s *playwrightSession) ScreenshotViewport(ctx context.Context, path string) error {
	return s.screenshot(ctx, path, false)
}

func (s *playwrightSession) screenshot(ctx context.Context, path string, fullPage bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	maskColor := "#FF00FF"
	mask := s.page.Locator("input[type=\"password\"], [data-argus-sensitive=\"true\"]")
	_, err := s.page.Screenshot(playwright.PageScreenshotOptions{
		Path: &path, FullPage: &fullPage, Timeout: playwright.Float(navigationTimeout),
		Mask: []playwright.Locator{mask}, MaskColor: &maskColor,
	})
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (s *playwrightSession) installRequestPolicy(options SessionOptions) error {
	return s.page.Route("**/*", func(route playwright.Route) {
		request := route.Request()
		method := strings.ToUpper(request.Method())
		mutationAllowed := options.AllowMutations || method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
		navigationAllowed := !request.IsNavigationRequest() || options.AllowNavigation == nil || options.AllowNavigation(request.URL())
		if mutationAllowed && navigationAllowed {
			_ = route.Continue()
			return
		}
		s.recordNetwork(NetworkError{Method: method, URL: server.SanitizeURL(request.URL()), Status: 0})
		if request.IsNavigationRequest() {
			s.policyMu.Lock()
			s.blockedNavigation = true
			s.policyMu.Unlock()
		}
		_ = route.Abort("blockedbyclient")
	})
}

func (s *playwrightSession) Close() error {
	s.close()
	return s.closeErr
}

func (s *playwrightSession) locator(reference string) (playwright.Locator, error) {
	target, err := s.elements.resolve(reference)
	if err != nil {
		return nil, err
	}
	return s.page.Locator(target.selector).First(), nil
}

func (s *playwrightSession) result(ctx context.Context) (ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return ActionResult{}, err
	}
	if s.restoreBlockedNavigation() {
		return ActionResult{}, ErrNavigationBlocked
	}
	s.rememberAuthorizedURL(s.page.URL())
	return ActionResult{URL: server.SanitizeURL(s.page.URL())}, nil
}

func (s *playwrightSession) rememberAuthorizedURL(value string) {
	if value == "" || strings.HasPrefix(value, "chrome-error:") {
		return
	}
	s.policyMu.Lock()
	s.lastAuthorizedURL = value
	s.policyMu.Unlock()
}

func (s *playwrightSession) restoreBlockedNavigation() bool {
	s.policyMu.Lock()
	if !s.blockedNavigation {
		s.policyMu.Unlock()
		return false
	}
	s.blockedNavigation = false
	target := s.lastAuthorizedURL
	s.policyMu.Unlock()
	if target != "" {
		_, _ = s.page.Goto(target, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
			Timeout:   playwright.Float(navigationTimeout),
		})
	}
	return true
}

func (s *playwrightSession) observePage() {
	s.page.OnConsole(func(message playwright.ConsoleMessage) {
		if message.Type() != "error" && message.Type() != "assert" {
			return
		}
		s.eventsMu.Lock()
		s.console = appendBounded(s.console, limit(message.Text(), 1_000), 100)
		s.eventsMu.Unlock()
	})
	s.page.OnResponse(func(response playwright.Response) {
		if response.Status() < 400 {
			return
		}
		request := response.Request()
		s.recordNetwork(NetworkError{Method: request.Method(), URL: server.SanitizeURL(response.URL()), Status: response.Status()})
	})
	s.page.OnRequestFailed(func(request playwright.Request) {
		s.recordNetwork(NetworkError{Method: request.Method(), URL: server.SanitizeURL(request.URL()), Status: 0})
	})
}

func (s *playwrightSession) recordNetwork(event NetworkError) {
	s.eventsMu.Lock()
	s.network = appendBounded(s.network, event, 100)
	s.eventsMu.Unlock()
}

func appendBounded[T any](values []T, value T, maximum int) []T {
	if len(values) == maximum {
		copy(values, values[1:])
		values = values[:maximum-1]
	}
	return append(values, value)
}

func (s *playwrightSession) close() {
	s.closeOnce.Do(func() {
		close(s.done)
		s.closeErr = errors.Join(s.context.Close(), s.browser.Close(), s.playwright.Stop())
	})
}

func limit(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}

const snapshotJavaScript = `(prefix) => {
  const attr = 'data-argus-agent-ref';
  document.querySelectorAll('[' + attr + ']').forEach((element) => element.removeAttribute(attr));
  const nodes = [...document.querySelectorAll('a,button,input,select,textarea,[role="button"],[role="link"],[role="checkbox"],[role="radio"],[role="menuitem"],[tabindex]')]
    .filter((element) => element.getClientRects().length > 0 && !element.closest('[aria-hidden="true"]'))
    .slice(0, 200);
  const destructiveWords = new Set(['delete','remove','destroy','purchase','pay','checkout']);
  const words = (value) => (value || '').toLowerCase().split(/[^a-z]+/).filter(Boolean);
  const elements = nodes.map((element, index) => {
    const id = prefix + '-' + (index + 1);
    element.setAttribute(attr, id);
    const labels = element.labels ? [...element.labels].map((label) => label.innerText.trim()).filter(Boolean) : [];
    const label = labels[0] || element.getAttribute('aria-label') || '';
    const name = label || element.innerText?.trim() || element.getAttribute('title') || element.getAttribute('placeholder') || '';
    const type = (element.getAttribute('type') || '').toLowerCase();
    const form = element.form;
    const method = (form?.method || 'get').toLowerCase();
    const destructive = element.matches('[data-danger],[data-destructive],[aria-label*="delete" i]') || words(name).some((word) => destructiveWords.has(word));
    const mutating = destructive || type === 'submit' || (form && method !== 'get');
    return {
      selector: '[' + attr + '="' + id + '"]',
      tag: element.tagName.toLowerCase(),
      role: element.getAttribute('role') || '',
      name: name.slice(0, 300),
      label: label.slice(0, 300),
      placeholder: (element.getAttribute('placeholder') || '').slice(0, 300),
      input_type: type,
      disabled: Boolean(element.disabled),
      checked: Boolean(element.checked),
      selected: element.tagName === 'SELECT' ? String(element.value || '').slice(0, 300) : '',
      mutating,
      destructive
    };
  });
  return {
    title: document.title || '',
    text: document.body?.innerText || '',
    width: window.innerWidth,
    height: window.innerHeight,
    elements
  };
}`
