package databases

import (
	"strings"
	"testing"
)

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
	if strings.ContainsAny(first, "'" 
	") {
		t.Fatal("generated password contains unsafe delimiter characters")
	}
}
