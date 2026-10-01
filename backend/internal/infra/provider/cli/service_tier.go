package cli

import (
	"bytes"
	"encoding/json"

	"github.com/chenyme/grok2api/backend/internal/infra/provider"
)

// Build has no verified service_tier surcharge contract. Its Fast product is a
// distinct discovered model, so a caller's priority/flex request must not silently
// become a standard request or a guessed billing multiplier.
func validateBuildServiceTier(request provider.ResponseResourceRequest) error {
	if len(request.Body) == 0 {
		return nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(request.Body, &payload); err != nil {
		return &responsesRequestError{Message: "Build request must be valid JSON", Code: "invalid_request"}
	}
	raw, exists := payload["service_tier"]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var tier string
	if json.Unmarshal(raw, &tier) == nil && (tier == "auto" || tier == "default") {
		return nil
	}
	return &responsesRequestError{
		Message: "Grok Build only supports service_tier auto/default. Select grok-4.7-build-fast for Fast mode.",
		Param:   "service_tier", Code: "unsupported_parameter",
	}
}
