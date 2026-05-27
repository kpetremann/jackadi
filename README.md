<div align="center">
  <picture style="max-width: 80%" >
    <source media="(prefers-color-scheme: dark)"  srcset="./assets/jackadi-banner-dark.png">
    <img alt="Jackadi logo" src="./assets/jackadi-banner.png" style="width: 80%">
  </picture>
  <h3 align="center">Developer-first automation platform</h3>
</div>

# Jackadi

[![Status](https://img.shields.io/badge/status-alpha-bue)](https://github.com/kpetremann/jackadi)

> [!WARNING]  
> Jackadi is currently in an alpha.
> You're welcome to try it out and give feedback.

## What is Jackadi?

Jackadi is a developer-first distributed task execution platform designed for developers with a plugin system. Jackadi is client/server based.

The main motivation is to create a framework where developers write tasks as pure code without abstractions or hidden behaviors. Task writing is meant to be natural and direct.

Key principles:
* **Pure Go Approach**: Tasks are written as Go code with no hidden behaviors - what you write is what you get.
* **No Runtime Dependencies**: Tasks have no runtime dependencies on other tasks; all dependencies are resolved at compile-time.
* **No Abstractions**: Task writing is natural for Go developers with minimal framework-specific knowledge needed.
* **Flexible Use Cases**: From simple package installation to complex workflows like server management and upgrades.

## Features

| Feature | Description |
|---------|-------------|
| **Distributed Task Execution** | Execute tasks across multiple nodes from a central manager. |
| **Plugin System**              | Extend functionality through custom Go plugins. |
| **Advanced Targeting**         | Target nodes via list, glob, regex, advanced query. |
| **Specs Collection**           | Gather and store system information from nodes. |
| **Security**                   | mTLS, node acceptance workflow, protection against rogue nodes. |
| **Developer-Friendly**         | Tasks/specs are Go function registered with a simple SDK. |
| **Web API**                    | Integrate Jackadi with your infrastructure stack. |

## Documentation

Full documentation can be found [here](https://jackadi.io/docs/).

## Architecture

![architecture](./assets/jackadi-overview.svg)

In a nutshell:
* Nodes are connected to a manager via persistent bidirectional gRPC.
* Simple plugin system:
  * All tasks and specs collectors are pure Go functions.
  * The plugin system is based on [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin/).
  * The SDK is simple and easy to use.
* Tasks results are stored in a local [BadgerDB](https://github.com/hypermodeinc/badger).

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
