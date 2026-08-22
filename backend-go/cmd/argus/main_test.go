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
