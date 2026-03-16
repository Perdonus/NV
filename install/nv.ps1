$ErrorActionPreference = 'Stop'
$repo = 'Perdonus/NV'
$rawBase = "https://raw.githubusercontent.com/$repo/windows-builds"
$manifest = Invoke-RestMethod "$rawBase/manifest.json"
$artifact = $manifest.artifacts | Where-Object { $_.platform -eq 'nv-windows' } | Select-Object -First 1
if (-not $artifact -or -not $artifact.download_url) { throw 'nv-windows artifact not found' }
$installRoot = Join-Path $env:USERPROFILE 'AppData\Local\NV'
New-Item -ItemType Directory -Force -Path $installRoot | Out-Null
$target = Join-Path $installRoot 'nv.exe'
Invoke-WebRequest -Uri $artifact.download_url -OutFile $target
Write-Host "Installed nv to $target"
Write-Host 'Next: nv install neuralv@latest'
