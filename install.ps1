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

# --- Ensure git is installed ---
if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Say "Git not found. Installing via winget..."
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        winget install --id Git.Git --accept-source-agreements --accept-package-agreements
    } else {
        Say "winget not found. Please install Git manually from https://git-scm.com/ and re-run this script."
        exit 1
    }
    # Refresh PATH
    $env:PATH = [System.Environment]::GetEnvironmentVariable("Path","Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path","User")
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

# --- Docker Desktop paths (check multiple common locations) ---
$dockerDesktopPaths = @(
    "C:\Program Files\Docker\Docker\Docker Desktop.exe",                    # System-wide install (default)
    "$env:LOCALAPPDATA\Docker\Docker\Docker Desktop.exe",                   # Per-user install (old location)
    "$env:PROGRAMFILES\Docker\Docker\Docker Desktop.exe",                   # Alt ProgramFiles
    "${env:ProgramFiles(x86)}\Docker\Docker\Docker Desktop.exe",            # 32-bit ProgramFiles
    "$env:LOCALAPPDATA\Programs\DockerDesktop\Docker Desktop.exe",          # Per-user install (new default location)
    "$env:USERPROFILE\.docker\cli-plugins\docker-desktop.exe"               # Docker CLI plugin location
)

function Find-DockerDesktop {
    foreach ($path in $dockerDesktopPaths) {
        if (Test-Path $path) { return $path }
    }
    return $null
}

function Start-DockerDesktop {
    Say "Starting Docker Desktop..."
    $exe = Find-DockerDesktop
    if ($exe) {
        $proc = Start-Process -FilePath $exe -ArgumentList "" -PassThru -WindowStyle Minimized
        return $true
    }
    Warn "Docker Desktop executable not found in standard locations"
    return $false
}

function Test-DockerDaemon {
    # Returns $true if Docker daemon is responding
    try {
        $null = docker version --format '{{.Server.Version}}' -ErrorAction SilentlyContinue 2>$null
        return $LASTEXITCODE -eq 0
    } catch {
        return $false
    }
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

# --- Check/Install/Start Docker Desktop ---
Say "Checking Docker installation..."

$dockerDesktopExe = Find-DockerDesktop
$dockerDesktopInstalled = $null -ne $dockerDesktopExe
$dockerCliPath = (Get-Command docker -ErrorAction SilentlyContinue).Source
$dockerCliExists = $null -ne $dockerCliPath
$dockerDaemonRunning = $false

if ($dockerCliExists) {
    $dockerDaemonRunning = Test-DockerDaemon
}

if ($dockerDesktopInstalled) {
    Say "Docker Desktop found at: $dockerDesktopExe"
}

# --- Auto-install Docker Desktop if missing ---
if (-not $dockerDesktopInstalled -and -not $dockerCliExists) {
    Say "Docker Desktop not found. Installing automatically..."
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        Say "Installing Docker Desktop via winget (this may take a few minutes)..."
        winget install --id Docker.DockerDesktop --accept-source-agreements --accept-package-agreements
    } else {
        Say "winget not found. Downloading Docker Desktop installer with checksum verification..."
        $dl = "https://desktop.docker.com/win/main/amd64/Docker%20Desktop%20Installer.exe"
        $out = "$env:TEMP\DockerDesktopInstaller.exe"
        Invoke-WebRequest -Uri $dl -OutFile $out

        # Verify SHA256 checksum against Docker's official release checksums (mandatory)
        Say "Verifying installer checksum..."
        $checksumUrl = "https://desktop.docker.com/win/main/amd64/sha256sum.txt"
        $checksumContent = Invoke-WebRequest -Uri $checksumUrl -UseBasicParsing -ErrorAction Stop
        $expectedHash = ($checksumContent.Content -split '\s+')[0].ToLower()
        $actualHash = (Get-FileHash $out -Algorithm SHA256).Hash.ToLower()
        if ($actualHash -ne $expectedHash) {
            Die "Checksum verification FAILED! Expected: $expectedHash, Got: $actualHash. Download may be corrupted or compromised."
        }
        Say "Checksum verified: $actualHash"

        Start-Process -FilePath $out -ArgumentList "install --quiet" -Wait
    }
    Say "Docker Desktop installed."
    # Re-detect after install
    Start-Sleep 5
    $dockerDesktopExe = Find-DockerDesktop
    $dockerDesktopInstalled = $null -ne $dockerDesktopExe
    $dockerCliPath = (Get-Command docker -ErrorAction SilentlyContinue).Source
    $dockerCliExists = $null -ne $dockerCliPath
}

# --- Auto-start Docker Desktop if installed but daemon not running ---
if ($dockerDesktopInstalled -and -not $dockerDaemonRunning) {
    Say "Docker Desktop is installed but daemon not running. Starting automatically..."
    $dockerDesktopExe = Find-DockerDesktop
    if (Start-DockerDesktop) {
        if (-not (Wait-DockerDaemon -TimeoutSeconds 180)) {
            Show-DockerTroubleshooting
            Die "Docker daemon failed to start after 3 minutes. See troubleshooting steps above."
        }
    } else {
        Die "Failed to start Docker Desktop. Try opening it manually from Start menu and re-run this script."
    }
}
elseif ($dockerCliExists -and -not $dockerDaemonRunning) {
    # CLI exists but daemon not running (maybe Docker Desktop not installed, or context issue)
    Say "Docker CLI found but daemon not responding."
    $dockerDesktopExe = Find-DockerDesktop
    if ($dockerDesktopExe) {
        Say "Starting Docker Desktop automatically..."
        if (Start-DockerDesktop) {
            if (-not (Wait-DockerDaemon -TimeoutSeconds 180)) {
                Show-DockerTroubleshooting
                Die "Docker daemon failed to start after 3 minutes. See troubleshooting steps above."
            }
        } else {
            Die "Failed to start Docker Desktop."
        }
    } else {
        # Docker CLI from elsewhere (e.g., Docker Engine, not Desktop)
        Warn "Docker CLI found but Docker Desktop not detected. Waiting for daemon..."
        if (-not (Wait-DockerDaemon -TimeoutSeconds 120)) {
            Show-DockerTroubleshooting
            Die "Docker daemon not responding after 2 minutes."
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

# Auto-use recommended values (user can override by passing env vars or editing .env later)
$ram = $env:LOOMPANEL_RAM_MB
if (-not $ram) { $ram = $recRam }
else { Say "Using RAM from env: ${ram}MB" }

$cpu = $env:LOOMPANEL_CPUS
if (-not $cpu) { $cpu = $cpuCount }
else { Say "Using CPU from env: ${cpu} cores" }

$disk = $env:LOOMPANEL_DISK_GB
if (-not $disk) { $disk = 10 }
else { Say "Using disk from env: ${disk}GB" }

Say "Resources allocated: ${ram}MB RAM, ${cpu} CPU cores, ${disk}GB disk"

# --- Domain / Port ---
$domain = $env:LOOMPANEL_DOMAIN
if (-not $domain) {
    $domain = Ask "Domain for panel (empty = localhost, no HTTPS)" ""
}
$dataDir = "C:\loompanel\data"
$port = 8080
# Cryptographically secure token (hex, 24 chars = 12 bytes)
$bytes = [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes(12)
$token = -join ($bytes | ForEach-Object { '{0:x2}' -f $_ })

New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
# Restrict data directory to current user
icacls $dataDir /inheritance:r /grant:r "$env:USERNAME:(OI)(CI)F" >$null 2>&1

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