#!/bin/sh
set -e

CONNECT="${CONNECT_ADDR:?CONNECT_ADDR is required (e.g. 127.0.0.1:443 for local Hysteria2)}"
LISTEN="${LISTEN_ADDR:-0.0.0.0:56000}"
HEALTH="${HEALTH_ADDR:-}"

if [ -n "$HEALTH" ]; then
  exec ./good-turn-server -listen "$LISTEN" -connect "$CONNECT" -health-addr "$HEALTH"
else
  exec ./good-turn-server -listen "$LISTEN" -connect "$CONNECT"
fi
