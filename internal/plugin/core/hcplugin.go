package core

import (
	"context"

	goplugin "github.com/hashicorp/go-plugin"
	"github.com/kpetremann/jackadi/internal/plugin/core/protoplugin"
	"google.golang.org/grpc"
)

// Handshake is a common handshake that is shared by plugin and host.
var Handshake = goplugin.HandshakeConfig{
	// This isn't required when using VersionedPlugins
	ProtocolVersion:  1,
	MagicCookieKey:   "JACKADI_PLUGIN_MAGIC_COOKIE",
	MagicCookieValue: "hv43fBvWLYYm0FWmur3V5KmJQXPR0woBg2F2MbyAcE8UuSCQGTlFJvTQUncCY0Xm",
}

type HCPlugin struct {
	goplugin.Plugin
	Impl Plugin
}

func (p *HCPlugin) GRPCServer(broker *goplugin.GRPCBroker, s *grpc.Server) error {
	protoplugin.RegisterJackadiPluginServer(s, &GRPCServer{Impl: p.Impl})
	return nil
}

func (*HCPlugin) GRPCClient(ctx context.Context, broker *goplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return &GRPCClient{client: protoplugin.NewJackadiPluginClient(c)}, nil
}
