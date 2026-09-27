$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
. (Join-Path $root 'install.ps1')

$meta = ConvertFrom-OsReleaseText -Lines @('ID=ubuntu', 'VERSION_ID="24.04"', 'PRETTY_NAME="Ubuntu 24.04 LTS"')
if ($meta['ID'] -ne 'ubuntu' -or $meta['VERSION_ID'] -ne '24.04') {
    throw 'os-release parser test failed'
}
if (-not (Test-SupportedDistroId -Id 'ubuntu')) {
    throw 'Ubuntu should be supported'
}
if (Test-SupportedDistroId -Id 'arch') {
    throw 'Arch should not be supported by installer'
}
if ((Get-LinuxInstallerMode -RequestedMode 'Repair') -ne '--repair') {
    throw 'mode mapping test failed'
}
Write-Host 'install.ps1 parser/WSL metadata tests: OK'
