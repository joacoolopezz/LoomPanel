#!/usr/bin/env bash
# LoomPanel installer: detects the OS, installs Docker if missing, asks how many resources to use, starts the panel.
set -euo pipefail

say()  { printf '\033[1m%s\033[0m\n' "$*"; }
warn() { printf '\033[33m%s\033[0m\n' "$*" >&2; }
die()  { printf '\033[31m%s\033[0m\n' "$*" >&2; exit 1; }
ask()  { # ask "question" default -> echoes answer (reads from the terminal even under curl | bash)
  local a; read -r -p "$1 [$2]: " a </dev/tty || a=""; echo "${a:-$2}"
}

[ "$(id -u)" -eq 0 ] || die "Run as root (sudo bash install.sh)."
[ -r /etc/os-release ] || die "Unsupported system: /etc/os-release not found."
. /etc/os-release
say "Detected: ${PRETTY_NAME:-$ID} ($(uname -m))"
case "$(uname -m)" in x86_64|aarch64|arm64) ;; *) die "Unsupported architecture $(uname -m)";; esac

# --- source code ---
DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo "")"
if [ -z "$DIR" ] || [ ! -f "$DIR/Dockerfile" ]; then
  : "${GP_REPO:?Run this script from the project folder, or set GP_REPO to the git URL.}"
  command -v git >/dev/null || { (apt-get update && apt-get install -y git) >/dev/null 2>&1 || (dnf install -y git || yum install -y git) >/dev/null 2>&1 || true; }
  DIR=/opt/loompanel/app; mkdir -p /opt/loompanel; [ -d "$DIR/.git" ] && git -C "$DIR" pull -q || git clone -q "$GP_REPO" "$DIR"
fi
cd "$DIR"

# --- Docker ---
if ! command -v docker >/dev/null; then
  say "Installing Docker..."
  curl -fsSL https://get.docker.com | sh
fi
systemctl enable --now docker >/dev/null 2>&1 || true
docker info >/dev/null 2>&1 || die "Docker is installed but not running."
docker compose version >/dev/null 2>&1 || die "Docker Compose plugin missing (install docker-compose-plugin)."

# --- game ---
say "Which game do you want to manage?"
echo "  1) Minecraft (Java)"
G="$(ask "Choose" 1)"; [ "$G" = "1" ] || die "Only Minecraft is available for now."

# --- resources ---
TOTAL_RAM=$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo)
CPUS=$(nproc)
FREE_GB=$(df -BG --output=avail / | tail -1 | tr -dc '0-9')
REC_RAM=$(( TOTAL_RAM - 768 )); [ "$REC_RAM" -lt 512 ] && REC_RAM=512
REC_DISK=$(( FREE_GB * 7 / 10 )); [ "$REC_DISK" -lt 5 ] && REC_DISK=5
say "This machine has ${TOTAL_RAM} MB RAM, ${CPUS} CPU(s), ${FREE_GB} GB free disk."
RAM=$(ask "RAM for game servers in MB" "$REC_RAM")
CPU=$(ask "CPU cores for game servers" "$CPUS")
DISK=$(ask "Disk for game servers in GB" "$REC_DISK")
[ "$RAM" -le "$TOTAL_RAM" ] 2>/dev/null || warn "Heads up: that is more RAM than the machine has."

# --- access ---
DOMAIN="$(ask "Domain for the panel (empty = access by IP, no HTTPS)" "")"
DATA=/opt/loompanel/data; mkdir -p "$DATA"
TOKEN="$(openssl rand -hex 12 2>/dev/null || head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')"
{
  echo "TZ=$(timedatectl show -p Timezone --value 2>/dev/null || cat /etc/timezone 2>/dev/null || echo UTC)"
  echo "GP_DATA_DIR=$DATA"; echo "GP_GAME=minecraft"; echo "GP_SETUP_TOKEN=$TOKEN"
  echo "GP_MAX_RAM_MB=$RAM"; echo "GP_MAX_CPUS=$CPU"; echo "GP_MAX_DISK_GB=$DISK"
  if [ -n "$DOMAIN" ]; then
    echo "GP_PORT=8080"; echo "GP_BIND=127.0.0.1"; echo "GP_TRUST_PROXY=1"; echo "COMPOSE_PROFILES=https"
    printf '%s {\n  encode zstd gzip\n  reverse_proxy panel:8080\n}\n' "$DOMAIN" > Caddyfile
  else
    PORT="$(ask "Panel port" 8080)"; echo "GP_PORT=$PORT"; echo "GP_BIND=0.0.0.0"; echo "GP_TRUST_PROXY=0"
  fi
} > .env
chmod 600 .env

# --- firewall (only if ufw is active) ---
if command -v ufw >/dev/null && ufw status | grep -q "Status: active"; then
  say "ufw is active: opening panel and game ports."
  if [ -n "$DOMAIN" ]; then ufw allow 80,443/tcp >/dev/null; else ufw allow "$(grep GP_PORT .env | cut -d= -f2)"/tcp >/dev/null; fi
  ufw allow 25565:25665/tcp >/dev/null
fi

say "Building and starting (first build takes a few minutes)..."
docker compose up -d --build

for _ in $(seq 1 40); do
  curl -fs "http://127.0.0.1:$(grep GP_PORT .env | cut -d= -f2)/api/setup" >/dev/null 2>&1 && break; sleep 2
done
IP=$(curl -fs -m 5 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')
echo
say "LoomPanel is running."
if [ -n "$DOMAIN" ]; then echo "  Open:  https://$DOMAIN   (point the domain's A record to $IP first)"; else echo "  Open:  http://$IP:$(grep GP_PORT .env | cut -d= -f2)"; fi
echo "  Setup code (needed once to create the admin account): $TOKEN"
echo "  Remember to open game ports 25565-25665/tcp in your provider's firewall."
