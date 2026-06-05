package grpcserver

import (
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

// keepaliveEnforcement rejects clients that ping more often than every 15s
// (a cheap DoS) while still permitting pings on idle connections, which
// long-lived clients need to detect a dead peer.
func keepaliveEnforcement() grpc.ServerOption {
	return grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
		MinTime:             15 * time.Second,
		PermitWithoutStream: true,
	})
}

// keepaliveParams makes the server probe idle peers (ping after 30s idle,
// dead after 10s without a pong) and recycle connections older than 30m so
// load balancers can rebalance, with 5s grace for in-flight RPCs.
func keepaliveParams() grpc.ServerOption {
	return grpc.KeepaliveParams(keepalive.ServerParameters{
		Time:                  30 * time.Second,
		Timeout:               10 * time.Second,
		MaxConnectionAge:      30 * time.Minute,
		MaxConnectionAgeGrace: 5 * time.Second,
	})
}
