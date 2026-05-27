---
title: 'Writing tasks'
weight: 2
---

## What are tasks?

Tasks are the primary way to add executable functionality to Jackadi. Each task:
* Can be triggered manually via the CLI or API*.
* Has defined inputs/outputs.
* Executes on nodes.
* Can implement complex workflows (like setting up a webserver).
* Can reuse or alias functions from other plugins.

{{< callout type="important" >}}
*API is not yet implemented.
{{< /callout >}}

## Task function structure

A task in Jackadi is a Go function that follows a flexible signature pattern:

```go
func Task([ctx context.Context], [opts *OptionsType], [arg <type>...]) (<type>, error)
```

Components:
1. **Context Parameter** (optional):
    * If present, must be the first parameter.
    * Allows for cancellation and timeout handling.
2. **Options Parameter** (optional):
    * If present, must be the first parameter (if no context) or second parameter (if context is present).
    * Allows for passing key-value pairs.
    * Must be a pointer to a struct that implements the [Options interface]({{<ref "#options-interface" >}}).
3. **Optional Positional Arguments**: Zero or more arguments for positional CLI parameters.
4. **Return Values**:
    * Any task must return two values: `(<type>, error)`.
    * `<type>` must be [serializable]({{<ref "#supported-inputoutput-data-types" >}}).

## Writing a task

1. Import `github.com/kpetremann/jackadi/sdk`:
```go
package main

import "github.com/kpetremann/jackadi/sdk"
````
2. If needed, define `Options` implementing `sdk.Options`:
```go
// Define an options structure
type GreetOptions struct {
	Name string
	Age  int
}

// Implement SetDefaults method (required by Options interface)
func (o *GreetOptions) SetDefaults() {
	o.Name = "Guest"
	o.Age = 30
}
```
3. Create a task function:
```go
func GreetTask(ctx context.Context, options *GreetOptions, greeting string) (string, error) {
	return fmt.Sprintf("%s, %s! You are %d years old.", greeting, options.Name, options.Age), nil
}
```
4. Register a task and serve:
```go
plugin := sdk.New("my-plugin")

// Basic registration
plugin.MustRegisterTask("task-name", MyTaskFunction)

// With additional metadata
plugin.MustRegisterTask("advanced-task", MyAdvancedFunction).
	WithSummary("Short summary").
	WithLockMode(sdk.WriteLock).
	WithDescription("Longer description of what the task does").
	WithArg("argName", "argType", "defaultValue").
	WithFlags(sdk.Deprecated) // Optional flags like Deprecated or NotImplemented

sdk.MustServe(plugin)
```

{{% details title="Full example" closed="true" %}}

```go
package main

import (
	"context"
	"fmt"
	"github.com/kpetremann/jackadi/sdk"
)

// Define an options structure
type GreetOptions struct {
	Name string
	Age  int
}

// Implement SetDefaults method (required by Options interface)
func (o *GreetOptions) SetDefaults() {
	o.Name = "Guest"
	o.Age = 30
}

// Define the task function
func GreetTask(ctx context.Context, options *GreetOptions, greeting string) (string, error) {
	return fmt.Sprintf("%s, %s! You are %d years old.", greeting, options.Name, options.Age), nil
}

func main() {
	plugin := sdk.New("my-plugin")

	// Basic registration
	plugin.MustRegisterTask("task-name", MyTaskFunction)

	// With additional metadata
	plugin.MustRegisterTask("advanced-task", MyAdvancedFunction).
			WithSummary("Short summary").
			WithLockMode(sdk.WriteLock).
			WithDescription("Longer description of what the task does").
			WithArg("argName", "argType", "defaultValue").
			WithFlags(sdk.Deprecated) // Optional flags like Deprecated or NotImplemented

	sdk.MustServe(plugin)
}

```

{{% /details %}}

## Reference

### Options interface

Task options are defined as a struct that implements the `Options` interface:

```go
type Options interface {
	SetDefaults()
}
```

The options struct:
* Provides a structured way to pass parameters to tasks.
* Fields are automatically mapped from CLI flags (e.g., `--name="John"`).
* Must include a `SetDefaults()` method to set default values.
* Supports nested structures and various data types.

### Supported arguments, options and output types

* Basic types: string, int, bool, float, etc..
* Complex types: structs, maps, slices, arrays.
* Pointers to any of the above types.
* Custom types with proper JSON marshaling/unmarshaling.

### Reusing functions from other plugins

Import and wrap functions from other plugins:

```go
import "github.com/other-org/cert-plugin/tasks"

func SetupSSL(ctx context.Context, options *SSLOptions) (Result, error) {
	// Convert options and call external function
	certOptions := &tasks.CertOptions{
		Domain: options.Domain,
		Email:  options.Email,
	}

	return tasks.GenerateCert(ctx, certOptions)
}

func main() {
	// ...

	// This task reuse a task from an another plugin
	plugin.MustRegisterTask("ssl-setup", SetupSSL)
	// You can also export the task from an another plugin directly
	plugin.MustRegisterTask("cert-alias", tasks.GenerateCert)

	// ...
}
```

### Task metadata methods

When registering tasks, you can use several fluent methods to configure task metadata and behavior:

```go
plugin.MustRegisterTask("my-task", MyTaskFunction).
    WithSummary("Short description of the task").
    WithDescription("Longer description explaining what the task does and how to use it").
    WithArg("filename", "string", "config.json").
    WithArg("port", "int", "8080").
    WithFlags(sdk.Deprecated, sdk.NotImplemented).
    WithLockMode(sdk.WriteLock)
```


#### WithSummary

Command: **`WithSummary(text string)`**
* Sets a short description that appears in task listings.
* Should be concise and descriptive.

#### WithDescription

Command: **`WithDescription(text string)`**
* Sets a longer description with detailed usage information.
* Can include examples and detailed explanations.

#### WithArg

Command: **`WithArg(name, argType, example string)`**
* Documents positional arguments for the task.
* `name`: argument name for documentation.
* `argType`: type description (e.g., "string", "int", "[]string").
* `example`: example value to show users.

#### WithFlags

Command: **`WithFlags(flags ...Flag)`**
* Adds metadata flags to the task.
* Available flags: `sdk.Deprecated`, `sdk.NotImplemented`.
* Multiple flags can be specified.

#### WithLockMode

Command: **`WithLockMode(lockMode LockMode)`**
* Sets the default lock mode (see next section).

### Lock modes

When registering tasks, you can specify a default lock mode that controls how tasks execute concurrently:

```go
plugin.MustRegisterTask("get-status", GetStatusTask).
    WithLockMode(sdk.NoLock)
```

* **`sdk.NoLock`**: Default, useful for read-only operations.
* **`sdk.WriteLock`**: For tasks that modify system state but can safely run alongside read-only tasks (single writer pattern).
* **`sdk.ExclusiveLock`**: To be used carefully: usually for jackadi management tasks like plugin sync, it blocks every requests including specs refresh.

Users can always override your default lock mode via CLI:
```sh
# Use plugin's default lock mode
jack run node1 my-plugin.update-config

# Override with exclusive lock
jack run --lock-mode exclusive node1 my-plugin.update-config
```

## Best practices

1. **Error Handling**: Return meaningful error messages that help diagnose issues
2. **Context Awareness**: Honor context cancellation for graceful termination
3. **Documentation**: Use summary and description when registering tasks
4. **Lock Modes**: Specify appropriate default lock modes for your tasks
