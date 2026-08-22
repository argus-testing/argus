// Package runner connects run state to the browser lifecycle.
package runner

import (
	"context"
	"errors"
	"time"

	"github.com/ace-foundry/argus-testing/backend-go/internal/browser"
	"github.com/ace-foundry/argus-testing/backend-go/internal/domain"
	"github.com/ace-foundry/argus-testing/backend-go/internal/store"
)

const pipelineNotConfigured = "agent pipeline not configured"

type Publisher func(domain.RunEvent)

type Options struct {
	ScreenshotDir string
	Timeout       time.Duration
}

type Runner struct {
	store         *store.Store
	browser       browser.Factory
	screenshotDir string
	timeout       time.Duration
	publish       Publisher
}

func New(runStore *store.Store, factory browser.Factory, options Options) *Runner {
	if factory == nil {
		factory = browser.NewPlaywrightFactory()
	}
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Minute
	}
	return &Runner{store: runStore, browser: factory, screenshotDir: options.ScreenshotDir, timeout: options.Timeout}
}

func (r *Runner) SetPublisher(publish Publisher) { r.publish = publish }

// Run advances one queued run through browser startup and initial evidence.
func (r *Runner) Run(parent context.Context, id string) {
	ctx, cancel := context.WithTimeout(parent, r.timeout)
	defer cancel()

	run, err := r.store.GetRun(id, false)
	if err != nil || run == nil || ctx.Err() != nil {
		return
	}
	started, err := r.store.Transition(id, []domain.RunStatus{domain.RunStatusQueued}, domain.RunStatusRunning, domain.EventRunStarted, nil, nil, nil)
	if err != nil || started == nil {
		return
	}
	r.publishEvent(*started)

	session, err := r.browser.Open(ctx)
	if err == nil {
		defer session.Close()
		_, err = session.Navigate(ctx, run.URL)
	}
	if err == nil {
		var publicPath, diskPath string
		publicPath, diskPath, err = NextScreenshotPath(r.screenshotDir, id, "initial")
		if err == nil {
			err = session.Screenshot(ctx, diskPath)
		}
		if err == nil {
			_, err = r.store.AddScreenshot(id, publicPath)
		}
		if err == nil {
			var event *domain.RunEvent
			event, err = r.store.AddEvent(id, domain.EventBrowserScreenshot, map[string]any{"path": publicPath, "label": "Initial page"})
			if event != nil {
				r.publishEvent(*event)
			}
		}
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			r.fail(id, "Run timed out", "timeout")
		}
		return
	}
	if err != nil {
		r.fail(id, err.Error(), "browser")
		return
	}
	r.fail(id, pipelineNotConfigured, "configuration")
}

func (r *Runner) fail(id, message, kind string) {
	event, err := r.store.Transition(id, []domain.RunStatus{domain.RunStatusRunning}, domain.RunStatusFailed, domain.EventRunFailed, map[string]any{"kind": kind, "message": message}, nil, &message)
	if err == nil && event != nil {
		r.publishEvent(*event)
	}
}

func (r *Runner) publishEvent(event domain.RunEvent) {
	if r.publish != nil {
		r.publish(event)
	}
}
