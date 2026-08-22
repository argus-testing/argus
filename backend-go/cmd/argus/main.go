package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ace-foundry/argus-testing/backend-go/internal/browser"
	"github.com/ace-foundry/argus-testing/backend-go/internal/runner"
	"github.com/ace-foundry/argus-testing/backend-go/internal/server"
	"github.com/ace-foundry/argus-testing/backend-go/internal/store"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "install-browser" {
		if err := browser.Install(); err != nil {
			log.Fatal(err)
		}
		return
	}
	dataDir := os.Getenv("ARGUS_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	screenshotDir := filepath.Join(dataDir, "screenshots")
	if err := os.MkdirAll(screenshotDir, 0o755); err != nil {
		log.Fatal(err)
	}
	runStore, err := store.Open(filepath.Join(dataDir, "argus.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer runStore.Close()
	runRunner := runner.New(runStore, browser.NewPlaywrightFactory(), runnerOptionsFromEnv(screenshotDir))
	handler, err := server.New(runStore, runRunner, optionsFromEnv(dataDir))
	if err != nil {
		log.Fatal(err)
	}
	runRunner.SetPublisher(handler.Publish)
	address := os.Getenv("ARGUS_ADDR")
	if address == "" {
		address = ":8000"
	}
	log.Fatal(http.ListenAndServe(address, handler))
}

func runnerOptionsFromEnv(screenshotDir string) runner.Options {
	return runner.Options{ScreenshotDir: screenshotDir, Model: os.Getenv("GEMINI_MODEL")}
}

func optionsFromEnv(dataDir ...string) server.Options {
	staticDir := os.Getenv("ARGUS_STATIC_DIR")
	if staticDir == "" {
		staticDir = defaultStaticDir()
	}
	data := "data"
	if len(dataDir) > 0 {
		data = dataDir[0]
	}
	return server.Options{
		StaticDir:     staticDir,
		ScreenshotDir: filepath.Join(data, "screenshots"),
		Model:         os.Getenv("GEMINI_MODEL"),
	}
}

func defaultStaticDir() string {
	directory, err := os.Getwd()
	if err != nil {
		return filepath.Join("argus", "static")
	}
	for {
		candidate := filepath.Join(directory, "argus", "static")
		if info, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return filepath.Join("argus", "static")
		}
		directory = parent
	}
}
