$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$files = @(
    (Join-Path $root 'install.ps1'),
    (Join-Path $root 'scripts/test-install.ps1')
)
foreach ($file in $files) {
    $tokens = $null
    $errors = $null
    [void][System.Management.Automation.Language.Parser]::ParseFile($file, [ref]$tokens, [ref]$errors)
    if ($errors.Count -gt 0) {
        $messages = ($errors | ForEach-Object { $_.Message }) -join '; '
        throw "PowerShell parse failed for $file: $messages"
    }
}
Write-Host 'PowerShell syntax validation: OK'
