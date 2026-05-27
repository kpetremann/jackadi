package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kpetremann/jackadi/sdk"
)

// Configuration structure for tasks that support options.
type DemoOptions struct {
	Verbose    bool
	OutputFile string
	Timeout    int
	Region     string
}

func (o *DemoOptions) SetDefaults() {
	o.Verbose = false
	o.Timeout = 30
	o.Region = "us-east-1"
}

// UpgradeOptions for OS upgrade operations.
type UpgradeOptions struct {
	DryRun          bool `jackadi:"dry-run"`
	SecurityOnly    bool `jackadi:"securityonly"`
	ExcludePackages []string
	RebootRequired  bool
	BackupBefore    bool
}

func (o *UpgradeOptions) SetDefaults() {
	o.DryRun = false
	o.SecurityOnly = false
	o.RebootRequired = false
	o.BackupBefore = true
}

// User represents a user in the system.
type User struct {
	ID       int64             `jackadi:"id"`
	Username string            `jackadi:"username"`
	Email    string            `jackadi:"email"`
	Metadata map[string]string `jackadi:"metadata,omitempty"`
}

// ServerInfo represents server information.
type ServerInfo struct {
	Hostname     string   `jackadi:"hostname"`
	IPAddresses  []string `jackadi:"ip_addresses"`
	Services     []string `jackadi:"services"`
	LastReboot   string   `jackadi:"last_reboot"`
	DiskUsage    float64  `jackadi:"disk_usage_percent"`
	CPUCores     int      `jackadi:"cpu_cores"`
	MemoryGB     int      `jackadi:"memory_gb"`
	IsProduction bool     `jackadi:"is_production"`
}

// DatabaseStats represents database statistics.
type DatabaseStats struct {
	Connections int            `jackadi:"active_connections"`
	QueryStats  map[string]any `jackadi:"query_stats"`
	TableSizes  []TableInfo    `jackadi:"table_sizes"`
}

type TableInfo struct {
	Name    string  `jackadi:"name"`
	SizeGB  float64 `jackadi:"size_gb"`
	Records int64   `jackadi:"records"`
}

// Network configuration spec.
type NetworkConfig struct {
	Interfaces []NetworkInterface `jackadi:"interfaces"`
	Routes     []string           `jackadi:"default_routes"`
	DNS        []string           `jackadi:"dns_servers"`
}

type NetworkInterface struct {
	Name      string `jackadi:"name"`
	Type      string `jackadi:"type"`
	State     string `jackadi:"state"`
	IPAddress string `jackadi:"ip_address,omitempty"`
	MAC       string `jackadi:"mac_address"`
}

// 1. Simple hello task.
func HelloWorld() (string, error) {
	return "Hello, World! This is Jackadi distributed task execution.", nil
}

// 2. Simple task using options.
func ConfigureService(ctx context.Context, options *DemoOptions, serviceName string) (string, error) {
	if options.Verbose {
		return fmt.Sprintf("Configuring service '%s' in region '%s' with timeout %d seconds (verbose mode enabled)",
			serviceName, options.Region, options.Timeout), nil
	}
	return fmt.Sprintf("Service '%s' configured successfully in %s", serviceName, options.Region), nil
}

// 3. Simple task using context (demonstrating timeout awareness).
func MonitorSystemHealth(ctx context.Context) (map[string]any, error) {
	// Simulate monitoring with context awareness.
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("monitoring cancelled: %w", ctx.Err())
	case <-time.After(time.Millisecond):
		return map[string]any{
			"status":           "healthy",
			"cpu_usage":        23.5,
			"memory_usage":     67.2,
			"disk_space_free":  "45.2GB",
			"active_processes": 142,
			"uptime_hours":     72.5,
		}, nil
	}
}

// 4. Task with multiple argument types (no options/context).
func CreateUserAccount(
	userID int64,
	username string,
	email string,
	isActive bool,
	permissions []string,
	metadata map[string]string,
	serverConfig ServerInfo,
	limits [3]int, // array with fixed size.
) (User, error) {
	// Process the various input types.
	user := User{
		ID:       userID,
		Username: username,
		Email:    email,
		Metadata: metadata,
	}

	// Log the processing (in real implementation).
	fmt.Printf("Creating user on server: %s with %d CPU cores\n",
		serverConfig.Hostname, serverConfig.CPUCores)
	fmt.Printf("User permissions: %v\n", permissions)
	fmt.Printf("Resource limits: %v\n", limits)
	fmt.Printf("Active status: %v\n", isActive)

	return user, nil
}

// Tasks showcasing different output types (no input args).

// 5. Return simple string.
func GetSystemVersion() (string, error) {
	return "openSUSE Tumbleweed 20240115 (Snapshot)", nil
}

// 6. Return integer.
func GetActiveConnectionCount() (int64, error) {
	return 1247, nil
}

// 7. Return boolean.
func IsMaintenanceModeEnabled() (bool, error) {
	return false, nil
}

// 8. Return float.
func GetCPUUsagePercent() (float64, error) {
	return 23.47, nil
}

// 9. Return slice of strings.
func ListActiveServices() ([]string, error) {
	return []string{
		"webserver-pro",
		"datastore-engine",
		"cache-service",
		"container-runtime",
		"secure-shell",
	}, nil
}

// 10. Return slice of structs.
func GetUserList() ([]User, error) {
	return []User{
		{ID: 1, Username: "alice", Email: "alice@jackadi.io"},
		{ID: 2, Username: "bob", Email: "bob@jackadi.io"},
		{ID: 3, Username: "charlie", Email: "charlie@jackadi.io"},
	}, nil
}

// 11. Return map of strings.
func GetEnvironmentVariables() (map[string]string, error) {
	return map[string]string{
		"NODE_ENV":     "production",
		"DATABASE_URL": "dbstore://localhost:5432/myapp",
		"CACHE_URL":    "cache://localhost:6379",
		"API_VERSION":  "v2.1.0",
		"LOG_LEVEL":    "info",
	}, nil
}

// 12. Return map with interface{} values.
func GetSystemMetrics() (map[string]any, error) {
	return map[string]any{
		"hostname":        "web-server-01",
		"uptime_seconds":  259200,
		"load_average":    []float64{1.2, 1.5, 1.8},
		"memory_usage_mb": 2048,
		"disk_usage":      map[string]string{"root": "45%", "home": "23%"},
		"is_healthy":      true,
		"last_check":      "2024-01-15T10:30:00Z",
	}, nil
}

// 13. Return complex struct.
func GetServerInformation() (ServerInfo, error) {
	return ServerInfo{
		Hostname:     "prod-web-01.jackadi.io",
		IPAddresses:  []string{"192.168.1.100", "10.0.0.50"},
		Services:     []string{"webserver-pro", "app-engine", "datastore"},
		LastReboot:   "2024-01-10T08:15:00Z",
		DiskUsage:    67.5,
		CPUCores:     8,
		MemoryGB:     32,
		IsProduction: true,
	}, nil
}

// 14. Return array (fixed size).
func GetLastThreeReboots() ([3]string, error) {
	return [3]string{
		"2024-01-10T08:15:00Z",
		"2023-12-15T02:30:00Z",
		"2023-11-20T14:45:00Z",
	}, nil
}

// 15. Return map with struct values.
func GetDatabaseStats() (map[string]DatabaseStats, error) {
	return map[string]DatabaseStats{
		"primary": {
			Connections: 45,
			QueryStats: map[string]any{
				"avg_query_time_ms": 12.5,
				"slow_queries":      3,
				"total_queries":     15420,
			},
			TableSizes: []TableInfo{
				{Name: "users", SizeGB: 2.3, Records: 50000},
				{Name: "orders", SizeGB: 5.7, Records: 125000},
			},
		},
		"replica": {
			Connections: 23,
			QueryStats: map[string]any{
				"avg_query_time_ms": 8.2,
				"slow_queries":      1,
				"total_queries":     8960,
			},
			TableSizes: []TableInfo{
				{Name: "users", SizeGB: 2.3, Records: 50000},
				{Name: "orders", SizeGB: 5.7, Records: 125000},
			},
		},
	}, nil
}

// 16. Return pointer (can be nil).
func FindUserByEmail(email string) (*User, error) {
	if email == "admin@jackadi.io" {
		return &User{
			ID:       999,
			Username: "admin",
			Email:    "admin@jackadi.io",
			Metadata: map[string]string{"role": "administrator"},
		}, nil
	}
	return nil, nil //nolint:nilnil // expected
}

// OS upgrade task with write lock - no real implementation, just demo.
func UpgradeSystem(ctx context.Context, options *UpgradeOptions) (map[string]any, error) {
	// Simulate OS upgrade process.
	result := map[string]any{
		"status":           "completed",
		"packages_updated": 47,
		"security_patches": 12,
		"reboot_required":  options.RebootRequired,
		"duration_seconds": 180,
		"dry_run":          options.DryRun,
	}

	if options.DryRun {
		result["status"] = "dry-run-completed"
		result["message"] = "Dry run completed - no actual changes made"
	}

	if options.SecurityOnly {
		result["packages_updated"] = 12
		result["message"] = "Security updates only"
	}

	if len(options.ExcludePackages) > 0 {
		result["excluded_packages"] = options.ExcludePackages
	}

	return result, nil
}

// Spec collector examples - these gather system information.

// Simple spec returning basic OS info.
func GetOSInfo() (map[string]string, error) {
	return map[string]string{
		"os":           "linux",
		"distribution": "opensuse-tumbleweed",
		"version":      "20240115",
		"kernel":       "6.7.1-1-default",
		"architecture": "x86_64",
	}, nil
}

// Spec returning hardware information as struct.
func GetHardwareInfo() (ServerInfo, error) {
	return ServerInfo{
		Hostname:     "demo-server-01",
		IPAddresses:  []string{"192.168.1.10", "10.0.0.15"},
		Services:     []string{"systemd", "networkd", "resolved"},
		LastReboot:   "2024-01-15T06:30:00Z",
		DiskUsage:    42.8,
		CPUCores:     4,
		MemoryGB:     16,
		IsProduction: false,
	}, nil
}

func GetNetworkConfig() (NetworkConfig, error) {
	return NetworkConfig{
		Interfaces: []NetworkInterface{
			{
				Name:      "eth0",
				Type:      "ethernet",
				State:     "up",
				IPAddress: "192.168.1.10/24",
				MAC:       "00:50:56:12:34:56",
			},
			{
				Name:  "lo",
				Type:  "loopback",
				State: "up",
				MAC:   "00:00:00:00:00:00",
			},
		},
		Routes: []string{"0.0.0.0/0 via 192.168.1.1"},
		DNS:    []string{"8.8.8.8", "1.1.1.1"},
	}, nil
}

// Software inventory spec.
func GetInstalledPackages() (map[string]any, error) {
	return map[string]any{
		"package_manager": "zypper",
		"total_packages":  2847,
		"critical_packages": []map[string]string{
			{"name": "webserver-pro", "version": "2.4.x-demo"},
			{"name": "datastore-engine", "version": "15.x-demo"},
			{"name": "container-runtime", "version": "25.x-demo"},
			{"name": "secure-shell-server", "version": "9.x-demo"},
		},
		"security_updates": 3,
		"last_update":      "2024-01-10T14:22:00Z",
	}, nil
}

func main() {
	plugin := sdk.New("demo")

	// Register tasks with meaningful descriptions and examples.

	plugin.MustRegisterTask("hello", HelloWorld).
		WithSummary("Simple hello world task").
		WithDescription("A basic task that returns a greeting message. Perfect for testing connectivity and basic functionality.")

	plugin.MustRegisterTask("configure_service", ConfigureService).
		WithSummary("Configure a system service").
		WithDescription("Configures a named service with regional settings and timeout controls. Demonstrates option usage.").
		WithArg("serviceName", "string", "webserver-pro").
		WithLockMode(sdk.WriteLock)

	plugin.MustRegisterTask("monitor_health", MonitorSystemHealth).
		WithSummary("Monitor system health metrics").
		WithDescription("Performs system health monitoring with context timeout awareness. Returns comprehensive health metrics.").
		WithLockMode(sdk.NoLock)

	plugin.MustRegisterTask("create_user", CreateUserAccount).
		WithSummary("Create a new user account").
		WithDescription("Creates a user account demonstrating various input types: int64, string, bool, slice, map, struct, and array.").
		WithArg("userID", "int64", "12345").
		WithArg("username", "string", "johndoe").
		WithArg("email", "string", "john@jackadi.io").
		WithArg("isActive", "bool", "true").
		WithArg("permissions", "[]string", `["read", "write"]`).
		WithArg("metadata", "map[string]string", `{"department": "engineering"}`).
		WithArg("serverConfig", "ServerInfo", `{"hostname": "web-01", "cpu_cores": 4}`).
		WithArg("limits", "[3]int", `[100, 200, 300]`).
		WithLockMode(sdk.WriteLock)

	plugin.MustRegisterTask("get_system_version", GetSystemVersion).
		WithSummary("Get system version (returns string)").
		WithDescription("Returns the operating system version as a string.")

	plugin.MustRegisterTask("get_connection_count", GetActiveConnectionCount).
		WithSummary("Get active connections (returns int64)").
		WithDescription("Returns the number of active database connections as an integer.")

	plugin.MustRegisterTask("is_maintenance_mode", IsMaintenanceModeEnabled).
		WithSummary("Check maintenance mode (returns bool)").
		WithDescription("Returns whether the system is currently in maintenance mode.")

	plugin.MustRegisterTask("get_cpu_usage", GetCPUUsagePercent).
		WithSummary("Get CPU usage (returns float64)").
		WithDescription("Returns current CPU usage percentage as a floating-point number.")

	plugin.MustRegisterTask("list_services", ListActiveServices).
		WithSummary("List active services (returns []string)").
		WithDescription("Returns a list of currently active system services.")

	plugin.MustRegisterTask("get_users", GetUserList).
		WithSummary("Get user list (returns []User)").
		WithDescription("Returns a list of system users as a slice of User structs.")

	plugin.MustRegisterTask("get_env_vars", GetEnvironmentVariables).
		WithSummary("Get environment variables (returns map[string]string)").
		WithDescription("Returns system environment variables as a string-to-string map.")

	plugin.MustRegisterTask("get_metrics", GetSystemMetrics).
		WithSummary("Get system metrics (returns map[string]any)").
		WithDescription("Returns comprehensive system metrics with mixed value types.")

	plugin.MustRegisterTask("get_server_info", GetServerInformation).
		WithSummary("Get server information (returns ServerInfo)").
		WithDescription("Returns detailed server information as a complex struct.")

	plugin.MustRegisterTask("get_reboot_history", GetLastThreeReboots).
		WithSummary("Get reboot history (returns [3]string)").
		WithDescription("Returns the last three system reboots as a fixed-size array.")

	plugin.MustRegisterTask("get_db_stats", GetDatabaseStats).
		WithSummary("Get database statistics (returns map[string]DatabaseStats)").
		WithDescription("Returns database statistics for multiple databases with complex nested structures.")

	plugin.MustRegisterTask("find_user", FindUserByEmail).
		WithSummary("Find user by email (returns *User)").
		WithDescription("Searches for a user by email address. Returns pointer (may be nil if not found).").
		WithArg("email", "string", "admin@jackadi.io")

	plugin.MustRegisterTask("upgrade_system", UpgradeSystem).
		WithSummary("Upgrade system packages").
		WithDescription("Performs system package upgrades with various options. Uses write lock to prevent conflicts during upgrades.").
		WithLockMode(sdk.WriteLock)

	// Register spec collectors - these gather system information for inventory and targeting.
	plugin.MustRegisterSpecCollector("os", GetOSInfo).
		WithSummary("Operating system information").
		WithDescription("Collects basic operating system and kernel information for system identification.")

	plugin.MustRegisterSpecCollector("hardware", GetHardwareInfo).
		WithSummary("Hardware specifications").
		WithDescription("Gathers hardware information including CPU, memory, disk usage, and network details.")

	plugin.MustRegisterSpecCollector("network", GetNetworkConfig).
		WithSummary("Network configuration").
		WithDescription("Retrieves network interface configuration, routing, and DNS settings.")

	plugin.MustRegisterSpecCollector("software", GetInstalledPackages).
		WithSummary("Software inventory").
		WithDescription("Collects information about installed packages, versions, and available updates.")

	sdk.MustServe(plugin)
}
