package auth

import "testing"

func TestGenerateAndParseToken(t *testing.T) {
	service := NewService("test-secret")

	token, err := service.Generate(42)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	userID, err := service.Parse(token)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if userID != 42 {
		t.Fatalf("expected userID 42, got %d", userID)
	}
}
