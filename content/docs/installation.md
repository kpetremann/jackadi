---
title: 'Installation'
weight: 2
---

## Linux packages (recommended)

1. Download and install Jackadi. Installation files (`.deb` and `.rpm`) are available in the [GitHub releases](https://github.com/kpetremann/jackadi/releases).
2. Edit the configuration (see [Configuration](/docs/configuration) page).
3. Start the service and ensure it is running:
```sh
# for the manager
systemctl enable --now jackadi-manager
systemctl status jackadi-manager

# for the node
systemctl enable --now jackadi-node
systemctl status jackadi-node
```

## Manual installation

{{< callout type="warning" >}}
To install Jackadi manually, you will need to manually create all the directories to make it work (see [File Structure]({{<ref "#file-structure" >}}) section to get the list of directories to create).
{{< /callout >}}

{{< callout type="tips" >}}
More details can be found in [Manual installation guide](/docs/advanced_guides/manual_installation/).
{{< /callout >}}

### Install binaries

All the binaries can be found in [GitHub releases](https://github.com/kpetremann/jackadi/releases).

### Install from source

1. Clone the repository:
```sh
git clone https://github.com/kpetremann/jackadi.git && cd jackadi
```
2. Run the build:
```sh
make build
```
3. The three binaries can be found in `dist/` directory.

## Directory and files

Once everything is installed, you should have the following directory layout:

``` {filename="manager"}
/etc/jackadi/
└── manager.yaml
└── plugins.yaml
└── registry.json
└── tls/

/var/lib/jackadi/
└── database/

/opt/jackadi/
└── plugins/
    └── plugin1
    └── plugin2
```

``` {filename="node"}
/etc/jackadi/
└── node.yaml

/var/lib/jackadi/
└── database/

/opt/jackadi/
└── plugins/
    └── plugin1
    └── plugin2
```
