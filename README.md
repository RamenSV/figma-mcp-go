# figma-mcp-go

> A **zero-cost, unlimited** Figma MCP server — no API key, no rate limits, just a single Go binary.

Works with Cursor, Claude, GitHub Copilot, and any MCP-compatible AI tool.

---

## Why this exists

Most Figma MCP servers rely on the **Figma REST API**.

That sounds fine… until you hit this:

| Plan | Limit |
|------|-------|
| Starter / View / Collab | **6 tool calls/month** |
| Pro / Org (Dev seat) | 200 tool calls/day |
| Enterprise | 600 tool calls/day |

If you're experimenting with AI tools, you’ll burn through that in minutes.

I didn’t have enough money to pay for higher limits.
So I built something that **doesn’t use the API at all**.

## Setup

Watch the video:

[![Watch the video](https://img.youtube.com/vi/DjqyU0GKv9k/sddefault.jpg)](https://youtu.be/DjqyU0GKv9k)

### 1. Configure your AI tool

.vscode/mcp.json
```json
{
	"servers": {
		"figma-mcp-go": {
			"type": "stdio",
			"command": "npx",
			"args": [
				"-y",
				"@vkhanhqui/figma-mcp-go"
			]
		}
	}
}
```

.mcp.json
```json
{
  "mcpServers": {
    "figma-mcp-go": {
      "command": "npx",
      "args": ["-y", "@vkhanhqui/figma-mcp-go"]
    }
  }
}
```

### 2. Install the Figma plugin

1. In Figma Desktop: **Plugins → Development → Import plugin from manifest**
2. Select `manifest.json` in the [plugin.zip](https://github.com/vkhanhqui/figma-mcp-go/releases)
3. Run the plugin inside any Figma file

---

## Available tools

### Document & Selection

| Tool | Description |
|------|-------------|
| `get_document` | Full current page tree |
| `get_metadata` | File name, pages, current page |
| `get_selection` | Currently selected nodes |
| `get_node` | Single node by ID |
| `get_nodes_info` | Multiple nodes by ID |
| `get_design_context` | Depth-limited tree — token-efficient for large files |
| `scan_text_nodes` | All text nodes in a subtree |
| `scan_nodes_by_types` | Nodes matching given type list |

### Styles & Variables

| Tool | Description |
|------|-------------|
| `get_styles` | Paint, text, effect, and grid styles |
| `get_variable_defs` | Variable collections and values |
| `get_local_components` | All components in the file |
| `get_annotations` | Dev-mode annotations |

### Export

| Tool | Description |
|------|-------------|
| `get_screenshot` | Base64 image export of any node |
| `save_screenshots` | Export images to disk (server-side, no API call) |

### Write

Run the plugin in Design mode (Dev Mode is read-only). Every write is one undo step — Cmd+Z reverts it.

| Tool | Description |
|------|-------------|
| `create_nodes` | Create frames, rectangles, ellipses, lines, text, components — nested trees in one call |
| `update_nodes` | Change position, size, fills, strokes, text, fonts, auto layout; move nodes between parents |
| `delete_nodes` | Delete nodes by ID |
| `clone_node` | Duplicate a node, optionally into a new parent |
| `create_instance` | Instance a local or library component |
| `set_selection` | Select nodes and scroll the viewport to them |

### Motion (beta)

Uses Figma's beta Motion plugin API, so it needs a Figma version with Motion and the plugin running in Design mode. Keyframes go on the timeline of the node's top-level frame; times are in seconds.

| Tool | Description |
|------|-------------|
| `animate_nodes` | Apply keyframe tracks (opacity, translation, scale, rotation, size, fills, effects…) with easing or springs, apply or remove Motion animation styles, set timeline duration |
| `get_animations` | Read timelines, applied styles, keyframe tracks and the animation styles available in the file |

### Large results

Results over 50KB are split so they fit client limits (Claude Desktop rejects results over 1MB; Claude Code saves results over ~25k tokens to a file instead of showing them). The split is lossless and follows the node tree: each chunk holds complete nodes as readable JSON, and large parents arrive first with `id` / `name` / `type` stubs for their children, which later chunks fill in. The first response includes `resultId`, `totalChunks` and `howToRead`; fetch the rest with `get_result_chunk`. Override the chunk size with `FIGMA_MCP_MAX_RESULT_BYTES`. For big pages, `depth` on `get_node` / `get_nodes_info` trims the tree before falling back to chunks.

---

## Contributing

Issues and PRs are welcome.

## References

- [magic-spells/figma-mcp-bridge](https://github.com/magic-spells/figma-mcp-bridge)
- [grab/cursor-talk-to-figma-mcp](https://github.com/grab/cursor-talk-to-figma-mcp)
- [gethopp/figma-mcp-bridge](https://github.com/gethopp/figma-mcp-bridge)
