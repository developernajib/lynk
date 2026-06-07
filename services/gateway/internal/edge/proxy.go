// proxy.go is the gRPC-Web ↔ gRPC bridge. The browser speaks gRPC-Web,
// backends speak plain gRPC, and neither understands the other, so the
// gateway translates: browser gRPC-Web → grpcweb wrapper → transparent raw
// proxy (forward.go) → backend gRPC. The gateway imports no service protos;
// it is a pure pipe routed by the method's package prefix.
package edge

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/improbable-eng/grpc-web/go/grpcweb"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/developernajib/lynk/services/gateway/internal/platform/config"
)

// routes maps proto package prefixes to backend names. ADD A LINE HERE when
// a new module's proto package appears, and flip the backend name when a
// module is extracted into its own service: routing is the only thing that
// changes.
var routes = []route{
	{"example.", "core"},
	{"identity.", "core"},
	{"authz.", "core"},
}

type route struct {
	packagePrefix string
	backend       string
}

// forwardableHeaders is the allowlist of metadata forwarded to backends: the
// verified principal plus the correlation id. Forwarding ALL inbound
// metadata would include reserved transport and grpc-web headers that
// corrupt the gateway-to-backend HTTP/2 stream.
var forwardableHeaders = []string{
	"x-user-id", "x-role", "x-token-type", "x-request-id",
}

// Proxy holds one lazy, long-lived client connection per backend and the
// gRPC-Web-wrapped transparent proxy server.
type Proxy struct {
	connections map[string]*grpc.ClientConn
	webHandler  http.Handler
}

// NewProxy dials every upstream (lazily: a backend briefly down at startup
// does not fail the gateway) and builds the bridge.
func NewProxy(upstreams config.UpstreamsConfig) (*Proxy, error) {
	creds, err := buildTransportCredentials(upstreams.TLS)
	if err != nil {
		return nil, err
	}

	backends := map[string]string{
		"core": upstreams.Core,
	}

	connections := make(map[string]*grpc.ClientConn, len(backends))
	for name, addr := range backends {
		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(creds),
			// The client-side OTel handler injects traceparent into the
			// proxied metadata: the gateway→backend half of distributed
			// tracing.
			grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		)
		if err != nil {
			for _, opened := range connections {
				_ = opened.Close()
			}
			return nil, fmt.Errorf("edge: dial %s: %w", name, err)
		}
		connections[name] = conn
	}

	p := &Proxy{connections: connections}

	// The transparent proxy server registers no services; every call lands
	// in the UnknownServiceHandler, which forwards raw frames to the backend
	// the director picks.
	grpcServer := grpc.NewServer(
		grpc.ForceServerCodec(rawCodec{}),
		grpc.UnknownServiceHandler(transparentHandler(p.direct)),
	)

	// Origin checking is permissive here because the CORS middleware earlier
	// in the chain already enforces the real allowlist.
	p.webHandler = grpcweb.WrapServer(
		grpcServer,
		grpcweb.WithOriginFunc(func(string) bool { return true }),
	)
	return p, nil
}

// direct picks the backend for a method and forwards only the allowlisted
// metadata.
func (p *Proxy) direct(ctx context.Context, fullMethod string) (context.Context, *grpc.ClientConn, error) {
	conn := p.resolve(fullMethod)
	if conn == nil {
		return ctx, nil, status.Errorf(codes.Unimplemented, "no backend for method %s", fullMethod)
	}

	incoming, _ := metadata.FromIncomingContext(ctx)
	forwarded := metadata.MD{}
	for _, key := range forwardableHeaders {
		if values := incoming.Get(key); len(values) > 0 {
			forwarded.Set(key, values...)
		}
	}
	return metadata.NewOutgoingContext(ctx, forwarded), conn, nil
}

// resolve matches "/example.v1.ExampleService/CreateNote" against the route
// table by package prefix.
func (p *Proxy) resolve(path string) *grpc.ClientConn {
	trimmed := strings.TrimPrefix(path, "/")
	for _, r := range routes {
		if strings.HasPrefix(trimmed, r.packagePrefix) {
			return p.connections[r.backend]
		}
	}
	return nil
}

// Handler returns the bridge handler the middleware chain wraps.
func (p *Proxy) Handler() http.Handler {
	return p.webHandler
}

// Close shuts every upstream connection; registered as a shutdown hook.
func (p *Proxy) Close(context.Context) error {
	for _, conn := range p.connections {
		_ = conn.Close()
	}
	return nil
}

// buildTransportCredentials secures gateway-to-backend connections: plain in
// development (trio unset; partial trios already failed validation), mutual
// TLS 1.3 when the INTERNAL_TLS_* trio is configured. RootCAs authenticates
// the backend to the gateway; the client keypair authenticates the gateway
// to the backend (which runs RequireAndVerifyClientCert): zero trust in both
// directions.
func buildTransportCredentials(internalTLS config.InternalTLSConfig) (credentials.TransportCredentials, error) {
	if !internalTLS.Enabled() {
		return insecure.NewCredentials(), nil
	}

	keyPair, err := tls.LoadX509KeyPair(internalTLS.CertFile, internalTLS.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("edge: load internal tls client keypair: %w", err)
	}

	caBytes, err := os.ReadFile(filepath.Clean(internalTLS.CAFile))
	if err != nil {
		return nil, fmt.Errorf("edge: read internal tls ca: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caBytes) {
		return nil, fmt.Errorf("edge: internal tls ca file contained no valid certificates")
	}

	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{keyPair},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS13,
	}), nil
}
