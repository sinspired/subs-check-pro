package platform

import (
	"net/http"
	"testing"
)

func TestCheckGemini_Success(t *testing.T) {
	// 使用默认 http.Client
	client := &http.Client{}

	ok, err := CheckGemini(client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Logf("Gemini结果: %v", ok)

	if !ok {
		t.Errorf("expected true, got false")
	}
}

func TestCheckCopilot_Success(t *testing.T) {
	// 使用默认 http.Client
	client := &http.Client{}

	Copilot, CopilotAPI := CheckCopilot(client)

	t.Logf("Copilot API: %v", CopilotAPI)

	if !Copilot {
		t.Errorf("expected true, got false")
	}
}
