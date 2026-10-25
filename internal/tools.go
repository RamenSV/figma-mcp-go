package internal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterTools registers all MCP tools on the server: read tools, write
// tools (create/update/delete/clone/instance/selection), and get_result_chunk.
func RegisterTools(s *server.MCPServer, node *Node) {
	// ── Document & Selection ─────────────────────────────────────────────

	s.AddTool(mcp.NewTool("get_document",
		mcp.WithDescription("Get the current Figma page document tree"),
	), makeHandler(node, "get_document", nil, nil))

	s.AddTool(mcp.NewTool("get_metadata",
		mcp.WithDescription("Get metadata about the current Figma document: file name, pages, current page"),
	), makeHandler(node, "get_metadata", nil, nil))

	s.AddTool(mcp.NewTool("get_selection",
		mcp.WithDescription("Get the currently selected nodes in Figma"),
	), makeHandler(node, "get_selection", nil, nil))

	s.AddTool(mcp.NewTool("get_node",
		mcp.WithDescription("Get a specific Figma node by ID. Must use colon format e.g. '4029:12345', never hyphens."),
		mcp.WithString("nodeId",
			mcp.Required(),
			mcp.Description("Node ID in colon format e.g. '4029:12345'"),
		),
		mcp.WithNumber("depth",
			mcp.Description("Optional: how many levels of children to include. Omit for the full subtree."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		nodeID, _ := req.Params.Arguments["nodeId"].(string)
		resp, err := node.Send(ctx, "get_node", []string{nodeID}, depthParams(req))
		return renderResponse(resp, err)
	})

	s.AddTool(mcp.NewTool("get_nodes_info",
		mcp.WithDescription("Get detailed information about multiple Figma nodes by ID in a single call."),
		mcp.WithArray("nodeIds",
			mcp.Required(),
			mcp.Description("List of node IDs in colon format e.g. ['4029:12345', '4029:67890']"),
		),
		mcp.WithNumber("depth",
			mcp.Description("Optional: how many levels of children to include per node. Omit for the full subtree."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		raw, _ := req.Params.Arguments["nodeIds"].([]interface{})
		nodeIDs := toStringSlice(raw)
		resp, err := node.Send(ctx, "get_nodes_info", nodeIDs, depthParams(req))
		return renderResponse(resp, err)
	})

	s.AddTool(mcp.NewTool("get_design_context",
		mcp.WithDescription("Get a depth-limited tree of the current selection or page. More token-efficient than get_document for large files."),
		mcp.WithNumber("depth",
			mcp.Description("How many levels deep to traverse (default 2)"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := map[string]interface{}{}
		if d, ok := req.Params.Arguments["depth"].(float64); ok && d > 0 {
			params["depth"] = d
		}
		resp, err := node.Send(ctx, "get_design_context", nil, params)
		return renderResponse(resp, err)
	})

	s.AddTool(mcp.NewTool("scan_text_nodes",
		mcp.WithDescription("Scan all TEXT nodes in a subtree. Useful for extracting all copy from a component or frame."),
		mcp.WithString("nodeId",
			mcp.Required(),
			mcp.Description("Root node ID to scan from, colon format e.g. '4029:12345'"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		nodeID, _ := req.Params.Arguments["nodeId"].(string)
		resp, err := node.Send(ctx, "scan_text_nodes", nil, map[string]interface{}{"nodeId": nodeID})
		return renderResponse(resp, err)
	})

	s.AddTool(mcp.NewTool("scan_nodes_by_types",
		mcp.WithDescription("Find all nodes matching specific types (e.g. FRAME, COMPONENT, INSTANCE) within a subtree."),
		mcp.WithString("nodeId",
			mcp.Required(),
			mcp.Description("Root node ID to scan from, colon format e.g. '4029:12345'"),
		),
		mcp.WithArray("types",
			mcp.Required(),
			mcp.Description("Node types to find e.g. ['FRAME', 'COMPONENT', 'INSTANCE']"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		nodeID, _ := req.Params.Arguments["nodeId"].(string)
		raw, _ := req.Params.Arguments["types"].([]interface{})
		resp, err := node.Send(ctx, "scan_nodes_by_types", nil, map[string]interface{}{
			"nodeId": nodeID,
			"types":  raw,
		})
		return renderResponse(resp, err)
	})

	// ── Styles & Variables ───────────────────────────────────────────────

	s.AddTool(mcp.NewTool("get_styles",
		mcp.WithDescription("Get all local styles in the document: paint, text, effect, and grid styles"),
	), makeHandler(node, "get_styles", nil, nil))

	s.AddTool(mcp.NewTool("get_variable_defs",
		mcp.WithDescription("Get all local variable definitions: collections, modes, and values. Variables are Figma's design token system."),
	), makeHandler(node, "get_variable_defs", nil, nil))

	s.AddTool(mcp.NewTool("get_local_components",
		mcp.WithDescription("Get all components defined in the current Figma file."),
	), makeHandler(node, "get_local_components", nil, nil))

	s.AddTool(mcp.NewTool("get_annotations",
		mcp.WithDescription("Get all dev-mode annotations in the current document or on a specific node."),
		mcp.WithString("nodeId",
			mcp.Description("Optional node ID to filter annotations, colon format e.g. '4029:12345'"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := map[string]interface{}{}
		if id, ok := req.Params.Arguments["nodeId"].(string); ok && id != "" {
			params["nodeId"] = id
		}
		resp, err := node.Send(ctx, "get_annotations", nil, params)
		return renderResponse(resp, err)
	})

	// ── Export ───────────────────────────────────────────────────────────

	s.AddTool(mcp.NewTool("get_screenshot",
		mcp.WithDescription("Export a screenshot of selected or specific nodes. Returns base64-encoded image data."),
		mcp.WithArray("nodeIds",
			mcp.Description("Optional node IDs to export, colon format. If empty, exports current selection."),
		),
		mcp.WithString("format",
			mcp.Description("Export format: PNG (default), SVG, JPG, or PDF"),
		),
		mcp.WithNumber("scale",
			mcp.Description("Export scale for raster formats (default 2)"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		raw, _ := req.Params.Arguments["nodeIds"].([]interface{})
		nodeIDs := toStringSlice(raw)
		params := map[string]interface{}{}
		if f, ok := req.Params.Arguments["format"].(string); ok && f != "" {
			params["format"] = f
		}
		if s, ok := req.Params.Arguments["scale"].(float64); ok && s > 0 {
			params["scale"] = s
		}
		resp, err := node.Send(ctx, "get_screenshot", nodeIDs, params)
		return renderResponse(resp, err)
	})

	s.AddTool(mcp.NewTool("save_screenshots",
		mcp.WithDescription("Export screenshots for multiple nodes and save them to the local filesystem. Returns metadata only (no base64)."),
		mcp.WithArray("items",
			mcp.Required(),
			mcp.Description("List of {nodeId, outputPath, format?, scale?} objects"),
		),
		mcp.WithString("format",
			mcp.Description("Default export format: PNG (default), SVG, JPG, or PDF"),
		),
		mcp.WithNumber("scale",
			mcp.Description("Default export scale for raster formats (default 2)"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return executeSaveScreenshots(ctx, node, req)
	})

	// ── Write ────────────────────────────────────────────────────────────

	s.AddTool(mcp.NewTool("create_nodes",
		mcp.WithDescription(`Create one or more nodes (optionally nested) in Figma. Each node spec: {type, ...props, children?: [spec]}.
Types: FRAME, RECTANGLE, ELLIPSE, LINE, TEXT, COMPONENT, SVG (pass the markup in svg), PAGE (creates a new page; parentId ignored), FROM_NODE (converts node nodeId into a component under parentId), COMPONENT_SET (children are COMPONENTs named \"Prop=Value\", combined as variants).
`+nodePropsDoc),
		mcp.WithString("parentId",
			mcp.Description("Optional parent node ID (colon format). Defaults to the current page."),
		),
		mcp.WithArray("nodes",
			mcp.Required(),
			mcp.Description("List of node specs to create, e.g. [{\"type\":\"FRAME\",\"name\":\"Card\",\"width\":320,\"height\":200,\"fills\":\"#FFFFFF\",\"layoutMode\":\"VERTICAL\",\"padding\":16,\"children\":[{\"type\":\"TEXT\",\"characters\":\"Hello\",\"fontSize\":24}]}]"),
			mcp.Items(map[string]interface{}{"type": "object"}),
		),
	), passParams(node, "create_nodes"))

	s.AddTool(mcp.NewTool("update_nodes",
		mcp.WithDescription(`Update properties of existing nodes. Each update: {nodeId, ...props}. Add parentId (and optional index) to move a node into another parent.
`+nodePropsDoc),
		mcp.WithArray("updates",
			mcp.Required(),
			mcp.Description("List of updates, e.g. [{\"nodeId\":\"1:2\",\"characters\":\"New title\",\"fills\":\"#111111\"}]"),
			mcp.Items(map[string]interface{}{"type": "object"}),
		),
	), passParams(node, "update_nodes"))

	s.AddTool(mcp.NewTool("delete_nodes",
		mcp.WithDescription("Delete nodes by ID. Can be undone in Figma with Cmd+Z."),
		mcp.WithArray("nodeIds",
			mcp.Required(),
			mcp.Description("Node IDs to delete, colon format"),
			mcp.Items(map[string]interface{}{"type": "string"}),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		raw, _ := req.Params.Arguments["nodeIds"].([]interface{})
		resp, err := node.Send(ctx, "delete_nodes", toStringSlice(raw), nil)
		return renderResponse(resp, err)
	})

	s.AddTool(mcp.NewTool("clone_node",
		mcp.WithDescription("Duplicate a node. The copy stays in the same parent unless parentId is given."),
		mcp.WithString("nodeId", mcp.Required(), mcp.Description("Node ID to clone, colon format")),
		mcp.WithString("parentId", mcp.Description("Optional new parent node ID")),
		mcp.WithNumber("index", mcp.Description("Optional child index within the parent")),
		mcp.WithNumber("x", mcp.Description("Optional x position")),
		mcp.WithNumber("y", mcp.Description("Optional y position")),
		mcp.WithString("name", mcp.Description("Optional name for the copy")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		nodeID, _ := req.Params.Arguments["nodeId"].(string)
		params := map[string]interface{}{}
		for k, v := range req.Params.Arguments {
			if k != "nodeId" {
				params[k] = v
			}
		}
		resp, err := node.Send(ctx, "clone_node", []string{nodeID}, params)
		return renderResponse(resp, err)
	})

	s.AddTool(mcp.NewTool("create_instance",
		mcp.WithDescription("Create an instance of a component. Use componentId for local components (see get_local_components) or componentKey for published library components."),
		mcp.WithString("componentId", mcp.Description("Local component node ID, colon format")),
		mcp.WithString("componentKey", mcp.Description("Published component key")),
		mcp.WithString("parentId", mcp.Description("Optional parent node ID. Defaults to the current page.")),
		mcp.WithNumber("index", mcp.Description("Optional child index within the parent")),
		mcp.WithNumber("x", mcp.Description("Optional x position")),
		mcp.WithNumber("y", mcp.Description("Optional y position")),
		mcp.WithString("name", mcp.Description("Optional instance name")),
	), passParams(node, "create_instance"))

	s.AddTool(mcp.NewTool("set_selection",
		mcp.WithDescription("Select nodes on the current page and scroll the viewport to them."),
		mcp.WithArray("nodeIds",
			mcp.Required(),
			mcp.Description("Node IDs to select, colon format"),
			mcp.Items(map[string]interface{}{"type": "string"}),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		raw, _ := req.Params.Arguments["nodeIds"].([]interface{})
		resp, err := node.Send(ctx, "set_selection", toStringSlice(raw), nil)
		return renderResponse(resp, err)
	})

	// ── Large results ────────────────────────────────────────────────────

	s.AddTool(mcp.NewTool("get_result_chunk",
		mcp.WithDescription("Fetch the next chunk of a large tool result. When a result is too big for one response it is split losslessly along the node tree; the first response includes resultId, totalChunks and howToRead. Each chunk holds complete nodes as items {path, parentId, value}; stubs marked \"$chunked\": true (with id/name/type) are filled by later items at the same path. Fetch every chunk to read the full design."),
		mcp.WithString("resultId",
			mcp.Required(),
			mcp.Description("resultId from the chunked response"),
		),
		mcp.WithNumber("chunk",
			mcp.Required(),
			mcp.Description("Zero-based chunk index to fetch"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, _ := req.Params.Arguments["resultId"].(string)
		idx, _ := req.Params.Arguments["chunk"].(float64)
		return chunkResult(id, int(idx))
	})
}

// RegisterPrompts registers MCP prompts on the server.
func RegisterPrompts(s *server.MCPServer) {
	s.AddPrompt(mcp.NewPrompt("read_design_strategy",
		mcp.WithPromptDescription("Best practices for reading Figma designs with figma-mcp-go"),
	), func(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return mcp.NewGetPromptResult(
			"Best practices for reading Figma designs",
			[]mcp.PromptMessage{
				mcp.NewPromptMessage(
					mcp.RoleUser,
					mcp.NewTextContent(`To effectively read a Figma design with figma-mcp-go:

1. Start with get_metadata — understand file name, pages, and current page
2. Use get_design_context (depth=2) for a summary of the current selection or page
3. Drill into specific nodes with get_node or get_nodes_info (prefer batch over single calls)
4. For text-heavy components, use scan_text_nodes to collect all copy at once
5. Use scan_nodes_by_types to find all FRAME/COMPONENT/INSTANCE nodes in a subtree
6. Call get_styles and get_variable_defs once per session to understand the design system
7. Call get_screenshot last and only when visual confirmation is needed — it is expensive
8. Node IDs use colon format: 4029:12345 — never use hyphens
9. get_design_context is more token-efficient than get_document for large files
10. get_local_components lists all components — use with get_node to inspect internals`),
				),
			},
		), nil
	})
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// makeHandler creates a simple tool handler with no parameters.
func makeHandler(node *Node, command string, nodeIDs []string, params map[string]interface{}) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := node.Send(ctx, command, nodeIDs, params)
		return renderResponse(resp, err)
	}
}

// renderResponse converts a BridgeResponse into an MCP tool result.
func renderResponse(resp BridgeResponse, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if resp.Error != "" {
		return mcp.NewToolResultError(resp.Error), nil
	}
	text, err := json.Marshal(resp.Data)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal response: %v", err)), nil
	}
	if len(text) > maxResultBytes() {
		return storeChunked(resp.Data)
	}
	return mcp.NewToolResultText(string(text)), nil
}

// nodePropsDoc describes the properties accepted by create_nodes and update_nodes.
const nodePropsDoc = `Supported props (all optional):
- name, x, y, width, height, rotation, opacity, visible, locked
- fills, strokes: "#RRGGBB" / "#RRGGBBAA", {color, opacity}, {image: url or imageBase64: base64 PNG/JPG, scaleMode (FILL|FIT|CROP|TILE)}, {gradient: [{color, position}], angle (90 = top to bottom)}, or an array of those; [] clears. Image URLs must be on images.unsplash.com or images.pexels.com
- strokeWeight, strokeAlign (INSIDE|OUTSIDE|CENTER), dashPattern ([dash, gap] e.g. [6, 4]), cornerRadius, topLeftRadius, topRightRadius, bottomLeftRadius, bottomRightRadius, clipsContent
- arcData (ELLIPSE): {startingAngle, endingAngle in degrees, 0 = 3 o'clock, clockwise; innerRadius 0..1} for rings/arcs
- effects: [{type: DROP_SHADOW|INNER_SHADOW, color "#RRGGBBAA", x, y, radius, spread}] or [{type: LAYER_BLUR|BACKGROUND_BLUR, radius}]; [] clears
- fillsFrom: node ID whose fills are copied (reuse image fills)
- reactions (prototype): [{to: nodeId, navigation: NAVIGATE|OVERLAY|SWAP|SCROLL_TO|BACK|CLOSE, transition: SMART_ANIMATE|DISSOLVE|MOVE_IN|SLIDE_IN|INSTANT, direction, duration ms, trigger: ON_CLICK|ON_HOVER|AFTER_TIMEOUT, timeout ms}]; [] clears
- flowStartName (top-level frame): marks it as a prototype flow start with that name; "" removes it
- Auto layout: layoutMode (NONE|HORIZONTAL|VERTICAL), layoutWrap, itemSpacing, counterAxisSpacing, padding (number or {top,right,bottom,left}), primaryAxisAlignItems (MIN|CENTER|MAX|SPACE_BETWEEN), counterAxisAlignItems (MIN|CENTER|MAX|BASELINE), primaryAxisSizingMode / counterAxisSizingMode (FIXED|AUTO)
- Child of auto layout: layoutSizingHorizontal / layoutSizingVertical (FIXED|HUG|FILL), layoutPositioning (AUTO|ABSOLUTE)
- TEXT: fillRanges ([{start, end, fills}] colours part of the text), characters, fontSize, fontFamily, fontStyle (e.g. "Bold"), textDecoration (NONE|UNDERLINE|STRIKETHROUGH), textAlignHorizontal, textAlignVertical, textAutoResize, lineHeight (px or {unit,value}), letterSpacing
All changes can be undone in Figma with Cmd+Z.`

// passParams creates a handler that forwards all tool arguments as params.
func passParams(node *Node, command string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := node.Send(ctx, command, nil, req.Params.Arguments)
		return renderResponse(resp, err)
	}
}

// depthParams extracts the optional depth argument for tree-returning tools.
func depthParams(req mcp.CallToolRequest) map[string]interface{} {
	if d, ok := req.Params.Arguments["depth"].(float64); ok {
		return map[string]interface{}{"depth": d}
	}
	return nil
}

// toStringSlice converts []interface{} to []string.
func toStringSlice(raw []interface{}) []string {
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// ── save_screenshots ─────────────────────────────────────────────────────────

type saveItem struct {
	NodeID     string `json:"nodeId"`
	OutputPath string `json:"outputPath"`
	Format     string `json:"format,omitempty"`
	Scale      float64 `json:"scale,omitempty"`
}

type saveResult struct {
	Index        int     `json:"index"`
	NodeID       string  `json:"nodeId"`
	NodeName     string  `json:"nodeName,omitempty"`
	OutputPath   string  `json:"outputPath"`
	Format       string  `json:"format,omitempty"`
	Width        float64 `json:"width,omitempty"`
	Height       float64 `json:"height,omitempty"`
	BytesWritten int     `json:"bytesWritten,omitempty"`
	Success      bool    `json:"success"`
	Error        string  `json:"error,omitempty"`
}

func executeSaveScreenshots(ctx context.Context, node *Node, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rawItems, _ := req.Params.Arguments["items"].([]interface{})
	defaultFormat, _ := req.Params.Arguments["format"].(string)
	defaultScale, _ := req.Params.Arguments["scale"].(float64)

	workDir, err := os.Getwd()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("getwd: %v", err)), nil
	}

	results := make([]saveResult, 0, len(rawItems))
	succeeded, failed := 0, 0

	for i, rawItem := range rawItems {
		item, err := parseSaveItem(rawItem)
		if err != nil {
			results = append(results, saveResult{Index: i, Error: err.Error()})
			failed++
			continue
		}

		r := saveScreenshotItem(ctx, node, item, i, workDir, defaultFormat, defaultScale)
		results = append(results, r)
		if r.Success {
			succeeded++
		} else {
			failed++
		}
	}

	out, _ := json.Marshal(map[string]interface{}{
		"total":     len(results),
		"succeeded": succeeded,
		"failed":    failed,
		"hasErrors": failed > 0,
		"results":   results,
	})
	return mcp.NewToolResultText(string(out)), nil
}

func saveScreenshotItem(ctx context.Context, node *Node, item saveItem, index int, workDir, defaultFormat string, defaultScale float64) saveResult {
	resolvedPath, err := resolveOutputPath(item.OutputPath, workDir)
	if err != nil {
		return saveResult{Index: index, NodeID: item.NodeID, OutputPath: item.OutputPath, Error: err.Error()}
	}

	format := coalesce(item.Format, defaultFormat)
	inferredFormat := inferFormat(resolvedPath)
	if format == "" {
		format = inferredFormat
	}
	if format == "" {
		format = "PNG"
	}
	if inferredFormat != "" && format != inferredFormat {
		return saveResult{Index: index, NodeID: item.NodeID, OutputPath: resolvedPath,
			Error: fmt.Sprintf("format %s conflicts with file extension %s", format, inferredFormat)}
	}

	scale := item.Scale
	if scale <= 0 {
		scale = defaultScale
	}

	params := map[string]interface{}{"format": format}
	if scale > 0 {
		params["scale"] = scale
	}

	resp, err := node.Send(ctx, "get_screenshot", []string{item.NodeID}, params)
	if err != nil {
		return saveResult{Index: index, NodeID: item.NodeID, OutputPath: resolvedPath, Error: err.Error()}
	}
	if resp.Error != "" {
		return saveResult{Index: index, NodeID: item.NodeID, OutputPath: resolvedPath, Error: resp.Error}
	}

	export, err := extractScreenshotExport(resp.Data)
	if err != nil {
		return saveResult{Index: index, NodeID: item.NodeID, OutputPath: resolvedPath, Error: err.Error()}
	}

	bytes, err := writeBase64(export.Base64, resolvedPath)
	if err != nil {
		return saveResult{Index: index, NodeID: item.NodeID, OutputPath: resolvedPath, Error: err.Error()}
	}

	return saveResult{
		Index:        index,
		NodeID:       export.NodeID,
		NodeName:     export.NodeName,
		OutputPath:   resolvedPath,
		Format:       format,
		Width:        export.Width,
		Height:       export.Height,
		BytesWritten: bytes,
		Success:      true,
	}
}

type screenshotExport struct {
	NodeID   string  `json:"nodeId"`
	NodeName string  `json:"nodeName"`
	Base64   string  `json:"base64"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
}

func extractScreenshotExport(data interface{}) (screenshotExport, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return screenshotExport{}, err
	}
	var wrapper struct {
		Exports []screenshotExport `json:"exports"`
	}
	if err := json.Unmarshal(b, &wrapper); err != nil {
		return screenshotExport{}, err
	}
	if len(wrapper.Exports) == 0 {
		return screenshotExport{}, errors.New("no screenshot export returned by plugin")
	}
	return wrapper.Exports[0], nil
}

func writeBase64(b64, outputPath string) (int, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return 0, fmt.Errorf("base64 decode: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return 0, fmt.Errorf("mkdir: %w", err)
	}
	f, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return 0, fmt.Errorf("file already exists at outputPath: %s", outputPath)
		}
		return 0, err
	}
	defer f.Close()
	n, err := f.Write(data)
	return n, err
}

func resolveOutputPath(outputPath, workDir string) (string, error) {
	resolved := filepath.Join(workDir, outputPath)
	rel, err := filepath.Rel(workDir, resolved)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("outputPath must be inside the working directory: %s", workDir)
	}
	return resolved, nil
}

func inferFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "PNG"
	case ".svg":
		return "SVG"
	case ".jpg", ".jpeg":
		return "JPG"
	case ".pdf":
		return "PDF"
	}
	return ""
}

func parseSaveItem(raw interface{}) (saveItem, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return saveItem{}, err
	}
	var item saveItem
	if err := json.Unmarshal(b, &item); err != nil {
		return saveItem{}, err
	}
	return item, nil
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
