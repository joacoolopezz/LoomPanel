<#
.SYNOPSIS
    LoomPanel Installer for Windows (PowerShell)
.DESCRIPTION
    Installs Docker Desktop if missing, sets up LoomPanel with Docker Compose.
.NOTES
    Run as Administrator in PowerShell 5.1+ or PowerShell 7+
    irm https://raw.githubusercontent.com/joacoolopezz/LoomPanel/main/install.ps1 | iex
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Say { Write-Host "`n[$(Get-Date -Format 'HH:mm:ss')] $args`n" -ForegroundColor Cyan }
function Warn { Write-Host "[WARN] $args" -ForegroundColor Yellow }
function Die { Write-Host "[ERROR] $args" -ForegroundColor Red; exit 1 }
function Ask {
    param([string]$Prompt, [string]$Default = '')
    if ($Default) { $suffix = " [$Default]" } else { $suffix = "" }
    $choice = Read-Host "$Prompt$suffix"
    if (-not $choice -and $Default) { return $Default }
    return $choice
}
function Confirm {
    param([string]$Prompt, [bool]$Default = $true)
    if ($Default) { $suffix = " [Y/n]" } else { $suffix = " [y/N]" }
    $choice = (Read-Host "$Prompt$suffix").ToLower()
    if (-not $choice) { return $Default }
    return $choice -in @('y','yes','s','si')
}

# --- Check Admin ---
if (-NOT ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Die "Run PowerShell as Administrator (right-click → Run as Administrator)."
}

# --- Detect OS ---
Say "Detecting Windows version..."
$ver = [System.Environment]::OSVersion.Version
if ($ver.Major -lt 10) { Die "Windows 10/11 required (build 19041+ for WSL2)." }

# --- Check/Enable WSL2 ---
Say "Checking WSL2..."
$wslStatus = wsl --status 2>$null
if (-not $wslStatus) {
    Say "Enabling WSL2 and Virtual Machine Platform..."
    dism.exe /online /enable-feature /featurename:Microsoft-Windows-Subsystem-Linux /all /norestart | Out-Null
    dism.exe /online /enable-feature /featurename:VirtualMachinePlatform /all /norestart | Out-Null
    wsl --set-default-version 2
    Warn "WSL2 enabled. **REBOOT REQUIRED** before continuing."
    Read-Host "Press Enter after reboot to continue..."
}

# --- Docker Desktop paths ---
$dockerDesktopExe = "C:\Program Files\Docker\Docker\Docker Desktop.exe"
$dockerCliPath = (Get-Command docker -ErrorAction SilentlyContinue).Source

function Test-DockerDaemon {
    # Returns $true if Docker daemon is responding
    $null = docker version --format '{{.Server.Version}}' 2>$null
    return $LASTEXITCODE -eq 0
}

function Start-DockerDesktop {
    Say "Starting Docker Desktop..."
    if (Test-Path $dockerDesktopExe) {
        $proc = Start-Process -FilePath $dockerDesktopExe -ArgumentList "" -PassThru -WindowStyle Minimized
        return $true
    }
    Warn "Docker Desktop executable not found at $dockerDesktopExe"
    return $false
}

function Wait-DockerDaemon {
    param([int]$TimeoutSeconds = 120, [int]$IntervalSeconds = 3)
    Say "Waiting for Docker daemon to be ready (up to ${TimeoutSeconds}s)..."
    $elapsed = 0
    $dots = ""
    while ($elapsed -lt $TimeoutSeconds) {
        if (Test-DockerDaemon) {
            Say "Docker daemon is running."
            return $true
        }
        $dots += "."
        if ($dots.Length -gt 10) { $dots = "." }
        Write-Host "`r[$(Get-Date -Format 'HH:mm:ss')] Waiting for Docker daemon$dots (${elapsed}s/${TimeoutSeconds}s)" -NoNewline -ForegroundColor Gray
        Start-Sleep $IntervalSeconds
        $elapsed += $IntervalSeconds
    }
    Write-Host ""
    return $false
}

function Show-DockerTroubleshooting {
    Warn "Docker daemon did not start in time. Troubleshooting steps:"
    Write-Host "  1. Open Docker Desktop manually from Start menu and wait for 'Docker Desktop is running'"
    Write-Host "  2. Check WSL2: run 'wsl --status' and 'wsl -l -v' (should show VERSION 2)"
    Write-Host "  3. Reset Docker Desktop: & 'C:\Program Files\Docker\Docker\DockerCli.exe' -Reset"
    Write-Host "  4. Check Docker context: docker context use desktop-linux"
    Write-Host "  5. Restart Windows if WSL2 was just enabled"
}

# --- Check/Install Docker Desktop ---
Say "Checking Docker installation..."

$dockerDesktopInstalled = Test-Path $dockerDesktopExe
$dockerCliExists = $null -ne $dockerCliPath
$dockerDaemonRunning = $false

if ($dockerCliExists) {
    $dockerDaemonRunning = Test-DockerDaemon
}

if (-not $dockerDesktopInstalled -and -not $dockerCliExists) {
    # Docker not installed at all
    Say "Docker Desktop is not installed."
    if (Confirm "Install Docker Desktop now?") {
        Say "Installing Docker Desktop via winget..."
        if (Get-Command winget -ErrorAction SilentlyContinue) {
            winget install --id Docker.DockerDesktop --accept-source-agreements --accept-package-agreements
        } else {
            Say "winget not found. Downloading Docker Desktop installer..."
            $dl = "https://desktop.docker.com/win/main/amd64/Docker%20Desktop%20Installer.exe"
            $out = "$env:TEMP\DockerDesktopInstaller.exe"
            Invoke-WebRequest -Uri $dl -OutFile $out
            Start-Process -FilePath $out -ArgumentList "install --quiet" -Wait
        }
        Say "Docker Desktop installed."
        $dockerDesktopInstalled = Test-Path $dockerDesktopExe
    } else {
        Die "Docker Desktop is required. Install it manually from https://www.docker.com/products/docker-desktop/ and re-run this script."
    }
}

# --- Ensure Docker Desktop is running ---
if ($dockerDesktopInstalled -and -not $dockerDaemonRunning) {
    Say "Docker Desktop is installed but daemon is not responding."
    if (Confirm "Start Docker Desktop now?") {
        if (Start-DockerDesktop) {
            if (-not (Wait-DockerDaemon -TimeoutSeconds 120)) {
                Show-DockerTroubleshooting
                Die "Docker daemon failed to start. See troubleshooting steps above."
            }
        } else {
            Die "Failed to start Docker Desktop. Try opening it manually from Start menu."
        }
    } else {
        Die "Docker daemon must be running to continue. Start Docker Desktop and re-run this script."
    }
}
elseif ($dockerCliExists -and -not $dockerDaemonRunning) {
    # CLI exists but daemon not running (maybe Docker Desktop not installed, or context issue)
    Say "Docker CLI found but daemon not responding."
    if ($dockerDesktopInstalled) {
        if (Confirm "Start Docker Desktop now?") {
            if (Start-DockerDesktop) {
                if (-not (Wait-DockerDaemon -TimeoutSeconds 120)) {
                    Show-DockerTroubleshooting
                    Die "Docker daemon failed to start. See troubleshooting steps above."
                }
            } else {
                Die "Failed to start Docker Desktop."
            }
        } else {
            Die "Docker daemon must be running."
        }
    } else {
        # Docker CLI from elsewhere (e.g., Docker Engine, not Desktop)
        Warn "Docker CLI found but Docker Desktop not detected. Ensure Docker daemon is running."
        if (-not (Wait-DockerDaemon -TimeoutSeconds 60)) {
            Show-DockerTroubleshooting
            Die "Docker daemon not responding."
        }
    }
}
else {
    Say "Docker is ready: $(docker --version)"
}

# --- Resources ---
$totalRamGB = [math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory / 1GB)
$cpuCount = (Get-CimInstance Win32_Processor).NumberOfLogicalProcessors
$freeGB = [math]::Round((Get-PSDrive C).Free / 1GB)

$recRam = [math]::Max(512, $totalRamGB * 1024 - 2048)
Say "System: ${totalRamGB}GB RAM, ${cpuCount} CPU cores, ${freeGB}GB free on C:"

$ram = Ask "RAM for game servers (MB)" $recRam
$cpu = Ask "CPU cores for game servers" $cpuCount
$disk = Ask "Disk for game servers (GB)" 10

# --- Domain / Port ---
$domain = Ask "Domain for panel (empty = localhost, no HTTPS)"
$dataDir = "C:\loompanel\data"
$port = 8080
# Cryptographically secure token (hex, 24 chars = 12 bytes)
$bytes = [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes(12)
$token = -join ($bytes | ForEach-Object { '{0:x2}' -f $_ })

New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
# Restrict data directory to current user
icacls $dataDir /inheritance:r /grant:r "$env:USERNAME:F" >$null 2>&1

# --- Write .env (restrict to current user only) ---
@"
GP_DATA_DIR=$dataDir
GP_GAME=minecraft
GP_SETUP_TOKEN=$token
GP_MAX_RAM_MB=$ram
GP_MAX_CPUS=$cpu
GP_MAX_DISK_GB=$disk
GP_PORT=$port
GP_BIND=0.0.0.0
GP_TRUST_PROXY=0
GP_LANG=es
TZ=$(Get-TimeZone).Id
"@ | Set-Content -Path ".\.env" -Encoding utf8

# Restrict .env to current user (remove inheritance, grant only current user FullControl)
icacls ".\.env" /inheritance:r /grant:r "$env:USERNAME:F" >$null 2>&1

# --- Clone or update repo ---
$repoDir = "C:\loompanel\app"
if (Test-Path "$repoDir\.git") {
    Say "Updating existing repo..."
    git -C $repoDir pull
} else {
    Say "Cloning LoomPanel..."
    git clone https://github.com/joacoolopezz/LoomPanel.git $repoDir
}

Set-Location $repoDir

# --- Build & Start ---
Say "Building and starting LoomPanel (first build takes a few minutes)..."
docker compose up -d --build

# --- Wait for API ---
Say "Waiting for panel to be ready..."
for ($i=0; $i -lt 40; $i++) {
    try {
        $resp = Invoke-RestMethod -Uri "http://127.0.0.1:$port/api/setup" -Method Get -ErrorAction Stop
        if ($resp.needed -eq $true -or $resp.needed -eq $false) { Say "Panel API responding."; break }
    } catch { }
    Start-Sleep 2
}

# --- Show result ---
$ip = $null
try { $ip = Invoke-RestMethod -Uri "https://api.ipify.org" -Method Get -ErrorAction Stop } catch { }
if (-not $ip) { $ip = (Test-Connection -ComputerName (hostname) -Count 1 -ErrorAction SilentlyContinue).IPV4Address.IPAddressToString }

Write-Host "`n" + "="*55
Write-Host "  LoomPanel is running!" -ForegroundColor Green
Write-Host "="*55
if ($domain) {
    Write-Host "  Panel:  https://$domain   (point A record to $ip first)"
} else {
    Write-Host "  Panel:  http://${ip}:${port}"
    Write-Host "  Local:  http://localhost:${port}"
}
Write-Host "  Setup code: $token"
Write-Host "  Game ports: 25565-25665/tcp (open in Windows Firewall / cloud firewall)"
Write-Host "  Data dir:   $dataDir"
Write-Host "="*55

# --- Open firewall for game ports ---
if (-not $domain) {
    Say "Opening game ports in Windows Firewall..."
    New-NetFirewallRule -DisplayName "LoomPanel Game Ports" -Direction Inbound -Protocol TCP -LocalPort 25565-25665 -Action Allow -ErrorAction SilentlyContinue
}