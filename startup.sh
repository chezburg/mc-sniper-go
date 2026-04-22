#!/bin/bash
set -e

echo "[STARTUP] Running dry-test..."

if ./mcsnipergo --dry-test; then
    echo "[STARTUP] Dry-test passed, starting main program..."
    exec ./mcsnipergo "$@"
else
    echo "[STARTUP] Dry-test FAILED!"
    echo "[STARTUP] Container unhealthy - either VPN or accounts are not functioning"
    exit 1
fi