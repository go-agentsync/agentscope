package wire

import (
	"context"
	"errors"
	"io"

	"github.com/go-agentsync/agentscope/model"
	wstransport "github.com/grpc-transports/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is the client side of the service over a WebSocket. It compiles and
// runs the same natively and under GOOS=js GOARCH=wasm.
type Client struct{ cc *grpc.ClientConn }

// Dial connects to the service at wsURL (ws:// or wss://). The connection is
// lazy: the first call opens it.
func Dial(wsURL string) (*Client, error) {
	opt, err := wstransport.DialOption(wsURL, wstransport.ClientConfig{})
	if err != nil {
		return nil, err
	}
	opts := append(DialOptions(), grpc.WithTransportCredentials(insecure.NewCredentials()), opt)
	cc, err := grpc.NewClient("passthrough:///agentscope", opts...)
	if err != nil {
		return nil, err
	}
	return &Client{cc: cc}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.cc.Close() }

// Snapshot fetches one snapshot.
func (c *Client) Snapshot(ctx context.Context) (model.Snapshot, error) {
	var out model.Snapshot
	err := c.cc.Invoke(ctx, methodSnapshot, &Empty{}, &out)
	return out, err
}

// Watch calls fn with every snapshot the server streams, until the stream or
// ctx ends. It returns nil when ctx was cancelled.
func (c *Client) Watch(ctx context.Context, fn func(model.Snapshot)) error {
	st, err := c.cc.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, methodWatch)
	if err != nil {
		return err
	}
	if err := st.SendMsg(&Empty{}); err != nil {
		return err
	}
	if err := st.CloseSend(); err != nil {
		return err
	}
	for {
		var s model.Snapshot
		if err := st.RecvMsg(&s); err != nil {
			if ctx.Err() != nil || errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if s.Version > model.Version {
			return errors.New("wire: the server speaks a newer snapshot version; update the UI")
		}
		fn(s)
	}
}
