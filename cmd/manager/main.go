package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/kpetremann/jackadi/internal/api"
	"github.com/kpetremann/jackadi/internal/config"
	"github.com/kpetremann/jackadi/internal/manager/forwarder"
	"github.com/kpetremann/jackadi/internal/manager/inventory"
	"github.com/kpetremann/jackadi/internal/manager/management"
	"github.com/kpetremann/jackadi/internal/manager/server"
	"github.com/kpetremann/jackadi/internal/proto"
	flag "github.com/spf13/pflag"
	"google.golang.org/grpc"

	_ "github.com/kpetremann/jackadi/internal/logs"
)

var version = "dev"
var commit = "N/A"
var date = "N/A"

func printVersion() {
	if version != "dev" {
		version = fmt.Sprintf("v%s", version)
	}
	fmt.Printf("%s (commit: %s, build date: %s)\n", version, commit, date)
}

type managerConfig struct {
	configDir        string
	listenAddress    string
	listenPort       string
	pluginDir        string
	pluginServerPort string
	autoAcceptNode   bool

	mTLS          bool
	mTLSCert      string
	mTLSKey       string
	mTLSNodeCA    string
	apiEnabled    bool
	apiAddress    string
	apiPort       string
	apiTLSEnabled bool
	apiTLSCert    string
	apiTLSKey     string
}

func dbGC(ctx context.Context, db *badger.DB) {
	ticker := time.NewTicker(config.DatabaseGCInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			err := db.RunValueLogGC(config.DBGCThreshold)
			if err != nil {
				slog.Warn("database GC failed", "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// NewRelayGRPCServer creates a new GRPC server to serve both CLI and Web API.
func NewRelayGRPCServer(clusterServer *server.Server, dis forwarder.Dispatcher[*proto.TaskRequest, *proto.TaskResponse], db *badger.DB) *grpc.Server {
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	fwd := forwarder.New(dis, db)
	proto.RegisterForwarderServer(grpcServer, &fwd)

	apiServer := management.New(clusterServer, db)
	proto.RegisterAPIServer(grpcServer, &apiServer)

	return grpcServer
}

func run(cfg managerConfig) error {
	closeCh := make(chan struct{}, 10)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	dbOptions := badger.
		DefaultOptions(config.DatabaseDir).
		WithLogger(slogBadgerAdapter{})

	db, err := badger.Open(dbOptions)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dbGC(ctx, db)

	nodesInventory := inventory.New()
	if err := nodesInventory.LoadRegistry(); err != nil {
		slog.Info("unable to load registry", "error", err)
	}
	taskDispatcher := forwarder.NewDispatcher[*proto.TaskRequest, *proto.TaskResponse](&nodesInventory)

	// start manager main instance
	managerInstance, err := newManager(cfg, &nodesInventory, taskDispatcher, db)
	if err != nil {
		return err
	}
	defer managerInstance.Close()
	go func() {
		if err := managerInstance.Serve(); err != nil {
			slog.Error("manager failed to start", "error", err)
			closeCh <- struct{}{}
			return
		}

		slog.Warn("manager stoped", "error", err)
		closeCh <- struct{}{}
	}()

	// start specs collector
	go func() {
		err := managerInstance.CollectNodesSpecs(ctx)
		slog.Error("spec collector stopped", "error", err)
		closeCh <- struct{}{}
	}()

	// start plugin server
	pluginDir := http.Dir(cfg.pluginDir)
	fs := http.FileServer(pluginDir)
	mux := http.NewServeMux()
	mux.Handle("GET "+config.PluginServerPath, http.StripPrefix(config.PluginServerPath, fs))

	socket := net.JoinHostPort(cfg.listenAddress, cfg.pluginServerPort)
	httpServer := http.Server{Addr: socket, Handler: mux, ReadHeaderTimeout: config.HTTPReadHeaderTimeout}
	go func() {
		slog.Info("Starting static webserver", "socket", socket)
		err = httpServer.ListenAndServe()
		if err != nil {
			slog.Error("http server stopped", "error", err)
			closeCh <- struct{}{}
		}
	}()

	// GPRC server to handle CLI and API requests
	relayGRPCServer := NewRelayGRPCServer(managerInstance.ClusterServer, taskDispatcher, db)
	defer func() {
		if relayGRPCServer != nil {
			relayGRPCServer.Stop()
		}
	}()

	cliListener, closeCliListener, err := NewCLIListener()
	if err != nil {
		return err
	}
	defer closeCliListener()

	// server CLI
	slog.Info("starting local gRPC server for CLI")
	go func() {
		if err = relayGRPCServer.Serve(cliListener); err != nil {
			slog.Error("gRPC local server stopped", "reason", err)
			closeCh <- struct{}{}
		}
	}()

	// start API (HTTP proxy to gRPC)
	if cfg.apiEnabled {
		go func() {
			apiCfg := api.Config{
				ConfigDir:     cfg.configDir,
				APIAddress:    cfg.apiAddress,
				APIPort:       cfg.apiPort,
				APITLSEnabled: cfg.apiTLSEnabled,
				APITLSCert:    cfg.apiTLSCert,
				APITLSKey:     cfg.apiTLSKey,
			}
			err := api.StartHTTPProxy(ctx, apiCfg)
			if err != nil {
				slog.Error("API: HTTP proxy stopped", "reason", err)
				closeCh <- struct{}{}
			}
		}()
	}

	// graceful shutdown
	select {
	case <-closeCh:
		slog.Warn("closing")
	case <-sigCh:
		slog.Warn("received closing signal")
	}
	slog.Warn("shutdown")
	if err := httpServer.Shutdown(context.Background()); err != nil {
		slog.Error("plugin server: graceful shutdown failed", "error", err)
	}

	return nil
}

func main() {
	versionCmd := flag.BoolP("version", "v", false, "print version")

	// Setup flags using the new config module
	config.SetupManagerFlags()

	flag.CommandLine.SortFlags = false
	flag.Parse()

	if *versionCmd {
		printVersion()
		os.Exit(0)
	}

	// Load configuration using Viper
	configFile := ""
	if flag.Lookup("config") != nil {
		configFile = flag.Lookup("config").Value.String()
	}
	managerCfg, err := config.LoadManagerConfig(configFile)

	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	cfg := managerConfig{
		listenAddress:    managerCfg.ListenAddress,
		listenPort:       managerCfg.ListenPort,
		pluginDir:        managerCfg.PluginDir,
		pluginServerPort: managerCfg.PluginServerPort,
		mTLS:             managerCfg.MTLS.Enabled,
		mTLSKey:          managerCfg.MTLS.Key,
		mTLSCert:         managerCfg.MTLS.Cert,
		mTLSNodeCA:       managerCfg.MTLS.NodeCA,
		autoAcceptNode:   managerCfg.AutoAcceptNode,
		configDir:        managerCfg.ConfigDir,
		apiEnabled:       managerCfg.API.Enabled,
		apiAddress:       managerCfg.API.Address,
		apiPort:          managerCfg.API.Port,
		apiTLSEnabled:    managerCfg.API.TLS.Enabled,
		apiTLSCert:       managerCfg.API.TLS.Cert,
		apiTLSKey:        managerCfg.API.TLS.Key,
	}

	slog.Info("jackadi manager", "version", version, "commit", commit, "build date", date)

	if err := run(cfg); err != nil {
		slog.Error("shutdown", "error", err)
		os.Exit(1)
	}
}
