package databases

import "testing"

func TestGeneratePassword(t *testing.T) {
	first, err := GeneratePassword()
	if err != nil {
		t.Fatal(err)
	}
	second, err := GeneratePassword()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 40 {
		t.Fatalf("generated password is too short: %d", len(first))
	}
	if first == second {
		t.Fatal("generated passwords must be unique")
	}
	for _, r := range first {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !valid {
			t.Fatalf("generated password contains character outside base64url alphabet: %q", r)
		}
	}
}
