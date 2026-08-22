package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// NextScreenshotPath reserves the next public and filesystem path for a run.
func NextScreenshotPath(root, runID, label string) (string, string, error) {
	if root == "" || !validRunID(runID) {
		return "", "", fmt.Errorf("invalid screenshot path")
	}
	directory := filepath.Join(root, runID)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", "", err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", "", err
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".png") {
			count++
		}
	}
	filename := fmt.Sprintf("%s-%d.png", safeLabel(label), count+1)
	return "/screenshots/" + runID + "/" + filename, filepath.Join(directory, filename), nil
}

func validRunID(runID string) bool {
	if len(runID) != 32 {
		return false
	}
	for _, character := range runID {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func safeLabel(label string) string {
	var value strings.Builder
	for _, character := range strings.ToLower(label) {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			value.WriteRune(character)
		} else {
			value.WriteByte('-')
		}
	}
	result := strings.Trim(value.String(), "-")
	if result == "" {
		return "evidence"
	}
	return result
}
