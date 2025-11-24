#!/bin/bash
export PATH=$PATH:$(go env GOPATH)/bin

echo "Building for Windows with stripped symbols, trimpath, and obfuscation..."

# Check if garble is installed
if ! command -v garble &> /dev/null; then
    echo "garble not found. Installing..."
    go install mvdan.cc/garble@latest
fi

# Build with obfuscation
# -trimpath: removes file system paths
# -obfuscated: uses garble to obfuscate the binary
# -ldflags "-s -w": strips debug symbols (wails does this automatically with -obfuscated usually, but we keep it to be safe)
wails build -platform windows/amd64 -trimpath -obfuscated -ldflags "-s -w" -o lhcontrol.exe

echo "Build complete. Check build/bin/lhcontrol.exe"
