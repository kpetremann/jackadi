---
title: 'Manual installation'
weight: 10
---

## Prerequisites

Before installing Jackadi, ensure you have:
* A supported operating system (Linux, macOS, Windows).
* Network connectivity between manager and nodes.
* Appropriate permissions to install binaries and create directories.

## Installing binaries

### From pre-built releases

Download the appropriate binaries for your platform from the releases page:

1. Download the latest release for your platform.
2. Extract the archive.
3. Move the binaries to a location in your PATH.

```sh
# Example for Linux amd64
tar xzf jackadi_linux_amd64.tar.gz
sudo mv manager node jack /usr/local/bin/
```

### Verifying installation

After installation, verify the components are working:

```sh
# Check versions
manager --version
node --version
jack --version

# Verify help is available
jack --help
```

### From source (advanced)

To build Jackadi from source, you need Go 1.24 or later:

```sh
# Clone the repository
git clone https://github.com/kpetremann/jackadi.git
cd jackadi

# Build the components
make build

# Alternatively, build them individually
go build -o manager ./cmd/manager
go build -o node ./cmd/node
go build -o jack ./cmd/jack
```

## Directory structure

Jackadi uses several directories for configuration, plugins, and data:

### Default directories

**Manager directories:**
* `/etc/jackadi/` - Configuration files
* `/opt/jackadi/plugins/` - Plugin binaries
* `/var/lib/jackadi/` - Data and state

**Node directories:**
* `/etc/jackadi/` - Configuration files
* `/var/lib/jackadi/plugins/` - Downloaded plugins
* `/var/log/jackadi/` - Log files

### Creating directories

```sh
# Create system directories (requires sudo)
sudo mkdir -p /etc/jackadi /opt/jackadi/plugins /var/lib/jackadi
sudo mkdir -p /var/lib/jackadi/plugins /var/log/jackadi

# Set appropriate permissions
sudo chown -R jackadi:jackadi /var/lib/jackadi /var/log/jackadi
sudo chmod 755 /etc/jackadi /opt/jackadi/plugins
```

## Setting up TLS certificates (production)

For production environments, you should use mTLS (mutual TLS) for secure communication between all components. This section provides a quick overview - see [Security Setup](../security/) for detailed instructions.

### Quick certificate setup

1. Generate a CA certificate
```sh
openssl genrsa -out ca.key 4096
openssl req -new -x509 -days 365 -key ca.key -out ca.crt
```

2. Generate Manager Certificate
```sh
openssl genrsa -out manager.key 4096
openssl req -new -key manager.key -out manager.csr
openssl x509 -req -days 365 -in manager.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out manager.crt
```

3. Generate Node Certificate
```sh
openssl genrsa -out node.key 4096
openssl req -new -key node.key -out node.csr
openssl x509 -req -days 365 -in node.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out node.crt
```

### Certificate distribution

After generating certificates, distribute them to appropriate systems:

```sh
# Copy certificates to manager system
sudo cp ca.crt manager.crt manager.key /etc/jackadi/certs/

# Copy certificates to node systems
sudo cp ca.crt node.crt node.key /etc/jackadi/certs/

# Set proper permissions
sudo chmod 600 /etc/jackadi/certs/*.key
sudo chmod 644 /etc/jackadi/certs/*.crt
```

## Post-installation steps

### 1. User and service setup

Create a dedicated user for running Jackadi services:

```sh
# Create jackadi user
sudo useradd --system --home-dir /var/lib/jackadi --shell /bin/false jackadi

# Set directory ownership
sudo chown -R jackadi:jackadi /var/lib/jackadi /var/log/jackadi
```

### 2. Systemd services (Linux)

Create systemd service files for automatic startup:

**Manager service (`/etc/systemd/system/jackadi-manager.service`):**
```ini
[Unit]
Description=Jackadi Manager
After=network.target

[Service]
Type=simple
User=jackadi
ExecStart=/usr/local/bin/manager --config /etc/jackadi/manager.yaml
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

**Node service (`/etc/systemd/system/jackadi-node.service`):**
```ini
[Unit]
Description=Jackadi Node
After=network.target

[Service]
Type=simple
User=jackadi
ExecStart=/usr/local/bin/node --config /etc/jackadi/node.yaml
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start services:
```sh
sudo systemctl enable jackadi-manager jackadi-node
sudo systemctl start jackadi-manager jackadi-node
```

### 3. Verify installation

Test basic connectivity:

```sh
# Test CLI connectivity
jack node list

# Test basic task execution
jack run node-name health.ping
```
