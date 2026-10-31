package internal

import (
	"fmt"
	"regexp"
)

// nodeIDPattern matches Figma node IDs: colon-separated integers e.g. "4029:12345",
// plus instance sublayer IDs e.g. "I60:3637;59:1069".
var nodeIDPattern = regexp.MustCompile(`^(\d+:\d+|I\d+:\d+(;\d+:\d+)+)$`)

// ValidNodeID reports whether s is a valid Figma node ID.
func ValidNodeID(s string) bool {
	return nodeIDPattern.MatchString(s)
}

// ValidateRPC validates an incoming RPC request against the tool's expected
// input shape. Returns an error string on failure, empty string if valid.
func ValidateRPC(tool string, nodeIDs []string, params map[string]interface{}) string {
	switch tool {
	case "get_node":
		if len(nodeIDs) == 0 || nodeIDs[0] == "" {
			return "nodeId is required"
		}
		if !ValidNodeID(nodeIDs[0]) {
			return fmt.Sprintf("nodeId must use colon format e.g. 4029:12345, got: %s", nodeIDs[0])
		}
		if msg := validateDepth(params); msg != "" {
			return msg
		}

	case "get_nodes_info":
		if len(nodeIDs) == 0 {
			return "nodeIds is required and must not be empty"
		}
		for _, id := range nodeIDs {
			if !ValidNodeID(id) {
				return fmt.Sprintf("invalid nodeId: %s — must use colon format e.g. 4029:12345", id)
			}
		}
		if msg := validateDepth(params); msg != "" {
			return msg
		}

	case "get_screenshot":
		for _, id := range nodeIDs {
			if !ValidNodeID(id) {
				return fmt.Sprintf("invalid nodeId: %s — must use colon format e.g. 4029:12345", id)
			}
		}
		if format, ok := params["format"].(string); ok {
			if !validExportFormat(format) {
				return fmt.Sprintf("format must be PNG, SVG, JPG, or PDF, got: %s", format)
			}
		}

	case "save_screenshots":
		items, ok := params["items"]
		if !ok {
			return "items is required"
		}
		itemList, ok := items.([]interface{})
		if !ok || len(itemList) == 0 {
			return "items must be a non-empty array"
		}
		for i, item := range itemList {
			m, ok := item.(map[string]interface{})
			if !ok {
				return fmt.Sprintf("items[%d] must be an object", i)
			}
			nodeID, _ := m["nodeId"].(string)
			if !ValidNodeID(nodeID) {
				return fmt.Sprintf("items[%d].nodeId must use colon format e.g. 4029:12345", i)
			}
			outputPath, _ := m["outputPath"].(string)
			if outputPath == "" {
				return fmt.Sprintf("items[%d].outputPath is required", i)
			}
		}

	case "get_design_context":
		if msg := validateDepth(params); msg != "" {
			return msg
		}

	case "create_nodes":
		if msg := validateOptionalNodeID(params, "parentId"); msg != "" {
			return msg
		}
		if nodes, ok := params["nodes"].([]interface{}); !ok || len(nodes) == 0 {
			return "nodes must be a non-empty array"
		}

	case "update_nodes":
		updates, ok := params["updates"].([]interface{})
		if !ok || len(updates) == 0 {
			return "updates must be a non-empty array"
		}
		for i, u := range updates {
			m, ok := u.(map[string]interface{})
			if !ok {
				return fmt.Sprintf("updates[%d] must be an object", i)
			}
			if id, _ := m["nodeId"].(string); !ValidNodeID(id) {
				return fmt.Sprintf("updates[%d].nodeId must use colon format e.g. 4029:12345", i)
			}
			if msg := validateOptionalNodeID(m, "parentId"); msg != "" {
				return fmt.Sprintf("updates[%d]: %s", i, msg)
			}
		}

	case "delete_nodes", "set_selection", "clone_node":
		if len(nodeIDs) == 0 {
			return "nodeIds is required and must not be empty"
		}
		for _, id := range nodeIDs {
			if !ValidNodeID(id) {
				return fmt.Sprintf("invalid nodeId: %s — must use colon format e.g. 4029:12345", id)
			}
		}
		if msg := validateOptionalNodeID(params, "parentId"); msg != "" {
			return msg
		}

	case "animate_nodes":
		tracks, _ := params["tracks"].([]interface{})
		styles, _ := params["styles"].([]interface{})
		if len(tracks) == 0 && len(styles) == 0 {
			return "tracks or styles must be a non-empty array"
		}
		for _, list := range []struct {
			name  string
			items []interface{}
		}{{"tracks", tracks}, {"styles", styles}} {
			for i, item := range list.items {
				m, ok := item.(map[string]interface{})
				if !ok {
					return fmt.Sprintf("%s[%d] must be an object", list.name, i)
				}
				if id, _ := m["nodeId"].(string); !ValidNodeID(id) {
					return fmt.Sprintf("%s[%d].nodeId must use colon format e.g. 4029:12345", list.name, i)
				}
			}
		}
		if d, ok := params["duration"].(float64); ok && d <= 0 {
			return "duration must be greater than zero"
		}

	case "get_animations":
		for _, id := range nodeIDs {
			if !ValidNodeID(id) {
				return fmt.Sprintf("invalid nodeId: %s — must use colon format e.g. 4029:12345", id)
			}
		}

	case "create_instance":
		id, _ := params["componentId"].(string)
		key, _ := params["componentKey"].(string)
		if id == "" && key == "" {
			return "componentId or componentKey is required"
		}
		if msg := validateOptionalNodeID(params, "componentId"); msg != "" {
			return msg
		}
		if msg := validateOptionalNodeID(params, "parentId"); msg != "" {
			return msg
		}

	case "scan_text_nodes", "scan_nodes_by_types":
		nodeID, _ := params["nodeId"].(string)
		if nodeID == "" {
			return "nodeId is required"
		}
		if !ValidNodeID(nodeID) {
			return fmt.Sprintf("nodeId must use colon format e.g. 4029:12345, got: %s", nodeID)
		}
		if tool == "scan_nodes_by_types" {
			types, ok := params["types"].([]interface{})
			if !ok || len(types) == 0 {
				return "types must be a non-empty array"
			}
		}
	}

	return ""
}

func validExportFormat(f string) bool {
	switch f {
	case "PNG", "SVG", "JPG", "PDF":
		return true
	}
	return false
}

// validateDepth checks the optional depth param shared by tree-returning tools.
func validateDepth(params map[string]interface{}) string {
	if depth, ok := params["depth"].(float64); ok && depth < 0 {
		return "depth must be a non-negative number"
	}
	return ""
}

// validateOptionalNodeID checks params[key] is a valid node ID when present.
func validateOptionalNodeID(params map[string]interface{}, key string) string {
	if id, ok := params[key].(string); ok && id != "" && !ValidNodeID(id) {
		return fmt.Sprintf("%s must use colon format e.g. 4029:12345, got: %s", key, id)
	}
	return ""
}
