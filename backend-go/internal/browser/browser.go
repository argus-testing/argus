// Package browser provides the run-scoped browser interface used by the runner.
package browser

import (
	"context"
	"errors"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ace-foundry/argus-testing/backend-go/internal/server"
	"github.com/mxschmitt/playwright-go"
)

const (
	navigationTimeout = 30_000.0
	actionTimeout     = 10_000.0
)

// Factory creates isolated browser sessions. Implementations must not reuse a
// BrowserContext between runs.
type Factory interface {
	Open(context.Context) (Session, error)
}

// Session is a single run's browser context and page.
type Session interface {
	Navigate(context.Context, string) (Navigation, error)
	Inspect(context.Context) (Inspection, error)
	Click(context.Context, string) error
	Fill(context.Context, string, string) error
	Screenshot(context.Context, string) error
	Close() error
}

type Navigation struct {
	URL   string
	Title string
}

type Inspection struct {
	URL         string
	Title       string
	Body        string
	Interactive string
}

type playwrightFactory struct{}

// NewPlaywrightFactory returns the production Chromium adapter.
func NewPlaywrightFactory() Factory { return playwrightFactory{} }

// Install downloads the Playwright driver and browsers. It is intentionally
// only called by the explicit install-browser CLI command, never at startup.
func Install() error {
	return playwright.Install(&playwright.RunOptions{Browsers: []string{"chromium"}})
}

func (playwrightFactory) Open(ctx context.Context) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
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
	session := &playwrightSession{playwright: playwrightInstance, browser: instance, context: contextInstance, page: page, done: make(chan struct{})}
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
	playwright *playwright.Playwright
	browser    playwright.Browser
	context    playwright.BrowserContext
	page       playwright.Page
	closeOnce  sync.Once
	closeErr   error
	done       chan struct{}
}

func (s *playwrightSession) Navigate(ctx context.Context, target string) (Navigation, error) {
	if err := ctx.Err(); err != nil {
		return Navigation{}, err
	}
	if err := server.ValidateURL(target); err != nil {
		return Navigation{}, err
	}
	if _, err := s.page.Goto(target, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(navigationTimeout)}); err != nil {
		return Navigation{}, err
	}
	if err := ctx.Err(); err != nil {
		return Navigation{}, err
	}
	title, err := s.page.Title()
	if err != nil {
		return Navigation{}, err
	}
	return Navigation{URL: server.SanitizeURL(s.page.URL()), Title: title}, nil
}

func (s *playwrightSession) Inspect(ctx context.Context) (Inspection, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	body, err := s.page.Locator("body").InnerText(playwright.LocatorInnerTextOptions{Timeout: playwright.Float(actionTimeout)})
	if err != nil {
		return Inspection{}, err
	}
	interactive, err := s.page.Locator("a, button, input, select, textarea").AllInnerTexts()
	if err != nil {
		return Inspection{}, err
	}
	title, err := s.page.Title()
	if err != nil {
		return Inspection{}, err
	}
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	return Inspection{
		URL:         server.SanitizeURL(s.page.URL()),
		Title:       title,
		Body:        limit(body, 12_000),
		Interactive: limit(strings.Join(interactive, " | "), 4_000),
	}, nil
}

func (s *playwrightSession) Click(ctx context.Context, selector string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.page.Locator(selector).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(actionTimeout)}); err != nil {
		return err
	}
	return ctx.Err()
}

func (s *playwrightSession) Fill(ctx context.Context, selector, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.page.Locator(selector).First().Fill(text, playwright.LocatorFillOptions{Timeout: playwright.Float(actionTimeout)}); err != nil {
		return err
	}
	return ctx.Err()
}

func (s *playwrightSession) Screenshot(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPage := true
	_, err := s.page.Screenshot(playwright.PageScreenshotOptions{Path: &path, FullPage: &fullPage, Timeout: playwright.Float(navigationTimeout)})
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (s *playwrightSession) Close() error {
	s.close()
	return s.closeErr
}

func (s *playwrightSession) close() {
	s.closeOnce.Do(func() {
		close(s.done)
		s.closeErr = errors.Join(s.context.Close(), s.browser.Close(), s.playwright.Stop())
	})
}

func limit(value string, maximum int) string {
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	return string([]rune(value)[:maximum])
}
