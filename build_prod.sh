#!/bin/bash
echo "Building for Windows with stripped symbols..."
wails build -platform windows/amd64 -ldflags "-s -w" -o lhcontrol.exe
echo "Build complete. Check build/bin/lhcontrol.exe"
