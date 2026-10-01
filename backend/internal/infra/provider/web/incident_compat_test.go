package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chenyme/grok2api/backend/internal/infra/provider/conversation"
	"strings"
	"testing"
)

func TestNamespaceToolsRoundTrip(t *testing.T) {
	raw := json.RawMessage(`[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"lookup","description":"Look up a record","parameters":{"type":"object","properties":{"id":{"type":"string"}}}}]},{"type":"namespace","name":"crm","tools":[{"type":"function","name":"lookup"}]}]`)
	config, err := parseToolConfiguration(raw, json.RawMessage(`{"type":"function","name":"lookup","namespace":"functions"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Functions) != 2 || config.Functions[0].Name == config.Functions[1].Name || config.ForcedName != config.Functions[0].Name {
		t.Fatalf("namespace aliases/choice: %#v", config)
	}
	parsed := parsedChat{Tools: config.ResponseTools, ToolChoice: config.ResponseChoice}
	parsed.appendText(fmt.Sprintf(`<tool_calls><tool_call><tool_name>%s</tool_name><parameters>{"id":"1"}</parameters></tool_call></tool_calls>`, config.ForcedName))
	applyParsedToolCalls(&parsed, config)
	if len(parsed.ToolCalls) != 1 || parsed.ToolCalls[0].Name != "lookup" || parsed.ToolCalls[0].Namespace != "functions" {
		t.Fatalf("calls=%#v", parsed.ToolCalls)
	}
	body, err := json.Marshal(buildOpenAIResult(conversation.OperationResponses, "resp_test", "grok-chat-fast", parsed, false, conversation.ResponseOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err = json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	item := wire["output"].([]any)[0].(map[string]any)
	if item["name"] != "lookup" || item["namespace"] != "functions" {
		t.Fatalf("wire=%s", body)
	}
	tools := wire["tools"].([]any)
	if tools[0].(map[string]any)["type"] != "namespace" {
		t.Fatalf("tools=%#v", tools)
	}
	history, err := normalizeOpenAIInput(openAIRequest{Input: json.RawMessage(`[{"type":"function_call","name":"lookup","namespace":"functions","call_id":"call_1","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"found"}]`)}, conversation.OperationResponses)
	if err != nil || !strings.Contains(history.Prompt, config.ForcedName) || !strings.Contains(history.Prompt, "found") {
		t.Fatalf("history=%#v err=%v", history, err)
	}
	var stream bytes.Buffer
	state := newWebResponsesStream(&stream, "resp_test")
	if err = state.ToolCalls(parsed.ToolCalls); err != nil {
		t.Fatal(err)
	}
	if err = state.Finish(&parsed); err != nil {
		t.Fatal(err)
	}
	if strings.Count(stream.String(), `"namespace":"functions"`) != 2 {
		t.Fatalf("stream=%s", stream.String())
	}
	stored, _ := json.Marshal(buildOpenAIResult(conversation.OperationResponses, "resp_test", "grok-chat-fast", parsed, false, conversation.ResponseOptions{}))
	if !bytes.Contains(stored, []byte(`"namespace":"functions"`)) {
		t.Fatalf("stored=%s", stored)
	}
}

func TestNamespaceToolsRejectMalformedAndCollisions(t *testing.T) {
	for _, raw := range []string{
		`[{"type":"namespace","name":"bad.name","tools":[{"type":"function","name":"x"}]}]`,
		`[{"type":"namespace","name":"ok","tools":[]}]`,
		`[{"type":"namespace","name":"ok","tools":[{"type":"web_search"}]}]`,
		`[{"type":"namespace","name":"ok","tools":[{"type":"function","name":"x"},{"type":"function","name":"x"}]}]`,
		fmt.Sprintf(`[{"type":"function","name":%q},{"type":"namespace","name":"ok","tools":[{"type":"function","name":"x"}]}]`, namespacedToolAlias("ok", "x")),
	} {
		if _, err := parseToolConfiguration(json.RawMessage(raw), nil); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	children := make([]any, 129)
	for i := range children {
		children[i] = map[string]any{"type": "function", "name": fmt.Sprintf("f%d", i)}
	}
	raw, _ := json.Marshal([]any{map[string]any{"type": "namespace", "name": "ok", "tools": children}})
	if _, err := parseToolConfiguration(raw, nil); err == nil {
		t.Fatal("accepted >128 expanded functions")
	}
	if _, err := parseToolConfiguration(json.RawMessage(`[{"type":"namespace","name":"ok","tools":[{"type":"function","name":"x"}]}]`), json.RawMessage(`{"type":"function","namespace":"other","name":"x"}`)); err == nil {
		t.Fatal("accepted undeclared forced tool")
	}
}

func TestUploadParseFailureDoesNotBecomeTransportError(t *testing.T) {
	_, err := decodeDirectFileUploadResponse(strings.NewReader(`{"uploadId":"upload_1","terminalError":"Cannot open given image [WKE=file:parse-failed]"}`))
	if !errors.Is(err, errInvalidChatImage) {
		t.Fatalf("not a request image error: %v", err)
	}
	if strings.Contains(err.Error(), "upload_1") {
		t.Fatal("upload ID leaked")
	}
	for _, terminal := range []string{`"quota exhausted"`, `"content policy violation"`, `{"message":"unknown"}`} {
		_, err = decodeDirectFileUploadResponse(strings.NewReader(`{"terminalError":` + terminal + `}`))
		if err == nil || errors.Is(err, errInvalidChatImage) {
			t.Fatalf("unrelated terminal error misclassified: %s %v", terminal, err)
		}
	}
}

func TestNamespaceStreamSieveEveryFrameBoundary(t *testing.T) {
	config, err := parseToolConfiguration(json.RawMessage(`[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"echo"}]}]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`<tool_calls><tool_call><tool_name>%s</tool_name><parameters>{"text":"ok"}</parameters></tool_call></tool_calls>`, config.Functions[0].Name)
	for size := 1; size <= len(raw); size++ {
		sieve := newToolStreamSieve(config.available)
		var text strings.Builder
		var calls []parsedToolCall
		for start := 0; start < len(raw); start += size {
			result := sieve.Feed(raw[start:min(start+size, len(raw))])
			text.WriteString(result.SafeText)
			calls = append(calls, result.Calls...)
		}
		end := sieve.Flush()
		text.WriteString(end.SafeText)
		calls = append(calls, end.Calls...)
		calls = config.restoreToolCalls(calls)
		if text.Len() != 0 || len(calls) != 1 || calls[0].Name != "echo" || calls[0].Namespace != "functions" || calls[0].Arguments != `{"text":"ok"}` {
			t.Fatalf("frame=%d text=%q calls=%#v", size, text.String(), calls)
		}
	}
}
