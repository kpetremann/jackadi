---
title: 'Writing spec collectors'
weight: 3
---

## What are spec collectors?

Spec collectors are Go functions that:
* Run automatically on a periodic schedule (default: 60 seconds).
* Gather information.
* Return structured data for querying via dotted notation.

Unlike tasks which are executed on demand, spec collectors run continuously in the background.

## Function signature

All spec collectors must follow this exact signature:

```go
func CollectorName(ctx context.Context) (ReturnType, error)
```

### Requirements

1. **Context Parameter**: For cancellation and timeout handling.
2. **Return Type**: Any serializable Go type (struct, map, slice, primitives), same as tasks.
3. **Error Return**: Standard Go error handling.

## Basic example

```go
package main

import (
	"context"
	"runtime"
	"os"
	"github.com/kpetremann/jackadi/sdk"
)

type SystemInfo struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	CPUCores     int    `json:"cpu_cores"`
	Hostname     string `json:"hostname"`
}

func SystemInfoCollector(ctx context.Context) (SystemInfo, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	return SystemInfo{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		CPUCores:     runtime.NumCPU(),
		Hostname:     hostname,
	}, nil
}

func main() {
	plugin := sdk.New("system-info")
	plugin.MustRegisterSpecCollector("basic", SystemInfoCollector)
	sdk.MustServe(plugin)
}
```

## Registration and usage

### Multiple collectors

```go
func main() {
	plugin := sdk.New("infrastructure")

	// Register multiple collectors
	plugin.MustRegisterSpecCollector("system", SystemInfoCollector)
	plugin.MustRegisterSpecCollector("network", NetworkCollector)
	plugin.MustRegisterSpecCollector("docker", DockerStatsCollector)

	sdk.MustServe(plugin)
}
```

### Accessing collected data

```sh
# List available specs
jack run node1 specs.list

# Get all spec data
jack run node1 specs.all

# Get specific data using dotted notation
jack run node1 specs.get infrastructure.system.cpu_cores
jack run node1 specs.get infrastructure.network.interfaces[0].ip_address
```

### Targeting with specs

```sh
# Target based on OS
jack run -q "specs.infrastructure.system.os==linux" linux-task.run
```

## Best practices

1. **Performance**: Keep collectors fast and lightweight.
2. **Error Handling**: Handle errors gracefully without affecting node stability.
3. **Caching**: Cache expensive operations with appropriate TTL.
4. **Structure**: Design data for easy querying with dotted notation.
5. **Naming**: Use consistent, descriptive naming conventions.
6. **Security**: Avoid collecting sensitive information.
7. **Context**: Always respect context cancellation and timeouts.
