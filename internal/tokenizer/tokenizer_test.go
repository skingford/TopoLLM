package tokenizer

import "testing"

func TestCountText_NonZero(t *testing.T) {
	if CountText("hello world", "gpt-4") == 0 {
		t.Error("expected non-zero token count for non-empty text")
	}
}

func TestCountText_Empty(t *testing.T) {
	if CountText("", "gpt-4") != 0 {
		t.Error("empty text should be 0 tokens")
	}
}

func TestCountChatMessages(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"Hello, world!"}]}`)
	n := CountChatMessages(body, "gpt-4")
	if n < 6 || n > 30 {
		t.Errorf("unexpected token count %d for 'Hello, world!'", n)
	}
}

func TestCountChatMessages_Multiple(t *testing.T) {
	multi := CountChatMessages([]byte(`{"messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hi"}]}`), "gpt-4")
	single := CountChatMessages([]byte(`{"messages":[{"role":"user","content":"hi"}]}`), "gpt-4")
	if multi <= single {
		t.Errorf("multi-message count (%d) should exceed single (%d)", multi, single)
	}
}

func TestCountChatMessages_FallbackOnGarbage(t *testing.T) {
	if got := CountChatMessages([]byte("not json"), "gpt-4"); got != len("not json")/4 {
		t.Errorf("garbage body should fall back to bytes/4, got %d", got)
	}
}

func TestCountEmbeddingsInput_String(t *testing.T) {
	if CountEmbeddingsInput([]byte(`{"input":"hello world"}`), "text-embedding-3") == 0 {
		t.Error("string input should yield non-zero")
	}
}

func TestCountEmbeddingsInput_Array(t *testing.T) {
	n := CountEmbeddingsInput([]byte(`{"input":["a","b","c"]}`), "text-embedding-3")
	if n < 3 {
		t.Errorf("array input should yield >= 3 tokens, got %d", n)
	}
}

func TestEncodingFor(t *testing.T) {
	cases := map[string]string{
		"gpt-4o":         "o200k_base",
		"gpt-4o-mini":    "o200k_base",
		"o1-preview":     "o200k_base",
		"gpt-5":          "o200k_base",
		"gpt-4-turbo":    "cl100k_base",
		"claude-sonnet":  "cl100k_base",
		"gemini-2.5-pro": "cl100k_base",
	}
	for in, want := range cases {
		if got := encodingFor(in); got != want {
			t.Errorf("encodingFor(%q) = %q, want %q", in, got, want)
		}
	}
}
