package internal

import "testing"

func TestValidNodeID(t *testing.T) {
	for _, id := range []string{"4029:12345", "0:1", "I60:3637;59:1069", "I1:2;3:4;5:6"} {
		if !ValidNodeID(id) {
			t.Errorf("ValidNodeID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"", "4029-12345", "I60:3637", "60:3637;59:1069", "I60:3637;", "abc"} {
		if ValidNodeID(id) {
			t.Errorf("ValidNodeID(%q) = true, want false", id)
		}
	}
}

func TestValidateAnimateNodes(t *testing.T) {
	track := map[string]interface{}{"nodeId": "1:2", "property": "OPACITY"}
	cases := []struct {
		name   string
		params map[string]interface{}
		ok     bool
	}{
		{"track", map[string]interface{}{"tracks": []interface{}{track}}, true},
		{"style", map[string]interface{}{"styles": []interface{}{map[string]interface{}{"nodeId": "1:2", "style": "Fade in"}}}, true},
		{"empty", map[string]interface{}{}, false},
		{"bad id", map[string]interface{}{"tracks": []interface{}{map[string]interface{}{"nodeId": "1-2"}}}, false},
		{"zero duration", map[string]interface{}{"tracks": []interface{}{track}, "duration": 0.0}, false},
	}
	for _, c := range cases {
		if got := ValidateRPC("animate_nodes", nil, c.params) == ""; got != c.ok {
			t.Errorf("%s: valid = %v, want %v", c.name, got, c.ok)
		}
	}
}
