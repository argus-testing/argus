package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultStaticDirFindsFrontend(t *testing.T) {
	path := defaultStaticDir()
	if info, err := os.Stat(filepath.Join(path, "index.html")); err != nil || info.IsDir() {
		t.Fatalf("static index = %q, %v", path, err)
	}
}

func TestOptionsReadGeminiModel(t *testing.T) {
	t.Setenv("GEMINI_MODEL", "gemini-test")
	if optionsFromEnv().Model != "gemini-test" {
		t.Fatalf("server model = %q", optionsFromEnv().Model)
	}
	if runnerOptionsFromEnv("screenshots").Model != "gemini-test" {
		t.Fatalf("runner model = %q", runnerOptionsFromEnv("screenshots").Model)
	}
}

func TestPathsAndAddressFromEnv(t *testing.T) {
	t.Setenv("ARGUS_DATA_DIR", "state")
	t.Setenv("ARGUS_DB_PATH", "")
	database, screenshots := pathsFromEnv()
	if database != filepath.Join("state", "argus.db") || screenshots != filepath.Join("state", "screenshots") {
		t.Fatalf("default paths = %q, %q", database, screenshots)
	}
	t.Setenv("ARGUS_DB_PATH", filepath.Join("custom", "runs.db"))
	database, screenshots = pathsFromEnv()
	if database != filepath.Join("custom", "runs.db") || screenshots != filepath.Join("custom", "screenshots") {
		t.Fatalf("custom paths = %q, %q", database, screenshots)
	}
	t.Setenv("ARGUS_ADDR", "")
	t.Setenv("PORT", "8123")
	if address := serverAddressFromEnv(); address != ":8123" {
		t.Fatalf("PORT address = %q", address)
	}
	t.Setenv("ARGUS_ADDR", "127.0.0.1:9000")
	if address := serverAddressFromEnv(); address != "127.0.0.1:9000" {
		t.Fatalf("ARGUS_ADDR address = %q", address)
	}
}
