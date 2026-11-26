package wire

import (
	"context"
	"errors"
	"testing"

	"github.com/go-agentsync/agentscope/model"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

var errBoom = errors.New("boom")

// fakeClientStream fails the step it is told to; the others succeed.
type fakeClientStream struct {
	send, closeSend, recv error
}

func (f *fakeClientStream) Header() (metadata.MD, error) { return nil, nil }
func (f *fakeClientStream) Trailer() metadata.MD         { return nil }
func (f *fakeClientStream) CloseSend() error             { return f.closeSend }
func (f *fakeClientStream) Context() context.Context     { return context.Background() }
func (f *fakeClientStream) SendMsg(any) error            { return f.send }
func (f *fakeClientStream) RecvMsg(any) error            { return f.recv }

type fakeConn struct{ st *fakeClientStream }

func (f fakeConn) Invoke(context.Context, string, any, any, ...grpc.CallOption) error {
	return nil
}
func (f fakeConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return f.st, nil
}
func (f fakeConn) Close() error { return nil }

// Watch reports the first step of the stream that fails, and a stream that
// breaks with the context still live is an error, not a clean end.
func TestWatchReportsEachFailingStepOfTheStream(t *testing.T) {
	for name, st := range map[string]*fakeClientStream{
		"send":      {send: errBoom},
		"closeSend": {closeSend: errBoom},
		"recv":      {recv: errBoom},
	} {
		c := &Client{cc: fakeConn{st}}
		if err := c.Watch(context.Background(), func(model.Snapshot) { t.Error("a snapshot was delivered") }); !errors.Is(err, errBoom) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

type registrar struct{ desc *grpc.ServiceDesc }

func (r *registrar) RegisterService(d *grpc.ServiceDesc, _ any) { r.desc = d }

type failingServerStream struct{ grpc.ServerStream }

func (failingServerStream) RecvMsg(any) error { return errBoom }

// The handlers refuse a request they cannot decode, and run through an
// interceptor when the server has one.
func TestHandlersRefuseUndecodableRequestsAndHonourInterceptors(t *testing.T) {
	r := &registrar{}
	sc := fixed{snaps: []model.Snapshot{{Version: model.Version}}}
	Register(r, sc)
	bad := func(any) error { return errBoom }
	good := func(any) error { return nil }
	passThrough := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		return h(ctx, req)
	}
	var names []string
	for _, m := range r.desc.Methods {
		names = append(names, m.MethodName)
		if _, err := m.Handler(sc, context.Background(), bad, nil); !errors.Is(err, errBoom) {
			t.Errorf("%s: an undecodable request gave %v", m.MethodName, err)
		}
		if out, err := m.Handler(sc, context.Background(), good, passThrough); err != nil || out == nil {
			t.Errorf("%s through an interceptor: %v %v", m.MethodName, out, err)
		}
	}
	if len(names) != 3 {
		t.Fatalf("methods %v", names)
	}
	if err := r.desc.Streams[0].Handler(sc, failingServerStream{}); !errors.Is(err, errBoom) {
		t.Errorf("a Watch whose request cannot be read: %v", err)
	}
}
