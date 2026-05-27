package api

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/kpetremann/jackadi/internal/config"
	"github.com/kpetremann/jackadi/internal/serializer"
)

type Role string
type User string

type Permission struct {
	Resource string `yaml:"resource"`
	Action   string `yaml:"action"`
}

func (p *Permission) Match(resource, action string) bool {
	resourceMatch := resource == p.Resource || p.Resource == "*"
	if !resourceMatch {
		return false
	}

	actionMatch := action == p.Action || p.Action == "*"
	return actionMatch
}

func parsePermission(s string) (Permission, error) {
	parts := strings.SplitN(s, ".", 2)
	if len(parts) != 2 {
		return Permission{}, fmt.Errorf("invalid permission format: %q (expected resource.action)", s)
	}
	return Permission{
		Resource: strings.TrimSpace(parts[0]),
		Action:   strings.TrimSpace(parts[1]),
	}, nil
}

type authorizationConfig struct {
	Users map[User]struct {
		Roles []Role `yaml:"roles"`
	} `yaml:"users"`
	Roles map[Role]struct {
		Endpoints []string `yaml:"endpoints"`
		Tasks     []string `yaml:"tasks"`
	} `yaml:"roles"`
}

type Permissions struct {
	Endpoints []Permission
	Tasks     []Permission
}

type ParsedAuthConfig struct {
	Users map[User][]Role
	Roles map[string]Permissions
}

type Authorizer struct {
	config    ParsedAuthConfig
	configDir string
}

func NewAuthorizer(configDir string) *Authorizer {
	return &Authorizer{
		config: ParsedAuthConfig{
			Users: make(map[User][]Role),
			Roles: make(map[string]Permissions),
		},
		configDir: configDir,
	}
}

func (a *Authorizer) Load() error {
	configFile := path.Join(a.configDir, "authorization.yaml")
	slog.Debug("loading authorization config", "file", configFile)

	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to load authorization config: %w", err)
	}

	rawConfig := authorizationConfig{}
	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("invalid authorization config file: %w", err)
	}

	parsedConfig := ParsedAuthConfig{
		Users: make(map[User][]Role),
		Roles: make(map[string]Permissions),
	}

	for username, userConfig := range rawConfig.Users {
		parsedConfig.Users[username] = userConfig.Roles
	}

	for roleName, roleConfig := range rawConfig.Roles {
		parsedRole := Permissions{
			Endpoints: make([]Permission, 0, len(roleConfig.Endpoints)),
			Tasks:     make([]Permission, 0, len(roleConfig.Tasks)),
		}

		// endpoint permissions
		for _, endpointStr := range roleConfig.Endpoints {
			perm, err := parsePermission(endpointStr)
			if err != nil {
				slog.Warn("invalid endpoint permission", "role", roleName, "permission", endpointStr, "error", err)
				continue
			}
			parsedRole.Endpoints = append(parsedRole.Endpoints, perm)
		}

		// task permissions
		for _, taskStr := range roleConfig.Tasks {
			perm, err := parsePermission(taskStr)
			if err != nil {
				slog.Warn("invalid task permission", "role", roleName, "permission", taskStr, "error", err)
				continue
			}
			parsedRole.Tasks = append(parsedRole.Tasks, perm)
		}

		parsedConfig.Roles[string(roleName)] = parsedRole
	}

	a.config = parsedConfig

	slog.Info("authorization config loaded", "users", len(parsedConfig.Users), "roles", len(parsedConfig.Roles))
	return nil
}

func (a *Authorizer) canAccessEndpoint(username, resource, action string) bool {
	userRoles, ok := a.config.Users[User(username)]
	if !ok {
		slog.Debug("user not found in config")
		return false
	}

	for _, roleName := range userRoles {
		role, ok := a.config.Roles[string(roleName)]
		if !ok {
			slog.Warn("role not found in config")
			continue
		}

		for _, perm := range role.Endpoints {
			if perm.Match(resource, action) {
				slog.Debug("permission granted")
				return true
			}
		}
	}

	slog.Debug("permission denied")
	return false
}

func (a *Authorizer) canAccessTask(username, plugin, task string) bool {
	userRoles, ok := a.config.Users[User(username)]
	if !ok {
		slog.Debug("user not found in config")
		return false
	}

	for _, roleName := range userRoles {
		role, ok := a.config.Roles[string(roleName)]
		if !ok {
			slog.Debug("role not found in config")
			continue
		}

		for _, perm := range role.Tasks {
			if perm.Match(plugin, task) {
				slog.Debug("task permission granted")
				return true
			}
		}
	}

	slog.Debug("task permission denied")
	return false
}

func (a *Authorizer) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// we expect credentials have already been validated with auth handler
		username, _, ok := r.BasicAuth()
		if !ok || username == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Unauthorized","message":"authentication required","status":401}`))
			return
		}

		// parse endpoint: /v1/resource/action -> resource, action
		endpoint := strings.TrimPrefix(r.URL.Path, "/v1/")
		parts := strings.Split(endpoint, "/")

		if len(parts) < 2 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"Bad Request","message":"invalid endpoint","status":400}`))
			return
		}

		// handles task execution perms
		if len(parts) >= 2 && parts[0] == "task" && parts[1] == "exec" {
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"Bad Request","message":"failed to read request body","status":400}`))
				return
			}
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			execReq := struct {
				Task string `json:"task"`
			}{}
			if err := serializer.JSON.Unmarshal(bodyBytes, &execReq); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"Bad Request","message":"failed to extract requested task from request","status":400}`))
				return
			}

			taskParts := strings.Split(execReq.Task, config.PluginSeparator)
			if len(taskParts) < 2 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"Bad Request","message":"failed to parse plugin.task","status":400}`))
				return
			}

			plugin, task := taskParts[0], taskParts[1]
			if !a.canAccessTask(username, plugin, task) {
				w.WriteHeader(http.StatusForbidden)
				slog.Warn("insufficient permissions", "plugin", plugin, "task", task)
				_, _ = w.Write([]byte(`{"error":"Forbidden","message":"insufficient permissions to execute this task","status":403}`))
				return
			}

			next.ServeHTTP(w, r)
			return
		}

		// handles endpoint call perms
		resource, action := parts[0], parts[1]
		if !a.canAccessEndpoint(username, resource, action) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"Forbidden","message":"insufficient permissions","status":403}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}
