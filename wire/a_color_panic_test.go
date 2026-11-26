package wire

import (
	"context"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

// With no interceptor on the server, a handler runs the call itself. Taking that
// branch away would call a nil interceptor; the panic is recovered and reported.
func TestHandlersRunWithoutAnInterceptor(t *testing.T) {
	r := &registrar{}
	sc := fixed{snaps: []model.Snapshot{{Version: model.Version}}}
	Register(r, sc)
	for _, m := range r.desc.Methods {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("%s: a nil interceptor was called: %v", m.MethodName, p)
				}
			}()
			if out, err := m.Handler(sc, context.Background(), func(any) error { return nil }, nil); err != nil || out == nil {
				t.Errorf("%s without an interceptor: %v %v", m.MethodName, out, err)
			}
		}()
	}
}

// A hub with no Colorer refuses; deleting the refusal would call a nil interface.
// That must fail on an assertion, so the panic is recovered and reported.
func TestHubSetColorWithoutAColorerRefusesWithoutPanicking(t *testing.T) {
	h := NewHub(&src{}, time.Hour, time.Hour)
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("a nil Colorer was called: %v", r)
			}
		}()
		err = h.SetColor(context.Background(), "k", "#112233")
	}()
	if err == nil {
		t.Error("a hub with no Colorer coloured an account")
	}
}
