@echo off
set "SITE_BASE=%NV_SITE_BASE%"
if "%SITE_BASE%"=="" set "SITE_BASE=https://sosiskibot.ru"
set "API_BASE=%NV_BOOTSTRAP_BASE%"
if "%API_BASE%"=="" set "API_BASE=%SITE_BASE%/nv/api"
set "DEFAULT_INSTALL_ROOT=%LOCALAPPDATA%\NV"
set "REGISTRY_KEY=HKCU:\Software\NV\Packages\nv-nv-windows"
set "LEGACY_REGISTRY_KEY=HKCU:\Software\NV\Packages\lvls-nv-nv-windows"
set "INSTALL_ROOT=%NV_INSTALL_ROOT%"
if not "%INSTALL_ROOT%"=="" goto install_root_ready
for /f "usebackq delims=" %%I in (`powershell -NoProfile -ExecutionPolicy Bypass -Command "$key='%REGISTRY_KEY%'; if (Test-Path $key) { $value=(Get-ItemProperty -Path $key -Name InstallRoot -ErrorAction SilentlyContinue).InstallRoot; if ($value) { [Console]::Out.Write($value) } }" 2^>nul`) do (
  set "INSTALL_ROOT=%%I"
  goto install_root_ready
)
for /f "usebackq delims=" %%I in (`powershell -NoProfile -ExecutionPolicy Bypass -Command "$key='%LEGACY_REGISTRY_KEY%'; if (Test-Path $key) { $value=(Get-ItemProperty -Path $key -Name InstallRoot -ErrorAction SilentlyContinue).InstallRoot; if ($value) { [Console]::Out.Write($value) } }" 2^>nul`) do (
  set "INSTALL_ROOT=%%I"
  goto install_root_ready
)
for /f "usebackq delims=" %%I in (`where nv.exe 2^>nul`) do (
  set "INSTALL_ROOT=%%~dpI"
  goto install_root_ready
)
if exist "%DEFAULT_INSTALL_ROOT%\nv.exe" (
  set "INSTALL_ROOT=%DEFAULT_INSTALL_ROOT%"
  goto install_root_ready
)
set /p INSTALL_ROOT=Папка установки NV [%DEFAULT_INSTALL_ROOT%]:
if "%INSTALL_ROOT%"=="" set "INSTALL_ROOT=%DEFAULT_INSTALL_ROOT%"
:install_root_ready
if "%INSTALL_ROOT:~-1%"=="\" set "INSTALL_ROOT=%INSTALL_ROOT:~0,-1%"
set "TARGET=%INSTALL_ROOT%\nv.exe"
set "WRAPPER=%INSTALL_ROOT%\nv.cmd"
set "TMP_TARGET=%INSTALL_ROOT%\nv.download.exe"
if not exist "%INSTALL_ROOT%" mkdir "%INSTALL_ROOT%" >nul 2>&1
curl.exe -fsSL "%API_BASE%/bootstrap/manifest?platform=nv-windows" -o "%TEMP%\nv-manifest.json" || exit /b 1
powershell -NoProfile -ExecutionPolicy Bypass -Command "$m=Get-Content '%TEMP%\nv-manifest.json' -Raw | ConvertFrom-Json; $a=$m.artifacts | Where-Object { $_.platform -eq 'nv-windows' } | Select-Object -First 1; if (-not $a -or -not $a.download_url) { exit 3 }; $u="$($a.download_url)"; if ($u.StartsWith('/')) { $u='%SITE_BASE%'+$u }; [Console]::Out.Write($u)" > "%TEMP%\nv-url.txt"
if errorlevel 1 exit /b 1
set /p NV_URL=<"%TEMP%\nv-url.txt"
if "%NV_URL%"=="" exit /b 1
if exist "%TMP_TARGET%" del /f /q "%TMP_TARGET%" >nul 2>&1
curl.exe -fsSL "%NV_URL%" -o "%TMP_TARGET%" || exit /b 1
move /y "%TMP_TARGET%" "%TARGET%" >nul || exit /b 1
> "%WRAPPER%" echo @echo off
>> "%WRAPPER%" echo "%TARGET%" %%*
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$entry='%INSTALL_ROOT%';" ^
  "$userPath=[Environment]::GetEnvironmentVariable('Path','User');" ^
  "$parts=@(); if ($userPath) { $parts=$userPath.Split(';',[System.StringSplitOptions]::RemoveEmptyEntries) };" ^
  "$exists=$false; foreach ($part in $parts) { if ($part.TrimEnd('\') -ieq $entry.TrimEnd('\')) { $exists=$true; break } };" ^
  "if (-not $exists) { [Environment]::SetEnvironmentVariable('Path', (($entry) + ';' + $userPath).Trim(';'),'User') }" || exit /b 1
set "PATH=%INSTALL_ROOT%;%PATH%"
"%TARGET%" -v > "%TEMP%\nv-version.txt" 2>&1 || (
  type "%TEMP%\nv-version.txt"
  exit /b 1
)
echo NV установлен: %TARGET%
type "%TEMP%\nv-version.txt"
