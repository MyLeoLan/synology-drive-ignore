#!/bin/bash

# Configuration
APP_NAME="drive-conf-watch"
PLIST_NAME="com.user.synologywatch.plist"
INSTALL_DIR="$(pwd)"
BIN_PATH="$INSTALL_DIR/$APP_NAME"
PLIST_PATH="$HOME/Library/LaunchAgents/$PLIST_NAME"

# Check if binary exists
if [ ! -f "$BIN_PATH" ]; then
    echo "Error: Binary '$APP_NAME' not found in current directory."
    echo "Please run 'go build -o $APP_NAME' first."
    exit 1
fi

echo "Installing $APP_NAME as a Launch Agent..."

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
    echo "Service loaded successfully!"
    echo "The application is now running in the background."
    echo "Logs can be viewed at: /tmp/synology-watch.log"
else
    echo "Failed to load service."
    exit 1
fi
