// Package wire is the gRPC contract between the collector and the browser:
//
//	service agentscope.v1.Scope {
//	  rpc Snapshot(Empty) returns (model.Snapshot);          // unary
//	  rpc Watch(Empty)    returns (stream model.Snapshot);   // server-streaming
//	}
//
// The messages are the JSON form of model.Snapshot, carried by a gRPC codec
// named "json" that both ends force, so there is no protoc step and the schema
// is the Go type. The transport in the browser is a WebSocket
// (github.com/grpc-transports/websocket), whose js/wasm client is the same
// code as the native one: the wasm UI speaks the same gRPC a terminal client
// does.
package wire

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/go-agentsync/agentscope/model"
	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
)

// Service and method names.
const (
	ServiceName    = "agentscope.v1.Scope"
	methodSnapshot = "/" + ServiceName + "/Snapshot"
	methodWatch    = "/" + ServiceName + "/Watch"
)

// MaxMessage bounds one message in either direction.
const MaxMessage = 16 << 20

// Empty is the request of both methods.
type Empty struct{}

// codec is the JSON codec both ends force.
type codec struct{}

// Codec is the codec to force on a server or a client of this service.
var Codec encoding.CodecV2 = codec{}

func (codec) Name() string { return "json" }

func (codec) Marshal(v any) (mem.BufferSlice, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return mem.BufferSlice{mem.SliceBuffer(b)}, nil
}

// Unmarshal decodes exactly one JSON value and refuses trailing data.
func (codec) Unmarshal(data mem.BufferSlice, v any) error {
	b := data.Materialize()
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("wire: trailing data after the JSON value")
	}
	return nil
}

// Scope is the server side of the service.
type Scope interface {
	Snapshot(ctx context.Context) (model.Snapshot, error)
	Watch(ctx context.Context, send func(model.Snapshot) error) error
}

// Register registers s on gs.
func Register(gs grpc.ServiceRegistrar, s Scope) {
	gs.RegisterService(&grpc.ServiceDesc{
		ServiceName: ServiceName,
		HandlerType: (*Scope)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Snapshot",
			Handler: func(srv any, ctx context.Context, dec func(any) error, ic grpc.UnaryServerInterceptor) (any, error) {
				var in Empty
				if err := dec(&in); err != nil {
					return nil, err
				}
				call := func(ctx context.Context, _ any) (any, error) {
					snap, err := srv.(Scope).Snapshot(ctx)
					return &snap, err
				}
				if ic == nil {
					return call(ctx, &in)
				}
				return ic(ctx, &in, &grpc.UnaryServerInfo{Server: srv, FullMethod: methodSnapshot}, call)
			},
		}},
		Streams: []grpc.StreamDesc{{
			StreamName:    "Watch",
			ServerStreams: true,
			Handler: func(srv any, stream grpc.ServerStream) error {
				var in Empty
				if err := stream.RecvMsg(&in); err != nil {
					return err
				}
				return srv.(Scope).Watch(stream.Context(), func(s model.Snapshot) error { return stream.SendMsg(&s) })
			},
		}},
	}, s)
}

// ServerOptions are the grpc.ServerOption a server of this service needs.
func ServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.ForceServerCodecV2(Codec),
		grpc.MaxRecvMsgSize(1 << 20), // requests are empty
		grpc.MaxSendMsgSize(MaxMessage),
	}
}

// DialOptions are the grpc.DialOption a client of this service needs, besides
// the transport.
func DialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.ForceCodecV2(Codec), grpc.MaxCallRecvMsgSize(MaxMessage)),
	}
}
