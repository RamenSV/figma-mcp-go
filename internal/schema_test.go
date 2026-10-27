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
