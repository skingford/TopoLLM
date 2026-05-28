package cache

import (
	"context"
	"testing"
	"time"

	"github.com/kingford/TopoLLM/internal/config"
)

func TestMemBackend_GetSet(t *testing.T) {
	b := newMemBackend()
	b.Set(context.Background(), "k", []byte("v"), time.Minute)
	got, ok := b.Get(context.Background(), "k")
	if !ok || string(got) != "v" {
		t.Errorf("get = %q ok=%v, want v true", got, ok)
	}
}

func TestMemBackend_Expiry(t *testing.T) {
	b := newMemBackend()
	b.Set(context.Background(), "k", []byte("v"), 1*time.Nanosecond)
	time.Sleep(2 * time.Millisecond)
	if _, ok := b.Get(context.Background(), "k"); ok {
		t.Error("expired entry should be invisible")
	}
}

func TestService_Disabled(t *testing.T) {
	s := New(config.SemanticCacheConfig{Enabled: false}, nil)
	if s.Enabled() {
		t.Error("disabled service should not be enabled")
	}
	if _, ok := s.Get(context.Background(), "k"); ok {
		t.Error("disabled Get should miss")
	}
}

func TestKeyForChat_NormalizationMatches(t *testing.T) {
	s := New(config.SemanticCacheConfig{Enabled: true, TTLSeconds: 60}, nil)
	k1, ok1 := s.KeyForChat([]byte(`{"model":"m","messages":[{"role":"user","content":"Hello   World"}]}`))
	k2, ok2 := s.KeyForChat([]byte(`{"model":"m","messages":[{"role":"User","content":"hello world"}]}`))
	if !ok1 || !ok2 {
		t.Fatal("KeyForChat should succeed for both")
	}
	if k1 != k2 {
		t.Errorf("normalized keys should match: %q vs %q", k1, k2)
	}
}

func TestKeyForChat_DifferentContentDiffers(t *testing.T) {
	s := New(config.SemanticCacheConfig{Enabled: true, TTLSeconds: 60}, nil)
	k1, _ := s.KeyForChat([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	k2, _ := s.KeyForChat([]byte(`{"model":"m","messages":[{"role":"user","content":"bye"}]}`))
	if k1 == k2 {
		t.Error("different content should produce different keys")
	}
}

func TestKeyForChat_StreamReturnsFalse(t *testing.T) {
	s := New(config.SemanticCacheConfig{Enabled: true, TTLSeconds: 60}, nil)
	if _, ok := s.KeyForChat([]byte(`{"model":"m","stream":true,"messages":[]}`)); ok {
		t.Error("streaming request should not produce a cache key")
	}
}

func TestService_GetSetRoundtrip(t *testing.T) {
	s := New(config.SemanticCacheConfig{Enabled: true, TTLSeconds: 60}, nil)
	ctx := context.Background()
	s.Set(ctx, "k", []byte("response"))
	got, ok := s.Get(ctx, "k")
	if !ok || string(got) != "response" {
		t.Errorf("get = %q ok=%v", got, ok)
	}
}
