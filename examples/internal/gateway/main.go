package gateway

import (
	"context"
	"net/http"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/grpclog"
)

// Endpoint describes a gRPC endpoint
type Endpoint struct {
	Network, Addr string
}

// Options is a set of options to be passed to Run
type Options struct {
	// Addr is the address to listen
	Addr string

	// GRPCServer defines an endpoint of a gRPC service
	GRPCServer Endpoint

	// OpenAPIDir is a path to a directory from which the server
	// serves OpenAPI specs.
	OpenAPIDir string

	// Mux is a list of options to be passed to the gRPC-Gateway multiplexer
	Mux []gwruntime.ServeMuxOption
}

// Run starts a HTTP server and blocks while running if successful.
// The server will be shutdown when "ctx" is canceled.
func Run(ctx context.Context, opts Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	conn, err := dial(opts.GRPCServer.Network, opts.GRPCServer.Addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		if err := conn.Close(); err != nil {
			grpclog.Errorf("Failed to close a client connection to the gRPC server: %v", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/openapiv2/", openAPIServer(opts.OpenAPIDir))
	mux.HandleFunc("/healthz", healthzServer(conn))

	gw, err := newGateway(ctx, conn, opts.Mux)
	if err != nil {
		return err
	}
	mux.Handle("/", gw)

	// Do not use logRequestBody for ExcessBodyServer because it will perform
	// io.ReadAll and mask the issue:
	// https://github.com/grpc-ecosystem/grpc-gateway/issues/5236
	hmux := http.NewServeMux()
	hmux.Handle("/rpc/excess-body/", allowCORS(mux))
	hmux.Handle("/", logRequestBody(allowCORS(mux)))

	s := &http.Server{
		Addr:    opts.Addr,
		Handler: hmux,
	}
	go func() {
		<-ctx.Done()
		grpclog.Infof("Shutting down the http server")
		if err := s.Shutdown(context.Background()); err != nil {
			grpclog.Errorf("Failed to shutdown http server: %v", err)
		}
	}()

	grpclog.Infof("Starting listening at %s", opts.Addr)
	if err := s.ListenAndServe(); err != http.ErrServerClosed {
		grpclog.Errorf("Failed to listen and serve: %v", err)
		return err
	}
	return nil
}
