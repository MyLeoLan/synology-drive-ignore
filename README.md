# Synology Drive Watcher (Go Version)

A lightweight, efficient macOS service to automatically manage ignored directories (like `node_modules`, `venv`, `target`) in Synology Drive.

## Why this exists

Synology Drive on macOS does not provide an easy way to globally ignore specific directory names (like `node_modules`) across all synchronization tasks. This often leads to massive resource usage and slow syncing as thousands of dependency files are uploaded.

This tool solves this by:
1.  **Monitoring** your Synology Drive configuration files (`blacklist.filter` and `filter-v4150`).
2.  **Automatically restoring** your ignore rules if they are overwritten or missing.
3.  **Smart Restarting** the Synology Drive Client to apply changes immediately.

## Features

- **Zero Config**: Works out of the box for common developer folders:
  - Node.js: `node_modules`, `.next`, `.nuxt`
  - Python: `venv`, `.venv`, `__pycache__`
  - Go/Rust: `vendor`, `target`
  - Mobile: `Pods`, `.gradle`, `DerivedData`
- **Low Resource Usage**: Uses native macOS file system events (`fsnotify`), consuming negligible CPU/RAM.
- **Robust Shutdown**: Ensures Synology Drive is completely closed before writing config to prevent data corruption.
- **Native Notifications**: Sends macOS notifications when rules are applied or the app restarts.

## Installation

### Method 1: Automatic Install (Recommended)

1.  Clone this repository or download the release.
2.  Run the installation script:

```bash
chmod +x install.sh
./install.sh
```

This will:
- Check for the binary (or tell you to build it).
- Register the application as a **macOS Launch Agent**.
- Start the service immediately and enable it on login.

### Method 2: Manual Run

You can simply run the binary in your terminal to test it:

```bash
./drive-conf-watch
```

## Customization

To add your own directories to the ignore list:

1.  Open `main.go`.
2.  Edit the `defaultIgnoreDirs` list at the top of the file:

```go
var defaultIgnoreDirs = []string{
    // ... existing ...
    "my-custom-folder",
    "another-ignored-dir",
}
```

3.  Rebuild the binary (see "Building" below).
4.  Restart the service:

```bash
launchctl unload ~/Library/LaunchAgents/com.user.synologywatch.plist
launchctl load ~/Library/LaunchAgents/com.user.synologywatch.plist
```

## Building

This project is written in Go. You need Go installed (1.20+ recommended).

### Build for current machine

```bash
go build -o drive-conf-watch
```

### Cross-Compile (e.g. for release)

**For Apple Silicon (M1/M2/M3):**
```bash
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o drive-conf-watch-darwin-arm64
```

**For Intel Mac:**
```bash
GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o drive-conf-watch-darwin-amd64
```

## CI/CD (GitHub Actions)

This repository includes a GitHub Actions workflow (`.github/workflows/build.yml`) that automatically builds binaries for both macOS architectures (amd64 and arm64) whenever you push a new tag (e.g., `v1.0.0`) or push to the `main` branch.

## Credits

Based on the original Python implementation, rewritten in Go for performance and reliability.
