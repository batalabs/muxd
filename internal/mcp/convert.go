package mcp

import (
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/batalabs/muxd/internal/provider"
)

// ToToolSpec converts an MCP Tool to a muxd provider.ToolSpec with a
// namespaced name. The InputSchema (typically map[string]any from JSON
// unmarshalling) is converted to ToolProp map.
func ToToolSpec(serverName string, tool *mcpsdk.Tool) provider.ToolSpec {
	spec := provider.ToolSpec{
		Name:        NamespacedName(serverName, tool.Name),
		Description: tool.Description,
	}

	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		// If InputSchema isn't a map, return spec with no properties.
		return spec
	}

	props, required := extractProperties(schema)
	spec.Properties = props
	spec.Required = required
	return spec
}

// extractProperties extracts properties and required fields from a JSON Schema map.
func extractProperties(schema map[string]any) (map[string]provider.ToolProp, []string) {
	props := map[string]provider.ToolProp{}

	if propsMap, ok := schema["properties"].(map[string]any); ok {
		for name, raw := range propsMap {
			propMap, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			props[name] = convertProp(propMap)
		}
	}

	var required []string
	if reqList, ok := schema["required"].([]any); ok {
		for _, r := range reqList {
			if s, ok := r.(string); ok {
				required = append(required, s)
			}
		}
	}

	return props, required
}

// convertProp converts a single JSON Schema property map to a ToolProp.
func convertProp(propMap map[string]any) provider.ToolProp {
	tp := provider.ToolProp{}

	if t, ok := propMap["type"].(string); ok {
		tp.Type = t
	} else {
		// Fallback for complex types (oneOf, anyOf, allOf).
		tp.Type = "object"
	}

	if d, ok := propMap["description"].(string); ok {
		tp.Description = d
	}

	if enumList, ok := propMap["enum"].([]any); ok {
		for _, e := range enumList {
			tp.Enum = append(tp.Enum, fmt.Sprintf("%v", e))
		}
	}

	// Handle array items
	if tp.Type == "array" {
		if items, ok := propMap["items"].(map[string]any); ok {
			itemProp := convertProp(items)
			tp.Items = &itemProp
		}
	}

	// Handle nested object properties
	if tp.Type == "object" {
		if nested, ok := propMap["properties"].(map[string]any); ok {
			tp.Properties = map[string]provider.ToolProp{}
			for name, raw := range nested {
				if pm, ok := raw.(map[string]any); ok {
					tp.Properties[name] = convertProp(pm)
				}
			}
		}
		if reqList, ok := propMap["required"].([]any); ok {
			for _, r := range reqList {
				if s, ok := r.(string); ok {
					tp.Required = append(tp.Required, s)
				}
			}
		}
	}

	return tp
}
