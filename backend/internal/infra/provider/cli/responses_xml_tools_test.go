package cli

import (
	"encoding/json"
	"github.com/chenyme/grok2api/backend/internal/infra/provider/conversation"
	"io"
	"strings"
	"testing"
)

const xmlToolFixture = `<tool_call><name>save_note</name><parameter name="path">note.txt</parameter><parameter name="content">alpha
beta &amp; gamma</parameter><parameter name="count">2</parameter></tool_call>`

func xmlTestCompatibility(t *testing.T, choice string) *responsesToolCompatibility {
	t.Helper()
	payload := map[string]json.RawMessage{
		"tools":       json.RawMessage(`[{"type":"function","name":"save_note","parameters":{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"},"count":{"type":"integer"}},"required":["path","content","count"],"additionalProperties":false}}]`),
		"tool_choice": json.RawMessage(choice),
	}
	c, err := normalizeResponsesTools(payload)
	if err != nil || c == nil {
		t.Fatal(err)
	}
	return c
}

func xmlTestMessage(text string) map[string]any {
	return map[string]any{"type": "message", "id": "msg_test", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text}}}
}

func TestXMLToolsDeclaredTypedAndLossless(t *testing.T) {
	c := xmlTestCompatibility(t, `"auto"`)
	for _, text := range []string{xmlToolFixture, strings.ReplaceAll(strings.ReplaceAll(xmlToolFixture, "<tool_call>", "<xai:tool_call>"), "</tool_call>", "</xai:tool_call>"), `<function_calls><invoke name="save_note"><parameter name="path">note.txt</parameter><parameter name="content">alpha
beta &amp; gamma</parameter><parameter name="count">2</parameter></invoke></function_calls>`, xmlToolFixture + ` persist leftover kwargs: {'count': 2} persist leftover kwargs: {'count': 2}`} {
		calls, ok := c.parseXMLCalls(text, "msg_test")
		if !ok || len(calls) != 1 {
			t.Fatal("valid XML not recovered")
		}
		call := calls[0].(map[string]any)
		var args map[string]any
		json.Unmarshal([]byte(call["arguments"].(string)), &args)
		if args["content"] != "alpha\nbeta & gamma" || args["count"] != float64(2) || call["name"] != "save_note" {
			t.Fatal("parameter corruption", args)
		}
		again, _ := c.parseXMLCalls(text, "msg_test")
		if again[0].(map[string]any)["call_id"] != call["call_id"] {
			t.Fatal("unstable call ID")
		}
	}
}

func TestXMLToolsFailClosedAndPreserveOrdinaryText(t *testing.T) {
	c := xmlTestCompatibility(t, `"auto"`)
	for _, text := range []string{"Example: " + xmlToolFixture, "```xml\n" + xmlToolFixture + "\n```", xmlToolFixture + " additional instructions", strings.ReplaceAll(xmlToolFixture, "save_note", "delete_everything"), strings.Replace(xmlToolFixture, "<parameter name=\"count\">2", "<parameter name=\"count\">not a number", 1), strings.TrimSuffix(xmlToolFixture, "</tool_call>"), strings.Replace(xmlToolFixture, "<name>save_note</name>", "<name>save_note</name><parameter name=\"count\">3</parameter>", 1)} {
		if _, ok := c.parseXMLCalls(text, "id"); ok {
			t.Fatal("ambiguous text promoted")
		}
		response := map[string]any{"output": []any{xmlTestMessage(text)}}
		if len(c.promoteXMLResponse(response)) != 0 {
			t.Fatal("ordinary text lost")
		}
	}
	if _, ok := xmlTestCompatibility(t, `"none"`).parseXMLCalls(xmlToolFixture, "id"); ok {
		t.Fatal("tool_choice none bypassed")
	}
}

func TestXMLToolNativePrecedence(t *testing.T) {
	c := xmlTestCompatibility(t, `"auto"`)
	calls, _ := c.parseXMLCalls(xmlToolFixture, "original")
	response := map[string]any{"output": []any{xmlTestMessage(xmlToolFixture), calls[0]}}
	c.promoteXMLResponse(response)
	if len(response["output"].([]any)) != 1 {
		t.Fatal("duplicate native tool call")
	}
}

func TestXMLCallsAllProtocolJSONBoundaries(t *testing.T) {
	c := xmlTestCompatibility(t, `{"type":"function","name":"save_note"}`)
	input, _ := json.Marshal(map[string]any{"id": "resp_test", "model": "grok-4.6", "status": "incomplete", "output": []any{xmlTestMessage(xmlToolFixture)}, "usage": map[string]any{"input_tokens": 20, "output_tokens": 30}})
	normalized, err := c.normalizeResponseJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	for kind, marker := range map[string]string{"chat": "tool_calls", "messages": "tool_use", "responses": "function_call"} {
		body, err := conversation.ConvertResponseJSON(normalized, kind)
		if err != nil || !strings.Contains(string(body), marker) || strings.Contains(string(body), "\\u003ctool") {
			t.Fatal(kind, string(body), err)
		}
	}
}

func TestXMLToolStreamFragmentsAndNativeEvents(t *testing.T) {
	for _, native := range []bool{false, true} {
		c := xmlTestCompatibility(t, `"auto"`)
		var wire strings.Builder
		sequence := 0
		emit := func(payload map[string]any) {
			payload["sequence_number"] = sequence
			sequence++
			b, _ := json.Marshal(payload)
			wire.WriteString("event: " + payload["type"].(string) + "\ndata: " + string(b) + "\n\n")
		}
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_test", "output": []any{}}})
		added := xmlTestMessage("")
		added["content"] = []any{}
		emit(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": added})
		for _, char := range xmlToolFixture {
			emit(map[string]any{"type": "response.output_text.delta", "output_index": 0, "item_id": "msg_test", "content_index": 0, "delta": string(char)})
		}
		emit(map[string]any{"type": "response.output_text.done", "output_index": 0, "item_id": "msg_test", "text": xmlToolFixture})
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": xmlTestMessage(xmlToolFixture)})
		items := []any{xmlTestMessage(xmlToolFixture)}
		if native {
			calls, _ := c.parseXMLCalls(xmlToolFixture, "native")
			for _, event := range xmlCallEvents(compatibleSSEEvent{}, calls[0].(map[string]any), 1) {
				var p map[string]any
				json.Unmarshal(event.Data(), &p)
				emit(p)
			}
			items = append(items, calls[0])
		}
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_test", "output": items, "usage": map[string]any{"input_tokens": 123, "output_tokens": 45}}})
		wire.WriteString("data: [DONE]\n\n")
		stream := c.normalizeResponseStream(io.NopCloser(strings.NewReader(wire.String())))
		data, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "<tool") || strings.Contains(string(data), "\\u003ctool") {
			t.Fatal("XML leaked")
		}
		var calls int
		lastSequence := float64(-1)
		err = consumeCompatibleSSE(strings.NewReader(string(data)), func(event compatibleSSEEvent) error {
			var p map[string]any
			_ = json.Unmarshal(event.Data(), &p)
			if n, ok := p["sequence_number"].(float64); ok {
				if n <= lastSequence {
					t.Fatal("nonmonotonic sequence")
				}
				lastSequence = n
			}
			if p["type"] == "response.output_item.done" {
				item, _ := p["item"].(map[string]any)
				if item["type"] == "function_call" {
					calls++
					if p["output_index"] != float64(0) {
						t.Fatal("invalid output index")
					}
				}
			}
			if p["type"] == "response.completed" {
				response := p["response"].(map[string]any)
				if len(response["output"].([]any)) != 1 || response["usage"].(map[string]any)["output_tokens"] != float64(45) {
					t.Fatal("final output or usage lost")
				}
			}
			return nil
		})
		if err != nil || calls != 1 {
			t.Fatal("missing or duplicate tool lifecycle", calls, err)
		}
	}
}

func TestXMLStreamOrdinaryTextFlushesImmediately(t *testing.T) {
	s := xmlToolStream{compatibility: xmlTestCompatibility(t, `"auto"`)}
	event := func(p map[string]any) compatibleSSEEvent {
		b, _ := json.Marshal(p)
		e := compatibleSSEEvent{}
		e.SetData(b)
		return e
	}
	if events, _ := s.accept(event(map[string]any{"type": "response.output_item.added", "item": xmlTestMessage("")})); len(events) != 0 {
		t.Fatal("header emitted before decision")
	}
	events, err := s.accept(event(map[string]any{"type": "response.output_text.delta", "item_id": "msg_test", "delta": "Hello"}))
	if err != nil || len(events) != 2 {
		t.Fatal("ordinary text buffered until completion")
	}
}
