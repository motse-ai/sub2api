package apicompat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ResponsesNamespaceName identifies a callable child in a Responses namespace.
// It aliases the chat bridge mapping so both native and bridged paths share one
// namespace identity contract.
type ResponsesNamespaceName = NamespacedToolName

// FlattenResponsesNamespaces converts Codex private namespace declarations into
// top-level Responses tools and rewrites namespace-qualified request calls.
func FlattenResponsesNamespaces(req map[string]any) (map[string]ResponsesNamespaceName, bool, error) {
	return FlattenResponsesNamespacesExcept(req, nil)
}

// FlattenResponsesNamespacesExcept is FlattenResponsesNamespaces with a set of
// service-owned namespace names that must remain native in the request.
func FlattenResponsesNamespacesExcept(req map[string]any, preserved map[string]bool) (map[string]ResponsesNamespaceName, bool, error) {
	if req == nil {
		return nil, false, nil
	}
	tools, ok := req["tools"].([]any)
	if !ok || len(tools) == 0 {
		return nil, false, nil
	}

	topLevel := make(map[string]bool)
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ := strings.TrimSpace(stringValue(tool["type"]))
		name := strings.TrimSpace(stringValue(tool["name"]))
		if (typ == "function" || typ == "custom") && name != "" {
			topLevel[name] = true
		}
	}

	names := make(map[string]ResponsesNamespaceName)
	kinds := make(map[string]string)
	flattened := make([]any, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(stringValue(tool["type"])) != "namespace" {
			flattened = append(flattened, raw)
			continue
		}
		namespace := strings.TrimSpace(stringValue(tool["name"]))
		if preserved[namespace] {
			flattened = append(flattened, raw)
			continue
		}
		if namespace == "" {
			continue
		}
		for _, rawChild := range namespaceChildren(tool) {
			child, _ := rawChild.(map[string]any)
			kind := strings.TrimSpace(stringValue(child["type"]))
			name := strings.TrimSpace(stringValue(child["name"]))
			if (kind != "function" && kind != "custom") || name == "" {
				continue
			}
			flat := flattenNamespaceToolName(namespace, name)
			entry := ResponsesNamespaceName{Namespace: namespace, Name: name}
			if topLevel[flat] {
				return nil, false, fmt.Errorf("namespace tool %q/%q flattens to %q which conflicts with a top-level tool of the same name; this upstream cannot disambiguate them, rename one of the tools", namespace, name, flat)
			}
			if prev, exists := names[flat]; exists {
				if prev != entry || kinds[flat] != kind {
					return nil, false, fmt.Errorf("namespace tools %q/%q (%s) and %q/%q (%s) both flatten to %q; this upstream cannot disambiguate them, rename one of the tools", prev.Namespace, prev.Name, kinds[flat], namespace, name, kind, flat)
				}
				continue
			}
			names[flat], kinds[flat] = entry, kind
			flatChild := copyClientTool(child)
			flatChild["name"] = flat
			flattened = append(flattened, flatChild)
		}
	}
	if len(names) == 0 {
		return nil, false, nil
	}
	req["tools"] = flattened
	rewriteNamespaceQualifiedCalls(req["input"], names)
	rewriteNamespaceToolChoice(req, names, preserved)
	return names, true, nil
}

func rewriteNamespaceToolChoice(req map[string]any, names map[string]ResponsesNamespaceName, preserved map[string]bool) bool {
	choice, ok := req["tool_choice"].(map[string]any)
	if !ok {
		return false
	}
	choiceNamespace := strings.TrimSpace(stringValue(choice["name"]))
	if strings.TrimSpace(stringValue(choice["type"])) == "namespace" && !preserved[choiceNamespace] {
		// Flattened tools can be selected individually, but their group no longer exists upstream.
		req["tool_choice"] = "auto"
		return true
	}
	return rewriteNamespaceQualifiedCall(choice, names)
}

// RestoreResponsesNamespaceCalls restores flattened function calls in a JSON
// Responses payload to the namespace/name identity expected by Codex.
func RestoreResponsesNamespaceCalls(payload []byte, names map[string]ResponsesNamespaceName) ([]byte, bool, error) {
	if len(payload) == 0 || len(names) == 0 {
		return payload, false, nil
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return payload, false, err
	}
	changed := restoreResponsesNamespaceValue(value, names)
	if !changed {
		return payload, false, nil
	}
	var rebuilt bytes.Buffer
	encoder := json.NewEncoder(&rebuilt)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return payload, false, err
	}
	return bytes.TrimSuffix(rebuilt.Bytes(), []byte("\n")), true, nil
}

func namespaceChildren(tool map[string]any) []any {
	if children, ok := tool["tools"].([]any); ok && len(children) > 0 {
		return children
	}
	children, _ := tool["children"].([]any)
	return children
}

func rewriteNamespaceQualifiedCalls(value any, names map[string]ResponsesNamespaceName) bool {
	changed := false
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			changed = rewriteNamespaceQualifiedCalls(item, names) || changed
		}
	case map[string]any:
		if typ := strings.TrimSpace(stringValue(typed["type"])); typ == "function_call" || typ == "custom_tool_call" {
			changed = rewriteNamespaceQualifiedCall(typed, names)
		}
		for _, child := range typed {
			changed = rewriteNamespaceQualifiedCalls(child, names) || changed
		}
	}
	return changed
}

func rewriteNamespaceQualifiedCall(item map[string]any, names map[string]ResponsesNamespaceName) bool {
	namespace := strings.TrimSpace(stringValue(item["namespace"]))
	name := strings.TrimSpace(stringValue(item["name"]))
	if namespace == "" || name == "" {
		return false
	}
	flat := flattenNamespaceToolName(namespace, name)
	entry, ok := names[flat]
	if !ok || entry.Namespace != namespace || entry.Name != name {
		return false
	}
	item["name"] = flat
	delete(item, "namespace")
	return true
}

func restoreResponsesNamespaceValue(value any, names map[string]ResponsesNamespaceName) bool {
	changed := false
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			changed = restoreResponsesNamespaceValue(item, names) || changed
		}
	case map[string]any:
		if typ := strings.TrimSpace(stringValue(typed["type"])); typ == "function_call" || typ == "custom_tool_call" {
			if entry, ok := names[strings.TrimSpace(stringValue(typed["name"]))]; ok {
				typed["name"] = entry.Name
				typed["namespace"] = entry.Namespace
				changed = true
			}
		}
		for _, child := range typed {
			changed = restoreResponsesNamespaceValue(child, names) || changed
		}
	}
	return changed
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
