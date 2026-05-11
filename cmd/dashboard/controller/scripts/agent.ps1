$NZ_BASE_PATH = "C:\nezha"
$NZ_AGENT_PATH = "$NZ_BASE_PATH\agent"

function err($msg) {
    Write-Host $msg -ForegroundColor Red
}

function success($msg) {
    Write-Host $msg -ForegroundColor Green
}

function info($msg) {
    Write-Host $msg -ForegroundColor Yellow
}

function env_check() {
    $arch = $env:PROCESSOR_ARCHITECTURE
    switch ($arch) {
        "AMD64" { $global:os_arch = "amd64" }
        "x86" { $global:os_arch = "386" }
        "ARM64" { $global:os_arch = "arm64" }
        default { err "Unknown architecture: $arch"; exit 1 }
    }
    $global:os = "windows"
}

function install() {
    info "Installing nezha-agent..."

    if ($env:NZ_DASHBOARD_URL) {
        $NZ_AGENT_URL = "$($env:NZ_DASHBOARD_URL)/script/bin/$global:os/$global:os_arch"
    } else {
        $NZ_AGENT_URL = "https://github.com/nezhahq/agent/releases/latest/download/nezha-agent_$global:os`_$global:os_arch.zip"
    }

    $dest = "$env:TEMP\nezha-agent.zip"
    try {
        Invoke-WebRequest -Uri $NZ_AGENT_URL -OutFile $dest
    } catch {
        err "Download nezha-agent failed, check your network connectivity"
        exit 1
    }

    if (!(Test-Path $NZ_AGENT_PATH)) {
        New-Item -ItemType Directory -Path $NZ_AGENT_PATH
    }

    Expand-Archive -Path $dest -DestinationPath $NZ_AGENT_PATH -Force
    Remove-Item $dest

    $path = "$NZ_AGENT_PATH\config.yml"
    if (Test-Path $path) {
        $random = -join ((97..122) + (48..57) | Get-Random -Count 5 | ForEach-Object { [char]$_ })
        $path = "$NZ_AGENT_PATH\config-$random.yml"
    }

    if (!($env:NZ_SERVER)) {
        err "NZ_SERVER should not be empty"
        exit 1
    }

    if (!($env:NZ_CLIENT_SECRET)) {
        err "NZ_CLIENT_SECRET should not be empty"
        exit 1
    }

    $args = "service -c $path uninstall"
    Start-Process -FilePath "$NZ_AGENT_PATH\nezha-agent.exe" -ArgumentList $args -Wait -WindowStyle Hidden -ErrorAction SilentlyContinue

    $env_str = "NZ_UUID=$($env:NZ_UUID) NZ_SERVER=$($env:NZ_SERVER) NZ_CLIENT_SECRET=$($env:NZ_CLIENT_SECRET) NZ_TLS=$($env:NZ_TLS) NZ_DISABLE_AUTO_UPDATE=$($env:NZ_DISABLE_AUTO_UPDATE) NZ_DISABLE_FORCE_UPDATE=$($env:NZ_DISABLE_FORCE_UPDATE) NZ_DISABLE_COMMAND_EXECUTE=$($env:NZ_DISABLE_COMMAND_EXECUTE) NZ_SKIP_CONNECTION_COUNT=$($env:NZ_SKIP_CONNECTION_COUNT)"
    
    # PowerShell env setting is different, but nezha-agent handles these env vars
    $args = "service -c $path install"
    # Set env vars for the process
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = "$NZ_AGENT_PATH\nezha-agent.exe"
    $psi.Arguments = $args
    $psi.EnvironmentVariables["NZ_SERVER"] = $env:NZ_SERVER
    $psi.EnvironmentVariables["NZ_TLS"] = $env:NZ_TLS
    $psi.EnvironmentVariables["NZ_CLIENT_SECRET"] = $env:NZ_CLIENT_SECRET
    if ($env:NZ_UUID) { $psi.EnvironmentVariables["NZ_UUID"] = $env:NZ_UUID }
    
    $proc = [System.Diagnostics.Process]::Start($psi)
    $proc.WaitForExit()

    if ($proc.ExitCode -ne 0) {
        err "Install nezha-agent service failed"
        exit 1
    }

    success "nezha-agent successfully installed"
}

env_check
install
