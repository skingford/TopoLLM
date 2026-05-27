package adaptor

import (
	"context"
	"net/http"
	"testing"

	"github.com/kingford/TopoLLM/internal/relay"
)

type stubAdaptor struct{}

func (stubAdaptor) Name() string               { return "stub" }
func (stubAdaptor) Capabilities() Capabilities { return Capabilities{Chat: true} }

func (stubAdaptor) ConvertRequest(context.Context, *relay.ChatRequest, *Channel) (*http.Request, error) {
	return nil, nil
}

func (stubAdaptor) ParseResponse(context.Context, *http.Response) (*relay.ChatResponse, *relay.Usage, error) {
	return nil, nil, nil
}

func TestRegisterAndGet(t *testing.T) {
	Register("stub", func() Adaptor { return stubAdaptor{} })

	got, ok := Get("stub")
	if !ok {
		t.Fatal("Get(stub) not found after Register")
	}
	if got.Name() != "stub" {
		t.Errorf("Name() = %q, want stub", got.Name())
	}
}

func TestGet_Unknown(t *testing.T) {
	if _, ok := Get("does-not-exist"); ok {
		t.Error("Get(unknown) returned ok=true, want false")
	}
}
