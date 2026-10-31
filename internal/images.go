package internal

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxImageFileBytes caps local images read for imagePath fills. Figma rejects
// images above 4096px per side, which keeps real-world files well under this.
const maxImageFileBytes = 20 * 1024 * 1024

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true}

// inlineImagePaths walks tool arguments and replaces every {imagePath: "..."}
// paint with {imageBase64: "..."} read from the local file, so callers can
// fill nodes with local images without pasting base64 into the request. The
// plugin only ever sees imageBase64.
func inlineImagePaths(v interface{}) error {
	switch val := v.(type) {
	case map[string]interface{}:
		if raw, ok := val["imagePath"]; ok {
			path, ok := raw.(string)
			if !ok || path == "" {
				return fmt.Errorf("imagePath must be a non-empty string")
			}
			encoded, err := readImageBase64(path)
			if err != nil {
				return err
			}
			delete(val, "imagePath")
			val["imageBase64"] = encoded
		}
		for _, child := range val {
			if err := inlineImagePaths(child); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, child := range val {
			if err := inlineImagePaths(child); err != nil {
				return err
			}
		}
	}
	return nil
}

// readImageBase64 loads a PNG/JPG/GIF from an absolute or ~-prefixed path.
func readImageBase64(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("imagePath %s: %v", path, err)
		}
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("imagePath must be absolute or start with ~/: %s", path)
	}
	if !imageExts[strings.ToLower(filepath.Ext(path))] {
		return "", fmt.Errorf("imagePath must be a .png, .jpg, .jpeg or .gif file: %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("imagePath %s: %v", path, err)
	}
	if info.Size() > maxImageFileBytes {
		return "", fmt.Errorf("imagePath %s is %d bytes; max is %d", path, info.Size(), maxImageFileBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("imagePath %s: %v", path, err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
