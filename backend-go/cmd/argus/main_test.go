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

func TestServerOptionsReadGeminiModel(t *testing.T) {
	t.Setenv("GEMINI_MODEL", "gemini-test")
	if optionsFromEnv().Model != "gemini-test" {
		t.Fatalf("model = %q", optionsFromEnv().Model)
	}
}
