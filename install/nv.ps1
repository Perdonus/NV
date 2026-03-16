$ErrorActionPreference = 'Stop'
$repo = 'Perdonus/NV'
$rawBase = "https://raw.githubusercontent.com/$repo/windows-builds"
$manifest = Invoke-RestMethod "$rawBase/manifest.json"
$artifact = $manifest.artifacts | Where-Object { $_.platform -eq 'nv-windows' } | Select-Object -First 1
if (-not $artifact -or -not $artifact.download_url) { throw 'nv-windows artifact not found' }
$installRoot = Join-Path $env:USERPROFILE 'AppData\Local\NV'
New-Item -ItemType Directory -Force -Path $installRoot | Out-Null
$target = Join-Path $installRoot 'nv.exe'
$wrapper = Join-Path $installRoot 'nv.cmd'
$tempTarget = Join-Path $installRoot 'nv.download.exe'
if (Test-Path $tempTarget) { Remove-Item -Force $tempTarget }
Invoke-WebRequest -Uri $artifact.download_url -OutFile $tempTarget
Move-Item -Force $tempTarget $target
Set-Content -Path $wrapper -Value "@echo off`r`n`"$target`" %*`r`n" -Encoding ASCII

function Add-UserPathEntry {
    param([string]$PathEntry)

    $currentUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $segments = @()
    if ($currentUserPath) {
        $segments = $currentUserPath.Split(';', [System.StringSplitOptions]::RemoveEmptyEntries)
    }
    $exists = $false
    foreach ($segment in $segments) {
        if ($segment.TrimEnd('\') -ieq $PathEntry.TrimEnd('\')) {
            $exists = $true
            break
        }
    }
    if (-not $exists) {
        $updatedSegments = @($PathEntry)
        if ($segments.Count -gt 0) {
            $updatedSegments += $segments
        }
        [Environment]::SetEnvironmentVariable('Path', ($updatedSegments -join ';'), 'User')
    }
    if (-not (($env:Path -split ';') | Where-Object { $_.TrimEnd('\') -ieq $PathEntry.TrimEnd('\') })) {
        $env:Path = "$PathEntry;$env:Path"
    }
}

Add-UserPathEntry -PathEntry $installRoot

$versionOutput = & $target -v
if ($LASTEXITCODE -ne 0) {
    throw 'nv verification failed'
}

Write-Host "NV установлен: $target"
Write-Host $versionOutput
