#!/usr/bin/env bash
# LoomPanel Installer for macOS (Homebrew + Docker Desktop)
# Usage: curl -sSL https://raw.githubusercontent.com/joacoolopezz/LoomPanel/main/install.sh | sed 's/apt-get install/brew install/g; s/systemctl/brew services/g; s/ufw //g' | bash
# Or download and run: curl -sSL https://raw.githubusercontent.com/joacoolopezz/LoomPanel/main/install.macos.sh | bash

set -euo pipefail

say()  { printf '\033[1m%s\033[0m\n' "$*"; }
warn() { printf '\033[33m%s\033[0m\n' "$*" >&2; }
die()  { printf '\033[31m%s\033[0m\n' "$*" >&2; exit 1; }
ask()  { local a; read -r -p "$1 [$2]: " a </dev/tty || a=""; echo "${a:-$2}"; }

say "LoomPanel installer for macOS"

# --- Homebrew ---
if ! command -v brew >/dev/null; then
  say "Installing Homebrew..."
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  eval "$(/opt/homebrew/bin/brew shellenv)" 2>/dev/null || eval "$(/usr/local/bin/brew shellenv)"
fi

# --- Docker Desktop ---
if ! command -v docker >/dev/null; then
  say "Installing Docker Desktop via Homebrew..."
  brew install --cask docker
  say "Docker Desktop installed. Open it from Applications and complete setup."
  read -r -p "Press Enter when Docker Desktop shows 'Docker Desktop is running'..."
fi

# --- Wait for Docker ---
say "Waiting for Docker daemon..."
for _ in {1..30}; do
  docker version --format '{{.Server.Version}}' >/dev/null 2>&1 && break
  sleep 2
done
docker version --format '{{.Server.Version}}' >/dev/null 2>&1 || die "Docker not responding. Start Docker Desktop."

# --- Resources ---
TOTAL_RAM=$(sysctl -n hw.memsize | awk '{print int($1/1024/1024/1024)}')
CPUS=$(sysctl -n hw.ncpu)
FREE_GB=$(df -g / | tail-1 | awk '{print $4}')
REC_RAM=$(( TOTAL_RAM * 1024 - 2048 )); [ "$REC_RAM" -lt 512 ] && REC_RAM=512
REC_DISK=10

say "This Mac has ${TOTAL_RAM}GB RAM, ${CPUS} CPU cores, ${FREE_GB}GB free disk."
RAM=$(ask "RAM for game servers in MB" "$REC_RAM")
CPU=$(ask "CPU cores for game servers" "$CPUS")
DISK=$(ask "Disk for game servers in GB" "$REC_DISK")

# --- Domain / Port ---
DOMAIN=$(ask "Domain for panel (empty = localhost, no HTTPS)" "")
DATA_DIR="$HOME/loompanel/data"
PORT=8080
TOKEN=$(openssl rand -hex 12 2>/dev/null || head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')

mkdir -p "$DATA_DIR"

# --- Write .env ---
cat > .env <<EOF
GP_DATA_DIR=$DATA_DIR
GP_GAME=minecraft
GP_SETUP_TOKEN=$TOKEN
GP_MAX_RAM_MB=$RAM
GP_MAX_CPUS=$CPU
GP_MAX_DISK_GB=$DISK
GP_PORT=$PORT
GP_BIND=0.0.0.0
GP_TRUST_PROXY=0
GP_LANG=es
TZ=$(sudo systemsetup -gettimezone | awk '{print $3}')
EOF

chmod 600 .env

# --- Clone or update repo ---
REPO_DIR="$HOME/loompanel/app"
if [ -d "$REPO_DIR/.git" ]; then
  say "Updating existing repo..."
  git -C "$REPO_DIR" pull -q
else
  say "Cloning LoomPanel..."
  git clone -q https://github.com/joacoolopezz/LoomPanel.git "$REPO_DIR"
fi
cd "$REPO_DIR"

# --- Build & Start ---
say "Building and starting LoomPanel (first build takes a few minutes)..."
docker compose up -d --build

# --- Wait for API ---
for _ in {1..40}; do
  curl -fs "http://127.0.0.1:$PORT/api/setup" >/dev/null 2>&1 && break
  sleep 2
done

IP=$(curl -fs -m 5 https://api.ipify.org 2>/dev/null || ipconfig getifaddr en0 2>/dev/null || hostname -I | awk '{print $1}')

echo
printf '═%.0s' {1..55}; echo
say "LoomPanel is running!"
if [ -n "$DOMAIN" ]; then
  echo "  Panel:  https://$DOMAIN   (point A record to $IP first)"
else
  echo "  Panel:  http://$IP:$PORT"
  echo "  Local:  http://localhost:$PORT"
fi
echo "  Setup code: $TOKEN"
echo "  Game ports: 25565-25665/tcp (configure in router/firewall)"
echo "  Data dir:   $DATA_DIR"
printf '═%.0s' {1..55}; echo