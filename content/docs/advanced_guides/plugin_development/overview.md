---
title: 'Plugin overview'
weight: 1
---

## What are plugins?

Plugins extend Jackadi's functionality by providing two main capabilities:
1. **Tasks**: Executable functions that nodes can run on demand.
2. **Specs**: Automatic system information collectors that run continuously.

This overview covers the core concepts and structure. For detailed implementation guides, see [Writing Tasks](../writing_tasks/) and [Writing Spec Collectors](../writing_specs/).

## Plugin architecture

A Jackadi plugin is a Go binary that uses the SDK to register functionality with nodes. Each plugin can provide multiple tasks and spec collectors.

Plugins are running in their own processes and communicate with the node using gRPC, thanks to [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin/).

### Task naming convention

Tasks follow the format `plugin:task`:
* `cmd:run` - The `run` task from the `cmd` plugin.
* `health:ping` - The `ping` task from the `health` plugin.
* `specs.get` - The `get` task from the `specs` plugin.

## Basic plugin structure

Here's a minimal plugin with both a task and spec collector:

```go
package main

import (
	"context"
	"fmt"
	"runtime"
	"github.com/kpetremann/jackadi/sdk"
)

// Task options structure
type GreetOptions struct {
	Name string
	Age  int
}

// Required SetDefaults method
func (o *GreetOptions) SetDefaults() {
	o.Name = "Guest"
	o.Age = 30
}

// Task function
func GreetTask(ctx context.Context, options *GreetOptions, greeting string) (string, error) {
	return fmt.Sprintf("%s, %s! You are %d years old.", greeting, options.Name, options.Age), nil
}

// Spec collector function
func SystemInfoCollector(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{
		"os":           runtime.GOOS,
		"architecture": runtime.GOARCH,
		"cpu_cores":    runtime.NumCPU(),
	}, nil
}

func main() {
	// Create plugin plugin
	plugin := sdk.New("my-plugin")

	// Register task
	plugin.MustRegisterTask("greet", GreetTask).
		WithSummary("Greets a person").
		WithLockMode(sdk.NoLock)

	// Register spec collector
	plugin.MustRegisterSpecCollector("system", SystemInfoCollector)

	// Serve the plugin
	sdk.MustServe(plugin)
}
```

## Core concepts

### Tasks

Tasks are functions that can be invoked on nodes via CLI or API*. They support:
* Optional context parameter for cancellation.
* Optional options struct for configuration.
* Positional arguments.
* Return any serializable type plus error.

{{< callout type="important" >}}
*API is not yet implemented.
{{< /callout >}}

### Spec collectors

Spec collectors automatically gather system information:
* Run periodically in the background.
* Return structured data for querying.
* Enable intelligent task targeting.
* Must follow signature: `func(context.Context) (ReturnType, error)`.

### Lock modes

Tasks can specify default concurrency behavior:
* `sdk.NoLock` - Allow concurrent execution (default).
* `sdk.WriteLock` - Single writer, multiple readers.
* `sdk.ExclusiveLock` - Exclusive access, blocks all other tasks.

See [Lock Modes](../../lock_modes/) for detailed information.

## Building and deployment

### Building
```sh
CGO_ENABLED=0 go build -o my-plugin main.go
```

### Deploying

1. Place binary in manager's plugin directory
2. Configure distribution in `plugins.yaml`:
   ```yaml
   "*":                # All nodes
     - my-plugin
   "worker-*":         # Pattern-matched nodes
     - worker-plugin
   ```
3. Sync to nodes:
   ```sh
   jack run node1 plugins.sync
   ```

### Using
```sh
# Execute task with options
jack run node1 my-plugin.greet --name="World" "Hello"

# Query spec data
jack run node1 specs.get my-plugin.system.os
```

## Built-in plugin features

All plugin binaries can be run manually with flags. The plugin won’t start, but the flags can be used to retrieve details about the plugin, such as its version.

### Version information
```sh
./my-plugin --version
# Output: Version, commit, build time, Go version
```

### Task helper

```sh
./my-plugin --describe
# Output: List of tasks with summaries and options
```

### Execute plugin as a standalone binary

You can execute plugins as standalone binary directly from the shell.

The syntax is similar to `jack` command:

```sh {filename="simple task"}
./my-plugin run task hello
"Hello, World! This is Jackadi distributed task execution."
```

More details can be found in [standalone executable](../standalone_executable).

### Jackadi tag

Similar to `json` tag, `jackadi` tag enables you to define an alias for a key.

It is supported for:
- `Options` input
- any output structure of your tasks

More details can be found in [tags](../tags).

### Cross-plugin usage

Direct cross-calling is not possible. Instead, if you want to create a workflow with tasks from another plugin, just import it as a library.

```go
import "github.com/jackardi-io/other-plugin/tasks"

func MyTask() (Result, error) {
    // Reuse functionality from another plugin:
    r, err := tasks.Func1()
    r2, err2 :=  tasks.Func2()
    // ...
}
```

## Best practices

1. **Error Handling**: Return meaningful error messages.
2. **Context Awareness**: Honor context cancellation.
3. **Documentation**: Provide clear summaries and descriptions.
4. **Naming**: Use descriptive names.
5. **Lock Modes**: Choose appropriate default lock modes.
