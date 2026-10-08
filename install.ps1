[CmdletBinding()]
param(
    [ValidateSet('Install', 'Status', 'Repair', 'Update', 'Reinstall', 'Uninstall')]
    [string]$Mode = 'Install',
    [string]$Distribution,
    [switch]$InstallUbuntu,
    [switch]$Purge
)

$ErrorActionPreference = 'Stop'

function Write-Status {
    param(
        [Parameter(Mandatory = $true)][string]$Level,
        [Parameter(Mandatory = $true)][string]$Message
    )
    Write-Host ('[{0}] {1}' -f $Level, $Message)
}

function Get-WSLDistributions {
    $output = & wsl.exe -l -q 2>$null
    if ($LASTEXITCODE -ne 0) {
        return @()
    }
    return @($output | ForEach-Object { ($_ -replace "`0", '').Trim() } | Where-Object { $_ })
}

function ConvertFrom-OsReleaseText {
    param([Parameter(Mandatory = $true)][string[]]$Lines)
    $result = @{}
    foreach ($line in $Lines) {
        $trimmed = $line.Trim()
        if (-not $trimmed -or $trimmed.StartsWith('#') -or -not $trimmed.Contains('=')) {
            continue
        }
        $parts = $trimmed.Split('=', 2)
        $key = $parts[0].Trim()
        $value = $parts[1].Trim().Trim('"').Trim("'")
        $result[$key] = $value
    }
    return $result
}

function Get-WSLDistroMetadata {
    param([Parameter(Mandatory = $true)][string]$Name)
    $lines = & wsl.exe -d $Name -- sh -lc 'cat /etc/os-release 2>/dev/null' 2>$null
    if ($LASTEXITCODE -ne 0) {
        return @{}
    }
    return ConvertFrom-OsReleaseText -Lines @($lines)
}

function Test-SupportedDistroId {
    param([string]$Id)
    return $Id -in @('ubuntu', 'debian')
}

function Test-WSLSystemd {
    param([Parameter(Mandatory = $true)][string]$Name)
    & wsl.exe -d $Name -- sh -lc 'test "$(ps -p 1 -o comm= | tr -d " ")" = systemd' 2>$null
    return $LASTEXITCODE -eq 0
}

function Enable-WSLSystemd {
    param([Parameter(Mandatory = $true)][string]$Name)
    $script = 'set -eu; f=/etc/wsl.conf; touch "$f"; if grep -q "^systemd=" "$f"; then sed -i "s/^systemd=.*/systemd=true/" "$f"; elif grep -q "^\[boot\]" "$f"; then sed -i "/^\[boot\]/a systemd=true" "$f"; else printf "\n[boot]\nsystemd=true\n" >> "$f"; fi'
    & wsl.exe -d $Name -u root -- sh -lc $script
    if ($LASTEXITCODE -ne 0) {
        throw "Nie udało się włączyć systemd w dystrybucji $Name."
    }
    Write-Status -Level 'INFO' -Message 'Włączono systemd w /etc/wsl.conf; restartuję WSL.'
    & wsl.exe --shutdown
    Start-Sleep -Seconds 2
    & wsl.exe -d $Name -- true
    if ($LASTEXITCODE -ne 0) {
        throw "Nie udało się ponownie uruchomić dystrybucji $Name."
    }
}

function Install-UbuntuWSL {
    Write-Status -Level 'INFO' -Message 'Instaluję WSL z Ubuntu.'
    & wsl.exe --install -d Ubuntu
    if ($LASTEXITCODE -ne 0) {
        throw 'wsl --install -d Ubuntu zakończył się błędem.'
    }
    Write-Status -Level 'WARN' -Message 'Jeżeli Windows wymaga restartu, uruchom ponownie system i ponów install.ps1.'
}

function Select-WSLDistribution {
    param(
        [string[]]$Available,
        [string]$Requested
    )
    if ($Requested) {
        if ($Requested -notin $Available) {
            throw "Dystrybucja WSL '$Requested' nie istnieje."
        }
        $meta = Get-WSLDistroMetadata -Name $Requested
        if (-not (Test-SupportedDistroId -Id $meta['ID'])) {
            throw "Dystrybucja '$Requested' nie jest obsługiwana. Obsługiwane: Ubuntu, Debian."
        }
        return $Requested
    }
    foreach ($name in $Available) {
        $meta = Get-WSLDistroMetadata -Name $name
        if (Test-SupportedDistroId -Id $meta['ID']) {
            return $name
        }
    }
    return $null
}

function Get-LinuxInstallerMode {
    param([string]$RequestedMode)
    switch ($RequestedMode) {
        'Install' { return '--install' }
        'Status' { return '--status' }
        'Repair' { return '--repair' }
        'Update' { return '--update' }
        'Reinstall' { return '--reinstall' }
        'Uninstall' { return '--uninstall' }
        default { throw "Nieobsługiwany tryb: $RequestedMode" }
    }
}

function Invoke-Main {
    Write-Status -Level 'INFO' -Message '[1/8] Wykrywanie WSL'
    $distributions = Get-WSLDistributions
    if ($distributions.Count -eq 0) {
        if (-not $InstallUbuntu) {
            Write-Status -Level 'FAIL' -Message 'Brak wspieranej dystrybucji WSL. Uruchom ponownie z -InstallUbuntu.'
            exit 2
        }
        Install-UbuntuWSL
        $distributions = Get-WSLDistributions
        if ($distributions.Count -eq 0) {
            Write-Status -Level 'FAIL' -Message 'Ubuntu nie jest jeszcze gotowe. Może być wymagany restart Windows.'
            exit 3
        }
    }

    Write-Status -Level 'INFO' -Message '[2/8] Wybór dystrybucji'
    $selected = Select-WSLDistribution -Available $distributions -Requested $Distribution
    if (-not $selected -and $InstallUbuntu) {
        Install-UbuntuWSL
        $distributions = Get-WSLDistributions
        $selected = Select-WSLDistribution -Available $distributions -Requested 'Ubuntu'
    }
    if (-not $selected) {
        Write-Status -Level 'FAIL' -Message 'Nie znaleziono wspieranej dystrybucji. Obsługiwane: Ubuntu, Debian.'
        exit 4
    }
    $metadata = Get-WSLDistroMetadata -Name $selected
    Write-Status -Level ' OK ' -Message ("WSL: {0} ({1} {2})" -f $selected, $metadata['ID'], $metadata['VERSION_ID'])

    Write-Status -Level 'INFO' -Message '[3/8] Sprawdzanie systemd'
    if (-not (Test-WSLSystemd -Name $selected)) {
        if ($Mode -in @('Install', 'Repair', 'Update', 'Reinstall')) {
            Enable-WSLSystemd -Name $selected
        }
    }
    if (Test-WSLSystemd -Name $selected) {
        Write-Status -Level ' OK ' -Message 'systemd aktywny w WSL.'
    }
    else {
        Write-Status -Level 'WARN' -Message 'systemd nadal nie jest aktywny; usługa DevBox może nie wystartować.'
    }

    Write-Status -Level 'INFO' -Message '[4/8] Lokalizacja repozytorium w WSL'
    $linuxRoot = (& wsl.exe -d $selected -- wslpath -a -u $PSScriptRoot).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $linuxRoot) {
        Write-Status -Level 'FAIL' -Message 'Nie udało się przetłumaczyć ścieżki Windows na ścieżkę WSL.'
        exit 5
    }
    Write-Status -Level ' OK ' -Message $linuxRoot

    Write-Status -Level 'INFO' -Message '[5/8] Uruchamianie instalatora Linux'
    $linuxMode = Get-LinuxInstallerMode -RequestedMode $Mode
    $linuxArgs = @('-d', $selected, '-u', 'root', '--', 'bash', "$linuxRoot/install.sh", $linuxMode)
    if ($Purge) {
        if ($Mode -ne 'Uninstall') {
            Write-Status -Level 'FAIL' -Message '-Purge jest dozwolone tylko z -Mode Uninstall.'
            exit 6
        }
        $linuxArgs += '--purge'
    }
    & wsl.exe @linuxArgs
    if ($LASTEXITCODE -ne 0) {
        Write-Status -Level 'FAIL' -Message "Linux installer zakończył się kodem $LASTEXITCODE."
        exit $LASTEXITCODE
    }

    Write-Status -Level 'INFO' -Message '[6/8] Weryfikacja usługi'
    if ($Mode -in @('Install', 'Repair', 'Update', 'Reinstall', 'Status')) {
        & wsl.exe -d $selected -- sh -lc 'systemctl is-active devbox.service >/dev/null 2>&1' 2>$null
        if ($LASTEXITCODE -eq 0) {
            Write-Status -Level ' OK ' -Message 'devbox.service active.'
        }
        else {
            Write-Status -Level 'WARN' -Message 'devbox.service nie jest aktywna.'
        }
    }
    else {
        Write-Status -Level 'INFO' -Message 'Tryb uninstall: weryfikacja usługi pominięta.'
    }

    Write-Status -Level 'INFO' -Message '[7/8] Sprawdzanie GUI/API'
    $gui = 'http://localhost:8787/'
    if ($Mode -in @('Install', 'Repair', 'Update', 'Reinstall', 'Status')) {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri ($gui + 'api/v1/health') -TimeoutSec 3
            if ($response.StatusCode -eq 200) {
                Write-Status -Level ' OK ' -Message "GUI/API dostępne: $gui"
            }
        }
        catch {
            Write-Status -Level 'WARN' -Message "GUI/API nie odpowiada jeszcze pod $gui"
        }
    }
    else {
        Write-Status -Level 'INFO' -Message 'Tryb uninstall: healthcheck pominięty.'
    }

    Write-Status -Level 'INFO' -Message '[8/8] Podsumowanie'
    if ($Mode -eq 'Uninstall') {
        Write-Status -Level ' OK ' -Message 'Odinstalowanie zakończone. Dane były zachowane, chyba że użyto -Purge.'
    }
    else {
        Write-Status -Level ' OK ' -Message "DevBox Universal: $gui"
    }
}

if ($MyInvocation.InvocationName -ne '.') {
    Invoke-Main
}
