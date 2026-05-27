package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/kpetremann/jackadi/internal/plugin/loader/hcplugin"
	"github.com/kpetremann/jackadi/internal/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type pluginInfo struct {
	Filename string
	Checksum string
}

func (s *Server) loadPluginsPolicies() (map[string][]pluginInfo, error) {
	s.pluginPolicies.lock.Lock()
	defer s.pluginPolicies.lock.Unlock()
	if time.Since(s.pluginPolicies.lastUpdate) < 10*time.Second {
		slog.Debug("get plugin list from cache")
		return s.pluginPolicies.cache, nil
	}

	// TODO: do we really want to "live" reload from the file? maybe make this configurable
	configFile := path.Join(s.config.ConfigDir, "plugins.yaml")
	slog.Debug("get plugin list from file", "file", configFile)
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load plugin list file: %w", err)
	}

	cfg := map[string][]string{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid plugin list file: %w", err)
	}

	// for each pluginInfo in list, calculate sha256
	checksums := make(map[string]string)
	pluginsPerPattern := make(map[string][]pluginInfo, len(cfg))
	for pattern, pluginList := range cfg {
		for _, filename := range pluginList {
			if checksum, ok := checksums[filename]; !ok || checksum == "" {
				file := filepath.Join(s.config.PluginDir, filename)
				chksum, err := hcplugin.CalculateChecksum(file)
				if err != nil {
					slog.Error("failed to calculate checksum", "file", file)
					continue
				}
				checksums[filename] = chksum
			}

			pluginsPerPattern[pattern] = append(pluginsPerPattern[pattern], pluginInfo{
				Filename: filename,
				Checksum: checksums[filename],
			})
		}
	}

	s.pluginPolicies.cache = pluginsPerPattern
	s.pluginPolicies.lastUpdate = time.Now()

	return pluginsPerPattern, nil
}

func (s *Server) ListNodePlugins(ctx context.Context, req *emptypb.Empty) (*proto.ListNodePluginsResponse, error) {
	resp := &proto.ListNodePluginsResponse{}
	nd, err := signatureFromContext(ctx, s.config.MTLSEnabled)
	if err != nil {
		return resp, status.Error(codes.InvalidArgument, err.Error())
	}

	pluginsPerPattern, err := s.loadPluginsPolicies()
	if err != nil {
		return resp, status.Error(codes.NotFound, fmt.Sprintf("failed to load plugin list: %s", err))
	}

	nodePlugins := make(map[string]string)
	for pattern, plugins := range pluginsPerPattern {
		matched, err := filepath.Match(pattern, string(nd.ID))
		if err != nil {
			return nil, fmt.Errorf("invalid pattern '%s': %w ", pattern, err)
		}
		if !matched {
			continue
		}
		for _, p := range plugins {
			nodePlugins[p.Filename] = p.Checksum
		}
	}
	resp.Plugin = nodePlugins
	return resp, nil
}
