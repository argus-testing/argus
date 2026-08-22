package runner

import (
	"context"
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/ace-foundry/argus-testing/argus/internal/gemini"
)

var errInvalidVisualMatch = errors.New("invalid visual match")

type GroundingRequest struct {
	Description string
	Image       []byte
	Width       int
	Height      int
	Limit       int
}

type VisualMatch struct {
	X           int     `json:"x"`
	Y           int     `json:"y"`
	Confidence  float64 `json:"confidence"`
	Description string  `json:"description"`
}

type Grounder interface {
	Locate(context.Context, GroundingRequest) ([]VisualMatch, error)
}

type geminiGrounder struct {
	provider *gemini.Provider
	model    string
}

func (g geminiGrounder) Locate(ctx context.Context, request GroundingRequest) ([]VisualMatch, error) {
	matches, err := g.provider.Locate(ctx, gemini.GroundingRequest{
		Model: g.model, Description: request.Description, Image: request.Image,
		Width: request.Width, Height: request.Height, Limit: request.Limit,
	})
	if err != nil {
		return nil, err
	}
	converted := make([]VisualMatch, len(matches))
	for index, match := range matches {
		converted[index] = VisualMatch{
			X: match.X, Y: match.Y, Confidence: match.Confidence, Description: match.Description,
		}
	}
	return converted, nil
}

func validateMatch(match VisualMatch, width, height int, minimumConfidence float64) (VisualMatch, error) {
	match.Description = strings.TrimSpace(match.Description)
	if width <= 0 || height <= 0 ||
		match.X < 0 || match.X >= width || match.Y < 0 || match.Y >= height ||
		math.IsNaN(match.Confidence) || math.IsInf(match.Confidence, 0) ||
		match.Confidence < minimumConfidence || match.Confidence > 1 ||
		!utf8.ValidString(match.Description) || utf8.RuneCountInString(match.Description) > 500 {
		return VisualMatch{}, errInvalidVisualMatch
	}
	return match, nil
}

func validateMatches(matches []VisualMatch, width, height int, minimumConfidence float64, limit int) ([]VisualMatch, error) {
	if limit < 1 || limit > 10 || len(matches) > limit {
		return nil, errInvalidVisualMatch
	}
	validated := make([]VisualMatch, len(matches))
	for index, match := range matches {
		value, err := validateMatch(match, width, height, minimumConfidence)
		if err != nil {
			return nil, err
		}
		validated[index] = value
	}
	return validated, nil
}
