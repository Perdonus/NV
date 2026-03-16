@echo off
set "REPO=Perdonus/NV"
set "RAW_BASE=https://raw.githubusercontent.com/%REPO%/windows-builds"
set "INSTALL_ROOT=%LOCALAPPDATA%\NV"
set "TARGET=%INSTALL_ROOT%\nv.exe"
set "WRAPPER=%INSTALL_ROOT%\nv.cmd"
set "TMP_TARGET=%INSTALL_ROOT%\nv.download.exe"
if not exist "%INSTALL_ROOT%" mkdir "%INSTALL_ROOT%" >nul 2>&1
curl.exe -fsSL "%RAW_BASE%/manifest.json" -o "%TEMP%\nv-manifest.json" || exit /b 1
powershell -NoProfile -ExecutionPolicy Bypass -Command "$m=Get-Content '%TEMP%\\nv-manifest.json' -Raw | ConvertFrom-Json; $a=$m.artifacts | Where-Object { $_.platform -eq 'nv-windows' } | Select-Object -First 1; if (-not $a -or -not $a.download_url) { exit 3 }; [Console]::Out.Write($a.download_url)" > "%TEMP%\nv-url.txt"
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
