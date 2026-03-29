#!/usr/bin/env bash
set -euo pipefail

# Stop all Go node processes.
# Usage: ./stop_cluster.sh

powershell.exe -NoProfile -Command '$p = Get-Process go -ErrorAction SilentlyContinue; if ($p) { $c = ($p | Measure-Object).Count; $p | Stop-Process -Force; Write-Output ("Stopped " + $c + " go process(es).") } else { Write-Output "No go processes found." }'
