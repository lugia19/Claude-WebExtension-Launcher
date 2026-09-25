#!/bin/bash

# Uninstall.command
# Removes the patched Claude WebExtension Launcher installation
# Double-click this file on macOS to run it.

INSTALL_DIR="$HOME/Library/Application Support/Claude WebExtension Launcher"
LAUNCHER_APP="$HOME/Applications/Claude_WebExtension_Launcher.app"

if [ ! -d "$INSTALL_DIR" ] && [ ! -d "$LAUNCHER_APP" ]; then
    echo "Nothing to uninstall - neither the launcher nor the patched Claude is installed."
    echo ""
    read -p "Press Enter to exit..."
    exit 0
fi

echo ""
echo "=== Claude WebExtension Launcher - Uninstall ==="
echo ""
echo "This will remove the patched Claude Desktop installation at:"
echo "  $INSTALL_DIR"
echo ""
echo "Your conversation data will NOT be deleted."
echo ""

read -p "Are you sure? (Y/N): " CONFIRM
if [ "$CONFIRM" != "Y" ] && [ "$CONFIRM" != "y" ]; then
    echo "Cancelled."
    read -p "Press Enter to exit..."
    exit 0
fi

echo ""
echo "Removing the installed launcher..."
rm -rf "$LAUNCHER_APP"

echo "Removing $INSTALL_DIR..."
rm -rf "$INSTALL_DIR"

if [ ! -d "$INSTALL_DIR" ]; then
    echo ""
    echo "Uninstall complete."
else
    echo ""
    echo "ERROR: Failed to remove install directory."
fi

echo ""
read -p "Press Enter to exit..."
exit 0
