---
title: 'Plugins'
weight: 4
---

## Built-ins

Jackadi comes up with few built-in plugins:
* `cmd`: tasks to run shell commands.
* `health`: tasks to check the health of an node (ping).
* `plugins`: manage and get info about plugins.
* `specs`: list and get specs.

More details can be found [here](/docs/advanced_guides/builtin_plugins).

## Creating a plugin

A plugin can serve both tasks and spec collectors.

### Creating a task

A task can be a simple operation like creating a systemd service, or a more complex workflow.

It is a function having a simple signature:
1. **Context Parameter** (optional): If present, must be the first parameter. Allows for cancellation and timeout handling.
2. **Options Parameter** (optional): If present, must be the first parameter (if no context) or second parameter (if context is present). A pointer to a struct that implements the `Options` interface.
3. **Positional Arguments** (optional): Zero or more arguments for positional CLI parameters.
4. **Return Values**: Always returns two values - a result of any serializable type and an error.

Example of a plugin serving one plugin which has one task:
```go {filename="demo.go"}
package main

import "github.com/kpetremann/jackadi/sdk"

func Hello() (string, error){
	return "hello world!", nil
}

func main() {
	plugin := sdk.New("demo")
	plugin.MustRegisterTask("hello", Hello).WithSummary("Simple hello world")
	sdk.MustServe(plugin)
}
```

Usage:
```sh {filename="command"}
jack run <node> demo.hello
```

For more details, check out the advanced [task writing guide](TODO).

### Creating spec collectors

Spec collectors are function returning information about the asset (e.g. hardware specs, resource usage, OS version etc...).

Their signature is similar to tasks:
1. **Context Parameter** (optional): Used for cancellation and timeout handling.
2. **Return Values**: Always returns two values:
   - A data structure containing the collected information.
   - An error value (or nil if successful).

```go {filename="demo.go"}
package main

import "github.com/kpetremann/jackadi/sdk"

func SystemInfoCollector() (map[string]string, error){
	info := map[string]string{
		{"distribution": "opensuse"},
		{"version": "tumbleweed"},
		{"kernel": "6.16"},
	}
	return info, nil
}

func main() {
	plugin := sdk.New("demo")
	plugin.MustRegisterSpecCollector("system-info", SystemInfoCollector)
	sdk.MustServe(plugin)
}
```

Once declared, they are regularly collected by the node (every 60s by default).

Usage:
```sh {filename="commands"}
jack run <node> specs.list
jack run <node> specs.all

# we can get all specs provided system-info
jack run <node> specs.get system-info
# or get a specific field
jack run <node> specs.get system-info.distribution
```
