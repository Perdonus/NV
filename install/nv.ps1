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
Write-Host "Установлен или обновлен nv в $target"
