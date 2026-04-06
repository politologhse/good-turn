#!/bin/bash
set -e

# Good TURN + Hysteria2 server setup
# Usage: curl -fsSL <url>/setup-server.sh | bash -s -- -pass <password> [-port 56000] [-hy-port 443] [-sni hy2]

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

PASS=""
PORT=56000
HY_PORT=443
SNI="hy2"

while [[ $# -gt 0 ]]; do
  case $1 in
    -pass)   PASS="$2"; shift 2;;
    -port)   PORT="$2"; shift 2;;
    -hy-port) HY_PORT="$2"; shift 2;;
    -sni)    SNI="$2"; shift 2;;
    *) echo -e "${RED}Unknown flag: $1${NC}"; exit 1;;
  esac
done

if [ -z "$PASS" ]; then
  echo -e "${RED}Usage: $0 -pass <hysteria2-password> [-port 56000] [-hy-port 443] [-sni hy2]${NC}"
  exit 1
fi

MY_IP=$(curl -4 -s --connect-timeout 5 ifconfig.me || curl -4 -s --connect-timeout 5 icanhazip.com)
if [ -z "$MY_IP" ]; then
  echo -e "${RED}Cannot detect public IP${NC}"
  exit 1
fi

echo -e "${GREEN}=== Good TURN Server Setup ===${NC}"
echo -e "IP:       ${YELLOW}${MY_IP}${NC}"
echo -e "TURN:     ${YELLOW}:${PORT}${NC}"
echo -e "Hysteria: ${YELLOW}:${HY_PORT}${NC}"
echo ""

# --- 1. Install Hysteria2 ---
echo -e "${GREEN}[1/4] Installing Hysteria2...${NC}"
if command -v hysteria &>/dev/null; then
  echo "  Already installed: $(hysteria version 2>/dev/null | head -1)"
else
  bash <(curl -fsSL https://get.hy2.sh/) 2>/dev/null
fi

# --- 2. Configure Hysteria2 ---
echo -e "${GREEN}[2/4] Configuring Hysteria2...${NC}"
mkdir -p /etc/hysteria

# Self-signed cert
if [ ! -f /etc/hysteria/key.pem ]; then
  openssl req -x509 -nodes -newkey ec:<(openssl ecparam -name prime256v1) \
    -keyout /etc/hysteria/key.pem -out /etc/hysteria/cert.pem \
    -subj "/CN=${SNI}" -days 3650 \
    -addext "subjectAltName=DNS:${SNI}" \
    -addext "basicConstraints=CA:FALSE" \
    -addext "keyUsage=digitalSignature,keyEncipherment" \
    -addext "extendedKeyUsage=serverAuth" 2>/dev/null
  chown root:hysteria /etc/hysteria/key.pem
  chmod 640 /etc/hysteria/key.pem
  echo "  Generated self-signed cert"
else
  echo "  Cert already exists"
fi

cat > /etc/hysteria/config.yaml <<EOF
listen: 127.0.0.1:${HY_PORT}

tls:
  cert: /etc/hysteria/cert.pem
  key: /etc/hysteria/key.pem

auth:
  type: password
  password: ${PASS}

masquerade:
  type: proxy
  proxy:
    url: https://news.ycombinator.com/
    rewriteHost: true
EOF

systemctl enable hysteria-server 2>/dev/null || true
systemctl restart hysteria-server
echo "  Hysteria2 running on 127.0.0.1:${HY_PORT}"

# --- 3. Install Good TURN server ---
echo -e "${GREEN}[3/4] Installing Good TURN server...${NC}"

ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  GOARCH="amd64";;
  aarch64) GOARCH="arm64";;
  armv7l)  GOARCH="arm";;
  *) echo -e "${RED}Unsupported arch: $ARCH${NC}"; exit 1;;
esac

# Try downloading pre-built binary from GitHub Releases
RELEASE_URL="https://github.com/politologhse/good-turn/releases/latest/download/server-linux-${GOARCH}"
if curl -fSL -o /usr/local/bin/good-turn-server "$RELEASE_URL" 2>/dev/null; then
  chmod +x /usr/local/bin/good-turn-server
  echo "  Downloaded from GitHub Releases"
elif command -v go &>/dev/null; then
  echo "  Download failed, building from source (pinned to latest tag)..."
  TMP=$(mktemp -d)
  cd "$TMP"
  LATEST_TAG=$(curl -fsSL https://api.github.com/repos/politologhse/good-turn/releases/latest 2>/dev/null | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
  if [ -z "$LATEST_TAG" ]; then
    echo -e "${RED}Cannot determine latest release tag. Aborting source build.${NC}"
    echo "  Download the server binary manually from:"
    echo "  https://github.com/politologhse/good-turn/releases"
    exit 1
  fi
  git clone --depth 1 --branch "$LATEST_TAG" https://github.com/politologhse/good-turn.git 2>/dev/null
  cd good-turn
  echo "  Building $LATEST_TAG..."
  CGO_ENABLED=0 go build -ldflags="-s -w" -o /usr/local/bin/good-turn-server ./server
  cd /
  rm -rf "$TMP"
else
  echo -e "${RED}Cannot download binary or build from source (Go not installed)${NC}"
  echo "  Install Go or download the binary manually from:"
  echo "  https://github.com/politologhse/good-turn/releases"
  exit 1
fi

# --- 4. Systemd service ---
echo -e "${GREEN}[4/4] Creating systemd service...${NC}"

cat > /etc/systemd/system/good-turn.service <<EOF
[Unit]
Description=Good TURN Server
After=network.target hysteria-server.service

[Service]
ExecStart=/usr/local/bin/good-turn-server -listen 0.0.0.0:${PORT} -connect 127.0.0.1:${HY_PORT}
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable good-turn
systemctl restart good-turn

echo ""
echo -e "${GREEN}=== Done! ===${NC}"
echo ""

# Generate config string (pass via env to avoid ps aux leak)
CONFIG=$(GT_PASS="${PASS}" /usr/local/bin/good-turn-server -generate-config -addr "${MY_IP}:${PORT}" -sni "${SNI}")

echo -e "Config string for client app:"
echo -e "${YELLOW}${CONFIG}${NC}"
echo ""
echo -e "Or manually:"
echo -e "  Server:   ${YELLOW}${MY_IP}:${PORT}${NC}"
echo -e "  Password: ${YELLOW}${PASS}${NC}"
echo -e "  SNI:      ${YELLOW}${SNI}${NC}"
echo ""
echo -e "Status:"
echo -e "  systemctl status good-turn"
echo -e "  systemctl status hysteria-server"
