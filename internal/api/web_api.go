package api

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/kpetremann/jackadi/internal/config"
	"github.com/kpetremann/jackadi/internal/proto"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	protobuf "google.golang.org/protobuf/proto"
)

type Config struct {
	ConfigDir     string
	APIAddress    string
	APIPort       string
	APITLSEnabled bool
	APITLSCert    string
	APITLSKey     string
}

type proxyResponse struct {
	Id            int64
	GroupID       *int64
	Output        json.RawMessage
	Error         string
	Retcode       int32
	InternalError string
	ModuleError   string
}

type Htpasswd struct {
	creds map[string]string
}

func NewHtpasswd() Htpasswd {
	return Htpasswd{creds: make(map[string]string)}
}

func (h *Htpasswd) Get(user string) (string, error) {
	password, ok := h.creds[user]
	if !ok {
		return "", errors.New("unknown user")
	}
	if password == "" {
		return "", errors.New("empty password")
	}

	return password, nil
}

func (h *Htpasswd) load(file string) error {
	fd, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("htpasswd not loaded: %w", err)
	}
	defer func() {
		_ = fd.Close()
	}()

	creds := make(map[string]string)
	sc := bufio.NewScanner(fd)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		creds[parts[0]] = parts[1]
	}
	h.creds = creds
	if len(h.creds) == 0 {
		return errors.New("no credentials in htpasswd file")
	}
	return nil
}

func (h *Htpasswd) basicAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()

		expectedHash, err := h.Get(username)
		if err != nil {
			_ = bcrypt.CompareHashAndPassword([]byte("$2y$10$wXuhWuwaECzjbYIrD9wH3OssSLDQCEojoeoWaCoIy9Dwa1L0XOwOS"), []byte(password))
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Unauthorized","message":"Authentication required","status":401}`))
			return
		}

		if !ok || bcrypt.CompareHashAndPassword([]byte(expectedHash), []byte(password)) != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Unauthorized","message":"Authentication failed","status":401}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func responseEnvelope(_ context.Context, response protobuf.Message) (any, error) {
	if out, ok := response.(*proto.FwdResponse); ok {
		decodedResponses := make(map[string]*proxyResponse, len(out.GetResponses()))

		for nodeName, response := range out.GetResponses() {
			decodedResponse := proxyResponse{
				Id:            response.GetId(),
				GroupID:       response.GroupID,
				Output:        response.GetOutput(),
				Error:         response.GetError(),
				InternalError: response.GetInternalError().String(),
				ModuleError:   response.GetModuleError(),
			}
			decodedResponses[nodeName] = &decodedResponse
		}
		return decodedResponses, nil
	}

	return response, nil
}

func StartHTTPProxy(ctx context.Context, cfg Config) error {
	cancelableCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	mux := runtime.NewServeMux(
		runtime.WithForwardResponseRewriter(responseEnvelope),
	)
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	endpoint := fmt.Sprintf("unix:///%s", config.CLISocket)

	if err := proto.RegisterForwarderHandlerFromEndpoint(cancelableCtx, mux, endpoint, opts); err != nil {
		return err
	}

	if err := proto.RegisterAPIHandlerFromEndpoint(cancelableCtx, mux, endpoint, opts); err != nil {
		return err
	}
	// Wrap the mux with basic auth middleware
	slog.Info("loading htpasswd")
	htpasswd := NewHtpasswd()
	err := htpasswd.load(filepath.Join(cfg.ConfigDir, config.HTPasswordFile))
	if err != nil {
		slog.Warn("htpasswd not loaded", "error", err)
	}
	authorizer := NewAuthorizer(cfg.ConfigDir)
	if err := authorizer.Load(); err != nil {
		return fmt.Errorf("failed to load permissions, please check authorization.yaml: %w", err)
	}
	authHandler := htpasswd.basicAuthMiddleware(authorizer.handler(mux))

	// start HTTP server (and proxy calls to gRPC server endpoint)
	apiAddr := fmt.Sprintf("%s:%s", cfg.APIAddress, cfg.APIPort)
	httpServer := http.Server{
		Addr:              apiAddr,
		Handler:           authHandler,
		ReadHeaderTimeout: config.HTTPReadHeaderTimeout,
	}

	if cfg.APITLSEnabled {
		if cfg.APITLSCert == "" || cfg.APITLSKey == "" {
			return errors.New("API TLS enabled but certificate or key file not specified")
		}

		certs, err := config.GetAPITLSCertificate(cfg.APITLSCert, cfg.APITLSKey)
		if err != nil {
			return fmt.Errorf("failed to load API TLS configuration: %w", err)
		}

		httpServer.TLSConfig = &tls.Config{Certificates: certs, MinVersion: tls.VersionTLS12}
	}
	slog.Info("starting Web API", "address", apiAddr)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Warn("web api failed to stop properly", "error", err)
		}
	}()

	if cfg.APITLSEnabled {
		return httpServer.ListenAndServeTLS("", "") // no cert file as already configured TLS earlier
	}
	return httpServer.ListenAndServe()
}
