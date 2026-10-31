package internal

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestInlineImagePaths(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "photo.png")
	content := []byte("\x89PNG fake bytes")
	if err := os.WriteFile(png, content, 0o644); err != nil {
		t.Fatal(err)
	}

	args := map[string]interface{}{
		"updates": []interface{}{
			map[string]interface{}{
				"nodeId": "1:2",
				"fills":  map[string]interface{}{"imagePath": png, "scaleMode": "FIT"},
			},
			map[string]interface{}{
				"nodeId": "1:3",
				"fills":  []interface{}{"#FFFFFF", map[string]interface{}{"imagePath": png}},
			},
		},
	}
	if err := inlineImagePaths(args); err != nil {
		t.Fatalf("inlineImagePaths: %v", err)
	}

	want := base64.StdEncoding.EncodeToString(content)
	updates := args["updates"].([]interface{})
	first := updates[0].(map[string]interface{})["fills"].(map[string]interface{})
	if first["imageBase64"] != want || first["imagePath"] != nil || first["scaleMode"] != "FIT" {
		t.Fatalf("single paint not inlined: %#v", first)
	}
	second := updates[1].(map[string]interface{})["fills"].([]interface{})[1].(map[string]interface{})
	if second["imageBase64"] != want || second["imagePath"] != nil {
		t.Fatalf("paint in array not inlined: %#v", second)
	}
}

func TestInlineImagePathsRejectsBadPaths(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txt, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"relative":  "photo.png",
		"extension": txt,
		"missing":   filepath.Join(dir, "missing.png"),
	} {
		args := map[string]interface{}{"fills": map[string]interface{}{"imagePath": path}}
		if err := inlineImagePaths(args); err == nil {
			t.Errorf("%s: expected error for %s", name, path)
		}
	}
}
