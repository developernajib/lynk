package edge

import (
	"context"
	"errors"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// director picks the upstream connection for a full method name and returns
// the (filtered) outgoing context to use for the proxied call.
type director func(ctx context.Context, fullMethod string) (context.Context, *grpc.ClientConn, error)

// transparentHandler forwards any RPC, unary or streaming, byte-for-byte to
// the upstream the director picks. It is installed as the proxy server's
// UnknownServiceHandler, which receives every call since the proxy registers
// no real services. Both directions pump concurrently until the upstream
// completes (its status and trailers propagate to the client) or the client
// connection breaks (the upstream call is cancelled).
func transparentHandler(d director) grpc.StreamHandler {
	// Every RPC shape is modeled as bidirectional here: a unary call is just
	// a stream that happens to carry one message each way.
	desc := &grpc.StreamDesc{ServerStreams: true, ClientStreams: true}

	return func(_ any, serverStream grpc.ServerStream) error {
		fullMethod, ok := grpc.MethodFromServerStream(serverStream)
		if !ok {
			return status.Error(codes.Internal, "no method in proxy stream")
		}

		outCtx, conn, err := d(serverStream.Context(), fullMethod)
		if err != nil {
			return err
		}

		clientCtx, cancel := context.WithCancel(outCtx)
		defer cancel()

		clientStream, err := conn.NewStream(clientCtx, desc, fullMethod, grpc.ForceCodec(rawCodec{}))
		if err != nil {
			return status.Errorf(codes.Unavailable, "proxy dial %s: %v", fullMethod, err)
		}

		clientToUpstream := make(chan error, 1)
		upstreamToClient := make(chan error, 1)
		go func() { clientToUpstream <- pumpClientToUpstream(serverStream, clientStream) }()
		go func() { upstreamToClient <- pumpUpstreamToClient(clientStream, serverStream) }()

		for range 2 {
			select {
			case err := <-clientToUpstream:
				if errors.Is(err, io.EOF) {
					// The client finished sending: half-close the upstream so
					// it knows no more requests are coming, then keep waiting
					// for its responses.
					_ = clientStream.CloseSend()
					continue
				}
				// The client connection broke mid-stream: cancel the upstream
				// call rather than leaving it running for nobody.
				cancel()
				return status.Errorf(codes.Internal, "proxy client->upstream: %v", err)
			case err := <-upstreamToClient:
				// The upstream finished (cleanly or not). Its trailers and
				// status are the call's outcome either way.
				serverStream.SetTrailer(clientStream.Trailer())
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
		}
		return status.Error(codes.Internal, "proxy: both pumps exited without upstream completion")
	}
}

// pumpClientToUpstream copies request frames from the client into the
// upstream call. Returns io.EOF when the client finishes sending.
func pumpClientToUpstream(src grpc.ServerStream, dst grpc.ClientStream) error {
	f := &rawFrame{}
	for {
		if err := src.RecvMsg(f); err != nil {
			return err
		}
		if err := dst.SendMsg(f); err != nil {
			return err
		}
	}
}

// pumpUpstreamToClient copies response frames from the upstream back to the
// client, forwarding the upstream's headers before the first frame. Returns
// io.EOF on clean upstream completion, or the upstream's status error.
func pumpUpstreamToClient(src grpc.ClientStream, dst grpc.ServerStream) error {
	f := &rawFrame{}
	for i := 0; ; i++ {
		if err := src.RecvMsg(f); err != nil {
			return err
		}
		if i == 0 {
			// Headers become available after the first response message;
			// they must reach the client before any frame.
			md, err := src.Header()
			if err != nil {
				return err
			}
			if err := dst.SendHeader(md); err != nil {
				return err
			}
		}
		if err := dst.SendMsg(f); err != nil {
			return err
		}
	}
}
