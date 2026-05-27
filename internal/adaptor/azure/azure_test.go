package azure

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kingford/TopoLLM/internal/adaptor"
)

func TestSetupRequest_AzureURLAndHeader(t *testing.T) {
	in := &adaptor.Request{Model: "gpt-4o", Body: []byte(`{"model":"gpt-4o","messages":[]}`)}
	ch := &adaptor.Channel{BaseURL: "https://my.openai.azure.com", APIKey: "k", Extra: map[string]string{"api_version": "2024-10-21"}}

	req, err := (&Adaptor{}).SetupRequest(context.Background(), in, ch)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://my.openai.azure.com/openai/deployments/gpt-4o/chat/completions?api-version=2024-10-21"
	if req.URL.String() != want {
		t.Errorf("url = %s, want %s", req.URL.String(), want)
	}
	if req.Header.Get("api-key") != "k" {
		t.Error("missing api-key header")
	}
}

func TestSetupRequest_DeploymentOverride(t *testing.T) {
	in := &adaptor.Request{Model: "gpt-4o", Body: []byte(`{}`)}
	ch := &adaptor.Channel{BaseURL: "https://x.openai.azure.com", APIKey: "k", Extra: map[string]string{"deployment": "my-deploy"}}
	req, _ := (&Adaptor{}).SetupRequest(context.Background(), in, ch)
	if !strings.Contains(req.URL.Path, "/deployments/my-deploy/") {
		t.Errorf("deployment override not applied: %s", req.URL.Path)
	}
}

func TestSetupEmbeddings(t *testing.T) {
	in := &adaptor.Request{Model: "text-embed", Body: []byte(`{}`)}
	ch := &adaptor.Channel{BaseURL: "https://x.openai.azure.com", APIKey: "k"}
	req, err := (&Adaptor{}).SetupEmbeddings(context.Background(), in, ch)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.URL.Path, "/embeddings") {
		t.Errorf("embeddings path missing: %s", req.URL.Path)
	}
}

func TestRelayResponse_Passthrough(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"object":"chat.completion","usage":{"total_tokens":5}}`)),
		Header:     make(http.Header),
	}
	rec := httptest.NewRecorder()
	usage, err := (&Adaptor{}).RelayResponse(rec, resp, false)
	if err != nil {
		t.Fatal(err)
	}
	if usage == nil || usage.TotalTokens != 5 {
		t.Errorf("usage = %+v, want total 5", usage)
	}
	if !strings.Contains(rec.Body.String(), "chat.completion") {
		t.Errorf("body = %s", rec.Body.String())
	}
}
