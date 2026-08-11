package httpapi

import (
	"fmt"
	"net/http"

	"github.com/leonardo2api/leonardo2api/internal/modelconstraints"
)

func validateModelPrompt(model, prompt string) error {
	limit, ok := modelconstraints.PromptLimit(model)
	if !ok {
		return nil
	}
	actual := modelconstraints.PromptCharacters(prompt)
	if actual <= limit {
		return nil
	}
	return &requestError{
		Status:  http.StatusBadRequest,
		Code:    "prompt_too_long",
		Message: fmt.Sprintf("prompt must not exceed %d Unicode characters for %s (received %d)", limit, model, actual),
		Details: map[string]any{
			"model":             model,
			"actual_characters": actual,
			"max_characters":    limit,
		},
	}
}
