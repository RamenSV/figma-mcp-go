package internal

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// reassemble rebuilds a result from chunks by following the howToRead rules.
func reassemble(t *testing.T, chunks [][]chunkItem) interface{} {
	t.Helper()
	var root interface{}
	for _, chunk := range chunks {
		for _, item := range chunk {
			// Round-trip through JSON so values match what a client would see.
			var value interface{}
			b, _ := encodeJSON(item.Value)
			if err := json.Unmarshal(b, &value); err != nil {
				t.Fatalf("unmarshal item: %v", err)
			}
			if item.Part > 1 {
				prev, _ := getPointer(root, item.Path).(string)
				value = prev + value.(string)
			}
			root = setPointer(t, root, item.Path, value)
		}
	}
	return root
}

func pointerTokens(path string) []string {
	if path == "" {
		return nil
	}
	tokens := strings.Split(path[1:], "/")
	for i, tok := range tokens {
		tokens[i] = strings.NewReplacer("~1", "/", "~0", "~").Replace(tok)
	}
	return tokens
}

func getPointer(root interface{}, path string) interface{} {
	cur := root
	for _, tok := range pointerTokens(path) {
		switch c := cur.(type) {
		case map[string]interface{}:
			cur = c[tok]
		case []interface{}:
			i, _ := strconv.Atoi(tok)
			cur = c[i]
		}
	}
	return cur
}

func setPointer(t *testing.T, root interface{}, path string, value interface{}) interface{} {
	t.Helper()
	tokens := pointerTokens(path)
	if len(tokens) == 0 {
		return value
	}
	cur := root
	for i, tok := range tokens {
		last := i == len(tokens)-1
		switch c := cur.(type) {
		case map[string]interface{}:
			if last {
				c[tok] = value
			} else {
				cur = c[tok]
			}
		case []interface{}:
			idx, err := strconv.Atoi(tok)
			if err != nil || idx >= len(c) {
				t.Fatalf("bad array index %q in %s", tok, path)
			}
			if last {
				c[idx] = value
			} else {
				cur = c[idx]
			}
		default:
			t.Fatalf("path %s walks into a non-container at %q", path, tok)
		}
	}
	return root
}

func makeTree(depth, width int, prefix string) map[string]interface{} {
	node := map[string]interface{}{
		"id":     prefix,
		"name":   "Frame " + prefix + " <ü/~ & é>",
		"type":   "FRAME",
		"bounds": map[string]interface{}{"x": 1.5, "y": 2.0, "width": 320.0, "height": 200.0},
		"styles": map[string]interface{}{
			"fills":    []interface{}{map[string]interface{}{"color": "#ffffff", "opacity": 1.0, "type": "SOLID"}},
			"path/key": "a~b",
		},
	}
	if depth > 0 {
		children := make([]interface{}, width)
		for i := range children {
			children[i] = makeTree(depth-1, width, fmt.Sprintf("%s:%d", prefix, i))
		}
		node["children"] = children
	}
	return node
}

func roundTrip(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := encodeJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSplitResultIsLossless(t *testing.T) {
	huge := strings.Repeat("M 0 0 L 10 10 \"q\" ✓ ", 20_000)
	cases := map[string]interface{}{
		"deep tree":     makeTree(5, 4, "1"),
		"wide array":    []interface{}{makeTree(2, 30, "2"), makeTree(1, 200, "3")},
		"huge string":   map[string]interface{}{"id": "4:1", "type": "VECTOR", "vectorPath": huge},
		"wrapped lists": map[string]interface{}{"created": []interface{}{makeTree(4, 5, "5")}},
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			const max = 20_000
			want := roundTrip(t, data)
			chunks := splitResult(want, max)
			if len(chunks) < 2 {
				t.Fatalf("expected multiple chunks, got %d", len(chunks))
			}
			for i, chunk := range chunks {
				if n := encodedLen(chunk); n > max {
					t.Errorf("chunk %d is %d bytes, over %d", i, n, max)
				}
			}
			got := reassemble(t, chunks)
			if !reflect.DeepEqual(got, want) {
				t.Fatal("reassembled result differs from original")
			}
		})
	}
}

func TestChunkStubsKeepOutline(t *testing.T) {
	chunks := splitResult(roundTrip(t, makeTree(3, 6, "1")), 10_000)
	root, ok := chunks[0][0].Value.(map[string]interface{})
	if !ok {
		t.Fatalf("first item should be the root shell, got %T", chunks[0][0].Value)
	}
	children, _ := root["children"].([]interface{})
	if len(children) != 6 {
		t.Fatalf("root shell should keep all 6 child stubs, got %d", len(children))
	}
	stub := children[0].(map[string]interface{})
	if stub["$chunked"] != true || stub["id"] != "1:0" || stub["type"] != "FRAME" {
		t.Fatalf("stub missing outline fields: %v", stub)
	}
	if root["name"] == nil || root["bounds"] == nil {
		t.Fatal("root shell should keep its own small fields")
	}
}
