#!/usr/bin/env bash
set -euo pipefail

# Start N nodes with default tolerance (do NOT pass -t).
# Usage: ./start_cluster.sh [N]
# Example: ./start_cluster.sh 5

N="${1:-3}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$SCRIPT_DIR/server"

if [[ ! -d "$SERVER_DIR" ]]; then
  echo "Error: server directory not found: $SERVER_DIR" >&2
  exit 1
fi

if command -v wslpath >/dev/null 2>&1; then
  SERVER_DIR_WIN="$(wslpath -w "$SERVER_DIR")"
else
  SERVER_DIR_WIN="$SERVER_DIR"
fi

echo "Starting cluster with n=$N (default t=floor((n-1)/2))"
for ((i=0; i<N; i++)); do
  gateway_flag=""
  if [[ "$i" == "0" ]]; then
    gateway_flag="-gateway"
  fi

  powershell.exe -NoProfile -Command "Start-Process powershell -ArgumentList '-NoExit','-Command','Set-Location ''$SERVER_DIR_WIN''; go run . -id=$i -n=$N $gateway_flag'" >/dev/null
  sleep 0.2
done

echo "Started $N node terminal(s)."
