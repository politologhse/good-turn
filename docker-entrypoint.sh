#!/bin/sh
set -e

CONNECT="${CONNECT_ADDR:?CONNECT_ADDR is required (e.g. 127.0.0.1:443 for local Hysteria2)}"

exec ./good-turn-server -listen 0.0.0.0:56000 -connect "$CONNECT"
