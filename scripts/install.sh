#!/bin/sh
# Installs mmt into ~/.local/bin.
set -e
dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
mkdir -p "$HOME/.local/bin"
# remove first: copying over a signed binary that macOS has already run gets
# the new one killed on launch
rm -f "$HOME/.local/bin/mmt"
cp "$dir/mmt" "$HOME/.local/bin/mmt"
chmod +x "$HOME/.local/bin/mmt"
# downloads from a browser or chat app are quarantined; this build is not notarized
if [ "$(uname)" = Darwin ]; then xattr -d com.apple.quarantine "$HOME/.local/bin/mmt" 2>/dev/null || true; fi
echo "Installed: $("$HOME/.local/bin/mmt" --version)"
case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) echo "Add to your shell profile: export PATH=\"\$HOME/.local/bin:\$PATH\"";; esac
echo "Next: mmt login"
