package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Ordinary text flushes on the first non-envelope prefix. Only a possible XML
// call waits for the final output, so native calls can take precedence without
// executing the same tool twice. State belongs to one response stream.
type xmlToolStream struct {
	compatibility *responsesToolCompatibility
	pending       []compatibleSSEEvent
	bytes         int
	prefix        string
	itemID        string
	candidate     bool
}

func (s *xmlToolStream) accept(event compatibleSSEEvent) ([]compatibleSSEEvent, error) {
	if s.compatibility == nil || len(s.compatibility.xmlTools) == 0 {
		return []compatibleSSEEvent{event}, nil
	}
	if !event.HasData() {
		return []compatibleSSEEvent{event}, nil
	}
	var p map[string]any
	_ = json.Unmarshal(event.Data(), &p)
	kind := stringField(p, "type")
	if len(s.pending) == 0 {
		item, _ := p["item"].(map[string]any)
		if kind != "response.output_item.added" || item["type"] != "message" || stringField(item, "id") == "" {
			return []compatibleSSEEvent{event}, nil
		}
		s.itemID = stringField(item, "id")
	}
	s.pending = append(s.pending, event)
	s.bytes += len(event.Data())
	if s.bytes > maxTotalBufferedFunctionArgsBytes {
		return nil, fmt.Errorf("tool envelope exceeds stream buffer limit")
	}
	if !s.candidate && kind == "response.output_text.delta" {
		s.prefix += stringField(p, "delta")
		prefix := strings.TrimSpace(s.prefix)
		s.candidate = xmlToolStart.MatchString(prefix)
		if !s.candidate && (len(prefix) > 192 || (prefix != "" && (!strings.HasPrefix(prefix, "<") || strings.Contains(prefix, ">")))) {
			return s.flush(), nil
		}
	}
	if !s.candidate && kind == "response.output_item.done" {
		item, _ := p["item"].(map[string]any)
		parts, _ := item["content"].([]any)
		var text strings.Builder
		for _, raw := range parts {
			if part, ok := raw.(map[string]any); ok {
				text.WriteString(stringField(part, "text"))
			}
		}
		s.candidate = xmlToolStart.MatchString(strings.TrimSpace(text.String()))
	}
	terminal := kind == "response.completed" || kind == "response.incomplete" || kind == "response.failed"
	if terminal || strings.TrimSpace(string(event.Data())) == "[DONE]" {
		if s.candidate {
			if response, ok := p["response"].(map[string]any); ok {
				return s.convert(response, p)
			}
		}
		return s.flush(), nil
	}
	if !s.candidate && kind == "response.output_item.done" {
		return s.flush(), nil
	}
	return nil, nil
}

func (s *xmlToolStream) flush() []compatibleSSEEvent {
	events := s.pending
	s.pending, s.bytes, s.prefix, s.itemID, s.candidate = nil, 0, "", "", false
	return events
}

func (s *xmlToolStream) convert(response, terminal map[string]any) ([]compatibleSSEEvent, error) {
	replaced := s.compatibility.promoteXMLResponse(response)
	if len(replaced) == 0 {
		return s.flush(), nil
	}
	indices := map[string]int{}
	items, _ := response["output"].([]any)
	for index, raw := range items {
		if item, ok := raw.(map[string]any); ok {
			indices[stringField(item, "id")] = index
		}
	}
	var output []compatibleSSEEvent
	for _, event := range s.flush() {
		var p map[string]any
		if json.Unmarshal(event.Data(), &p) != nil {
			output = append(output, event)
			continue
		}
		kind := stringField(p, "type")
		id := stringField(p, "item_id")
		if item, ok := p["item"].(map[string]any); ok {
			id = stringField(item, "id")
		}
		if calls, converted := replaced[id]; converted {
			if kind == "response.output_item.done" {
				for _, raw := range calls {
					call := raw.(map[string]any)
					output = append(output, xmlCallEvents(event, call, indices[stringField(call, "id")])...)
				}
			}
			continue
		}
		if _, exists := p["output_index"]; exists {
			if index, known := indices[id]; known {
				p["output_index"] = index
			}
		}
		if _, exists := p["response"]; exists && kind == stringField(terminal, "type") {
			p = terminal
		}
		encoded, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		event.SetData(encoded)
		output = append(output, event)
	}
	return output, nil
}

func xmlCallEvents(original compatibleSSEEvent, call map[string]any, index int) []compatibleSSEEvent {
	added := cloneJSONObject(call)
	added["arguments"], added["status"] = "", "in_progress"
	payloads := []map[string]any{
		{"type": "response.output_item.added", "output_index": index, "item": added},
		{"type": "response.function_call_arguments.delta", "output_index": index, "item_id": call["id"], "delta": call["arguments"]},
		{"type": "response.function_call_arguments.done", "output_index": index, "item_id": call["id"], "arguments": call["arguments"]},
		{"type": "response.output_item.done", "output_index": index, "item": call},
	}
	var source map[string]any
	_ = json.Unmarshal(original.Data(), &source)
	result := make([]compatibleSSEEvent, 0, len(payloads))
	for _, payload := range payloads {
		if sequence, exists := source["sequence_number"]; exists {
			payload["sequence_number"] = sequence
		}
		event := compatibleSSEEvent{Event: stringField(payload, "type")}
		data, _ := json.Marshal(payload)
		event.SetData(data)
		result = append(result, event)
	}
	return result
}
