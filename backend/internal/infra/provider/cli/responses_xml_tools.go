package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io"
	"regexp"
	"strings"
)

// Only an entire, declared tool envelope is executable. Quoted XML, examples,
// unknown tools, ambiguous parameter types and incomplete tags remain text.
var xmlToolStart = regexp.MustCompile(`^<(?:[A-Za-z_][\w.-]*:)?(?:tool_calls|tool_call|function_calls|function_call|invoke)(?:\s|>)`)
var xmlToolLoopTail = regexp.MustCompile(`^(?:persist leftover kwargs: \{[^{}\r\n]*\}\s*)+$`)

type toolXMLNode struct {
	name     string
	attrs    map[string]string
	text     string
	children []toolXMLNode
}

func readToolXML(d *xml.Decoder, start xml.StartElement, depth int) (toolXMLNode, bool) {
	n := toolXMLNode{name: start.Name.Local, attrs: map[string]string{}}
	if depth > 16 {
		return n, false
	}
	for _, attr := range start.Attr {
		if _, exists := n.attrs[attr.Name.Local]; exists {
			return n, false
		}
		n.attrs[attr.Name.Local] = attr.Value
	}
	var text strings.Builder
	for {
		token, err := d.Token()
		if err != nil {
			return n, false
		}
		switch value := token.(type) {
		case xml.StartElement:
			child, ok := readToolXML(d, value, depth+1)
			if !ok || len(n.children) >= 256 {
				return n, false
			}
			n.children = append(n.children, child)
		case xml.CharData:
			text.Write(value)
		case xml.EndElement:
			n.text = text.String()
			return n, value.Name == start.Name
		default:
			return n, false
		}
	}
}

func (c *responsesToolCompatibility) configureXMLTools(payload map[string]json.RawMessage) {
	c.xmlTools = map[string]any{}
	var choice any
	_ = json.Unmarshal(payload["tool_choice"], &choice)
	if choice == "none" {
		return
	}
	var forced string
	if object, ok := choice.(map[string]any); ok {
		forced = stringField(object, "name")
		if nested, ok := object["function"].(map[string]any); ok {
			forced = stringField(nested, "name")
		}
		if forced == "" || stringField(object, "type") != "function" {
			return
		}
	}
	var tools []map[string]any
	_ = json.Unmarshal(payload["tools"], &tools)
	for _, tool := range tools {
		name := stringField(tool, "name")
		identity, ok := c.aliases[name]
		if ok && identity.Kind == responsesFunctionTool && (forced == "" || forced == name) {
			c.xmlTools[name] = c.functionSchemas[name]
		}
	}
	c.xmlParallel = string(payload["parallel_tool_calls"]) != "false"
}

func (c *responsesToolCompatibility) parseXMLCalls(text, itemID string) ([]any, bool) {
	text = strings.TrimSpace(text)
	if len(text) > maxBufferedFunctionArgumentsBytes || !xmlToolStart.MatchString(text) {
		return nil, false
	}
	d := xml.NewDecoder(strings.NewReader(text))
	token, err := d.Token()
	start, ok := token.(xml.StartElement)
	if err != nil || !ok {
		return nil, false
	}
	root, ok := readToolXML(d, start, 0)
	if !ok {
		return nil, false
	}
	tail := strings.TrimSpace(text[d.InputOffset():])
	if tail != "" && !xmlToolLoopTail.MatchString(tail) {
		return nil, false
	}
	nodes := []toolXMLNode{root}
	if root.name == "tool_calls" || root.name == "function_calls" {
		if strings.TrimSpace(root.text) != "" {
			return nil, false
		}
		nodes = root.children
	}
	if len(nodes) == 0 || len(nodes) > 32 || (!c.xmlParallel && len(nodes) > 1) {
		return nil, false
	}
	calls := make([]any, 0, len(nodes))
	for index, node := range nodes {
		name, arguments, ok := c.xmlCallArguments(node)
		if !ok {
			return nil, false
		}
		seed, _ := json.Marshal([]any{itemID, index, name, arguments})
		hash := sha256.Sum256(seed)
		id := hex.EncodeToString(hash[:12])
		calls = append(calls, map[string]any{"type": "function_call", "id": "fc_xml_" + id, "call_id": "call_xml_" + id, "name": name, "arguments": arguments, "status": "completed"})
	}
	return calls, true
}

func (c *responsesToolCompatibility) xmlCallArguments(node toolXMLNode) (string, string, bool) {
	if node.name != "tool_call" && node.name != "function_call" && node.name != "invoke" {
		return "", "", false
	}
	if strings.TrimSpace(node.text) != "" {
		return "", "", false
	}
	name := node.attrs["name"]
	var parameters []toolXMLNode
	var jsonArgs string
	for _, child := range node.children {
		switch child.name {
		case "name", "tool_name":
			if name != "" || len(child.children) != 0 {
				return "", "", false
			}
			name = strings.TrimSpace(child.text)
		case "parameter":
			parameters = append(parameters, child)
		case "parameters", "arguments", "input":
			if jsonArgs != "" || len(parameters) != 0 {
				return "", "", false
			}
			if len(child.children) > 0 {
				if strings.TrimSpace(child.text) != "" {
					return "", "", false
				}
				parameters = child.children
			} else {
				jsonArgs = strings.TrimSpace(child.text)
			}
		default:
			return "", "", false
		}
	}
	schema, declared := c.xmlTools[name]
	if !declared {
		return "", "", false
	}
	root, ok := schema.(map[string]any)
	if !ok {
		return "", "", false
	}
	args := map[string]any{}
	if jsonArgs != "" {
		if len(parameters) != 0 || decodeXMLArgumentJSON(jsonArgs, &args) != nil || args == nil {
			return "", "", false
		}
	} else {
		properties, _ := root["properties"].(map[string]any)
		for _, parameter := range parameters {
			key := parameter.attrs["name"]
			if parameter.name != "parameter" || key == "" || len(parameter.children) != 0 {
				return "", "", false
			}
			if _, duplicate := args[key]; duplicate {
				return "", "", false
			}
			property, ok := properties[key].(map[string]any)
			if !ok {
				return "", "", false
			}
			if property["type"] == "string" {
				args[key] = parameter.text
			} else {
				var value any
				if decodeXMLArgumentJSON(parameter.text, &value) != nil {
					return "", "", false
				}
				args[key] = value
			}
		}
	}
	if !xmlArgumentTypes(args, root, root, 0) {
		return "", "", false
	}
	encoded, err := json.Marshal(args)
	return name, string(encoded), err == nil
}

func decodeXMLArgumentJSON(text string, target any) error {
	d := json.NewDecoder(strings.NewReader(text))
	d.UseNumber()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	return nil
}

// Validate types/required fields without inventing values. The caller still owns
// business-level tool validation, exactly as for native function calls.
func xmlArgumentTypes(value any, schema, root map[string]any, depth int) bool {
	if depth > 32 {
		return false
	}
	if ref, ok := schema["$ref"].(string); ok {
		resolved, exists := resolveLocalSchemaRef(root, ref)
		return exists && xmlArgumentTypes(value, resolved, root, depth+1)
	}
	switch schema["type"] {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	case "integer":
		n, ok := value.(json.Number)
		if !ok {
			return false
		}
		if _, err := n.Int64(); err == nil {
			return true
		}
		_, ok = normalizeIntegralNumber(n)
		return ok
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "array":
		items, ok := value.([]any)
		if !ok {
			return false
		}
		child, ok := schema["items"].(map[string]any)
		if !ok {
			return false
		}
		for _, item := range items {
			if !xmlArgumentTypes(item, child, root, depth+1) {
				return false
			}
		}
		return true
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		properties, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]any)
		for _, field := range required {
			name, ok := field.(string)
			if !ok {
				return false
			}
			if _, exists := object[name]; !exists {
				return false
			}
		}
		for key, item := range object {
			child, ok := properties[key].(map[string]any)
			if !ok {
				child, ok = schema["additionalProperties"].(map[string]any)
			}
			if !ok || !xmlArgumentTypes(item, child, root, depth+1) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func toolCallSignature(item map[string]any) string {
	if stringField(item, "type") != "function_call" {
		return ""
	}
	var args any
	if json.Unmarshal([]byte(stringField(item, "arguments")), &args) != nil {
		return ""
	}
	encoded, _ := json.Marshal([]any{item["name"], args})
	return string(encoded)
}

func (c *responsesToolCompatibility) promoteXMLResponse(response map[string]any) map[string][]any {
	replaced := map[string][]any{}
	items, _ := response["output"].([]any)
	if len(c.xmlTools) == 0 {
		return replaced
	}
	native := map[string]bool{}
	for _, raw := range items {
		if item, ok := raw.(map[string]any); ok {
			native[toolCallSignature(item)] = true
		}
	}
	output := make([]any, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok || item["type"] != "message" || item["role"] != "assistant" {
			output = append(output, raw)
			continue
		}
		parts, _ := item["content"].([]any)
		var text bytes.Buffer
		valid := len(parts) > 0
		for _, part := range parts {
			p, ok := part.(map[string]any)
			if !ok || p["type"] != "output_text" {
				valid = false
				break
			}
			text.WriteString(stringField(p, "text"))
		}
		calls, converted := c.parseXMLCalls(text.String(), stringField(item, "id"))
		if !valid || !converted {
			output = append(output, raw)
			continue
		}
		kept := make([]any, 0, len(calls))
		for _, call := range calls {
			if !native[toolCallSignature(call.(map[string]any))] {
				kept = append(kept, call)
			}
		}
		replaced[stringField(item, "id")] = kept
		output = append(output, kept...)
	}
	if len(replaced) > 0 {
		response["output"] = output
		if _, exists := response["output_text"]; exists {
			var remaining strings.Builder
			for _, raw := range output {
				item, _ := raw.(map[string]any)
				parts, _ := item["content"].([]any)
				for _, rawPart := range parts {
					if part, ok := rawPart.(map[string]any); ok && part["type"] == "output_text" {
						remaining.WriteString(stringField(part, "text"))
					}
				}
			}
			response["output_text"] = remaining.String()
		}
	}
	return replaced
}
