package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

// Clients cap tool result size: Claude Desktop rejects results over 1MB, and
// Claude Code spills results over ~25k tokens to a file instead of showing
// them. Results larger than maxResultBytes are split into chunks served via
// get_result_chunk. Override with FIGMA_MCP_MAX_RESULT_BYTES.
const defaultMaxResultBytes = 50_000

// maxStoredResults caps how many chunked results are kept in memory.
const maxStoredResults = 10

// chunkHeaderBytes is reserved in each chunk for the envelope fields.
const chunkHeaderBytes = 1_000

// chunkReadme is included in chunk 0 so the reader knows how to reassemble.
const chunkReadme = `Lossless split along the node tree. Each item places "value" at JSON pointer "path" from the result root; "parentId" is the id of the enclosing node. Objects marked "$chunked": true are stubs, replaced by later items at the same path. Strings too large for one chunk arrive as numbered "part"s of "parts" — concatenate them in order.`

// chunkItem is one self-contained piece of a split result.
type chunkItem struct {
	Path     string      `json:"path"`
	ParentID string      `json:"parentId,omitempty"`
	Part     int         `json:"part,omitempty"`
	Parts    int         `json:"parts,omitempty"`
	Value    interface{} `json:"value"`
}

var chunkStore = struct {
	sync.Mutex
	results map[string][][]chunkItem
	order   []string
}{results: map[string][][]chunkItem{}}

func maxResultBytes() int {
	if v, err := strconv.Atoi(os.Getenv("FIGMA_MCP_MAX_RESULT_BYTES")); err == nil && v > 0 {
		return v
	}
	return defaultMaxResultBytes
}

// storeChunked splits data along its tree structure, stores the chunks, and
// returns chunk 0.
func storeChunked(data interface{}) (*mcp.CallToolResult, error) {
	chunks := splitResult(data, maxResultBytes())

	id := uuid.NewString()
	chunkStore.Lock()
	chunkStore.results[id] = chunks
	chunkStore.order = append(chunkStore.order, id)
	if len(chunkStore.order) > maxStoredResults {
		delete(chunkStore.results, chunkStore.order[0])
		chunkStore.order = chunkStore.order[1:]
	}
	chunkStore.Unlock()

	return chunkResult(id, 0)
}

// chunkResult returns one chunk of a stored result.
func chunkResult(id string, index int) (*mcp.CallToolResult, error) {
	chunkStore.Lock()
	chunks, ok := chunkStore.results[id]
	chunkStore.Unlock()
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("unknown or expired resultId: %s", id)), nil
	}
	if index < 0 || index >= len(chunks) {
		return mcp.NewToolResultError(fmt.Sprintf("chunk must be between 0 and %d", len(chunks)-1)), nil
	}

	out := map[string]interface{}{
		"chunked":     true,
		"resultId":    id,
		"chunk":       index,
		"totalChunks": len(chunks),
		"items":       chunks[index],
	}
	if index == 0 {
		out["howToRead"] = chunkReadme
	}
	if index+1 < len(chunks) {
		out["next"] = fmt.Sprintf("call get_result_chunk with resultId=%s chunk=%d", id, index+1)
	}
	text, err := encodeJSON(out)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal chunk: %v", err)), nil
	}
	return mcp.NewToolResultText(string(text)), nil
}

// splitResult breaks data into chunks of items that each fit maxBytes.
func splitResult(data interface{}, maxBytes int) [][]chunkItem {
	budget := maxBytes - chunkHeaderBytes
	if budget < 1_000 {
		budget = 1_000
	}
	s := &splitter{budget: budget}
	s.split(data, "", "")

	var chunks [][]chunkItem
	var cur []chunkItem
	size := 0
	for _, item := range s.items {
		n := encodedLen(item) + 1
		if len(cur) > 0 && size+n > budget {
			chunks = append(chunks, cur)
			cur, size = nil, 0
		}
		cur = append(cur, item)
		size += n
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	return chunks
}

type splitter struct {
	budget int
	items  []chunkItem
}

// split emits v as one item when it fits, otherwise emits a shell with stubs
// for its largest fields and recurses into them. Parents are emitted before
// children, so chunks read top-down.
func (s *splitter) split(v interface{}, path, parentID string) {
	if encodedLen(v) <= s.budget {
		s.items = append(s.items, chunkItem{Path: path, ParentID: parentID, Value: v})
		return
	}

	switch t := v.(type) {
	case map[string]interface{}:
		id, _ := t["id"].(string)
		childParent := parentID
		if id != "" {
			childParent = id
		}

		// Defer the largest fields until the shell fits the budget.
		keys := make([]string, 0, len(t))
		sizes := make(map[string]int, len(t))
		total := 2
		for k, child := range t {
			keys = append(keys, k)
			sizes[k] = encodedLen(child)
			total += sizes[k] + len(k) + 4
		}
		sort.Slice(keys, func(i, j int) bool { return sizes[keys[i]] > sizes[keys[j]] })

		shell := make(map[string]interface{}, len(t))
		for k, child := range t {
			shell[k] = child
		}
		var deferred []string
		for _, k := range keys {
			if total <= s.budget {
				break
			}
			switch t[k].(type) {
			case map[string]interface{}, []interface{}, string:
				stub := stubFor(t[k])
				shell[k] = stub
				total += encodedLen(stub) - sizes[k]
				deferred = append(deferred, k)
			}
		}
		s.items = append(s.items, chunkItem{Path: path, ParentID: parentID, Value: shell})

		sort.Strings(deferred)
		for _, k := range deferred {
			childPath := path + "/" + escapePointer(k)
			if arr, ok := t[k].([]interface{}); ok {
				for i, el := range arr {
					s.split(el, childPath+"/"+strconv.Itoa(i), childParent)
				}
			} else {
				s.split(t[k], childPath, childParent)
			}
		}

	case []interface{}:
		s.items = append(s.items, chunkItem{Path: path, ParentID: parentID, Value: stubFor(t)})
		for i, el := range t {
			s.split(el, path+"/"+strconv.Itoa(i), parentID)
		}

	case string:
		parts := splitString(t, s.budget-len(path)-len(parentID)-100)
		for i, p := range parts {
			s.items = append(s.items, chunkItem{Path: path, ParentID: parentID, Part: i + 1, Parts: len(parts), Value: p})
		}

	default:
		s.items = append(s.items, chunkItem{Path: path, ParentID: parentID, Value: v})
	}
}

// stubFor returns a small placeholder for a deferred value. Arrays keep their
// length, and node elements keep id/name/type so the outline stays readable.
func stubFor(v interface{}) interface{} {
	arr, ok := v.([]interface{})
	if !ok {
		return map[string]interface{}{"$chunked": true}
	}
	stubs := make([]interface{}, len(arr))
	for i, el := range arr {
		stub := map[string]interface{}{"$chunked": true}
		if m, ok := el.(map[string]interface{}); ok {
			for _, k := range []string{"id", "name", "type"} {
				if val, ok := m[k]; ok {
					stub[k] = val
				}
			}
		}
		stubs[i] = stub
	}
	return stubs
}

// splitString cuts s into pieces whose JSON encoding fits size bytes, never
// splitting a multi-byte UTF-8 character.
func splitString(s string, size int) []string {
	if size < 100 {
		size = 100
	}
	// Leave room for escaping (quotes, backslashes, control characters).
	size /= 2
	var parts []string
	for start := 0; start < len(s); {
		end := start + size
		if end >= len(s) {
			end = len(s)
		} else {
			for end > start && !utf8.RuneStart(s[end]) {
				end--
			}
		}
		parts = append(parts, s[start:end])
		start = end
	}
	return parts
}

// escapePointer escapes a key for use in a JSON pointer (RFC 6901).
func escapePointer(k string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(k)
}

// encodeJSON marshals v without HTML escaping, so <, > and & stay readable.
func encodeJSON(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func encodedLen(v interface{}) int {
	b, err := encodeJSON(v)
	if err != nil {
		return 0
	}
	return len(b)
}
