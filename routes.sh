#!/bin/bash
set -euo pipefail

gateway="$(ip -o -4 route show to default | awk '/via/ {print $3}' | head -1)"
if [[ -z "$gateway" ]]; then
  echo "Error: cannot detect default gateway" >&2
  exit 1
fi

while IFS= read -r line; do
  line="${line//$'\r'/}"
  # Extract IPv4 or relayed-address=IP:port
  remote="$(printf '%s\n' "$line" | sed -nE '
    s/.*relayed-address=(([0-9]{1,3}\.){3}[0-9]{1,3}):[0-9]+.*/\1/p
    t done
    s/^(([0-9]{1,3}\.){3}[0-9]{1,3}(\/[0-9]{1,2})?)$/\1/p
    :done
  ')"

  [[ -z "$remote" ]] && continue

  # ip route replace is idempotent — safe to re-run
  sudo ip route replace "$remote" via "$gateway"
  echo "Route: $remote via $gateway"
done
