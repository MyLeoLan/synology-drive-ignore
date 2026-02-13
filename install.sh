#!/bin/bash

# Configuration
REPO="MyLeoLan/synology-drive-ignore"
APP_NAME="drive-conf-watch"
PLIST_NAME="com.user.synologywatch.plist"
INSTALL_DIR="$HOME/.local/bin"
BIN_PATH="$INSTALL_DIR/$APP_NAME"
PLIST_PATH="$HOME/Library/LaunchAgents/$PLIST_NAME"

# Function to uninstall
uninstall() {
    echo "Uninstalling $APP_NAME..."
    
    # 1. Unload service
    if launchctl list | grep -q "$PLIST_NAME"; then
        echo "Stopping service..."
        launchctl unload "$PLIST_PATH" 2>/dev/null
    fi
    
    # 2. Remove plist
    if [ -f "$PLIST_PATH" ]; then
        echo "Removing plist..."
        rm "$PLIST_PATH"
    fi
    
    # 3. Remove binary
    if [ -f "$BIN_PATH" ]; then
        echo "Removing binary..."
        rm "$BIN_PATH"
    fi
    
    echo "Uninstallation complete."
}

# Check for uninstall flag
if [ "$1" == "--uninstall" ] || [ "$1" == "uninstall" ]; then
    uninstall
    exit 0
fi

# Determine Architecture
ARCH=$(uname -m)
if [ "$ARCH" == "x86_64" ]; then
    BINARY_NAME="drive-conf-watch-darwin-amd64"
elif [ "$ARCH" == "arm64" ]; then
    BINARY_NAME="drive-conf-watch-darwin-arm64"
else
    echo "Error: Unsupported architecture: $ARCH"
    exit 1
fi

echo "Detected architecture: $ARCH"
echo "Downloading latest release from GitHub..."

# Create install clean directory
mkdir -p "$INSTALL_DIR"

# Download latest release
DOWNLOAD_URL="https://github.com/$REPO/releases/latest/download/$BINARY_NAME"
echo "Downloading from: $DOWNLOAD_URL"

if curl -L --fail "$DOWNLOAD_URL" -o "$BIN_PATH"; then
    echo "Download successful."
else
    echo "Error: Failed to download binary. Please check if a release exists on GitHub."
    exit 1
fi

chmod +x "$BIN_PATH"
echo "Installed to $BIN_PATH"

echo "Configuring Launch Agent..."

# Create Plist file
cat > "$PLIST_PATH" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.user.synologywatch</string>
    <key>ProgramArguments</key>
    <array>
        <string>$BIN_PATH</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/synology-watch.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/synology-watch.err</string>
</dict>
</plist>
EOF

echo "Created plist at $PLIST_PATH"

# Unload if exists (ignore error)
launchctl unload "$PLIST_PATH" 2>/dev/null

# Load the service
if launchctl load "$PLIST_PATH"; then
    echo "Service loaded successfully! The application is running in the background."
    echo "To uninstall, run this script with --uninstall"
else
    echo "Failed to load service."
    exit 1
fi
