---
title: 'Introduction'
weight: 1
---

> [!WARNING]  
> Jackadi is currently in an alpha.
>
> You're welcome to try it out and give feedback.

## What is Jackadi?

Jackadi is a developer-first distributed task execution platform designed for developers with a plugin system. Jackadi is client/server based.

The main motivation is to create a framework where developers write tasks as pure code without abstractions or hidden behaviors. Task writing is meant to be natural and direct.

Key principles:
* **Pure Go Approach**: Tasks are written as Go code with no hidden behaviors - what you write is what you get.
* **No Runtime Dependencies**: Tasks have no runtime dependencies on other tasks; all dependencies are resolved at compile-time.
* **No Abstractions**: Task writing is natural for Go developers with minimal framework-specific knowledge needed.
* **Flexible Use Cases**: From simple package installation to complex workflows like server management and upgrades.
* **No locking**: Plugins are libraries which can be imported from any other program. They are standalone binaries, meaning they can be executed directly from the shell.

## Features

| | Description |
|---------|-------------|
| **Distributed Task Execution** | Execute tasks across multiple nodes from a central manager. |
| **Plugin System**              | Extend functionality through custom Go plugins.<br>Bonus: a plugin can be executed as a standalone binary. |
| **Advanced Targeting**         | Target nodes via list, glob, regex, advanced query. |
| **Specs Collectors**           | Gather and store system information from nodes. |
| **Security**                   | mTLS, node acceptance workflow, protection against rogue nodes. |
| **Developer-Friendly**         | Tasks/specs are Go function registered with a simple SDK. |
| **Web API	Integrate**          | Jackadi with your infrastructure stack. |

## Architecture

<img src="/images/jackadi-overview.svg" />

Nodes are Jackadi clients installed on the nodes. They are connected to a manager via persistent bidirectional gRPC connections.

Jackadi features a simple plugin architecture where all tasks and specs collectors are implemented as pure Go functions. The plugin system is built on top of [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin/), and is made intuitive using a simple and easy to use SDK.

By default, all executed task are recorded in [BadgerDB](https://github.com/hypermodeinc/badger).

## Quick demo tour

### Quickstart

```sh
# Start the manager
manager --mtls=false

# Start an node
node --id="node1" --mtls=false

# Accept the node connection (if not using auto-accept)
jack nodes list
jack nodes accept node1

# The node should be now in "accepted" list
jack nodes list

# Check nodes health
jack nodes health

# Run a task
jack run node1 cmd.run "echo hello"
```

### Write my first plugin

{{% steps %}}

#### Create a Go project

```go {filename=tour.go}
package main

import "github.com/kpetremann/jackadi/sdk"

func Hello(name string) (string, error) {
	return fmt.Sprintf("Hello %s!", name), nil
}

func main() {
	tour := sdk.New("tour")
	tour.MustRegisterTask("hello", Hello).WithDescription("Greetings.")
	sdk.MustServe(tour)
}
```

#### Compile the plugin

```sh
CGO_ENABLED=0 go build -o tour .
```
#### Put the plugin in the manager

Copy the file in the manager `/opt/jackadi/plugins` directory.

Then configure `/etc/jackadi/plugins.yaml` file:

```sh
"*":
  - tour
```

#### Synchronize the plugin to the node
```sh
jack run node1 plugins.sync
```

#### Run the plugin
```sh
jack run node1 tour.hello
```

{{% /steps %}}

## Next step

{{< cards >}}
  {{< card link="basic_guides" title="Basic guides" icon="book-open" >}}
{{< /cards >}}
