@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

rem Checks prerequisites (Go, Python 3, and its packages), then launches the
rem interactive server-panel TUI (tui-panel\server-panel.py). Run this instead
rem of the TUI directly so missing prerequisites get a clear message instead
rem of a Python traceback.

echo == Checking prerequisites ==

set "PROBLEMS=0"

rem --- Go ---------------------------------------------------------------
where go >nul 2>&1
if errorlevel 1 (
  echo [WARN] Go is not installed ^(needed to build owpengram-server / owpengram-admin-panel^)
  echo        Install it from: https://go.dev/dl/
  set "PROBLEMS=1"
) else (
  for /f "delims=" %%v in ('go version') do echo [ok] Go found: %%v
)

rem --- Python -------------------------------------------------------------
set "PYTHON="
where py >nul 2>&1
if not errorlevel 1 (
  py -3 --version >nul 2>&1
  if not errorlevel 1 set "PYTHON=py -3"
)
if not defined PYTHON (
  where python >nul 2>&1
  if not errorlevel 1 set "PYTHON=python"
)

if not defined PYTHON (
  echo [WARN] Python 3 is not installed ^(needed to run the server-panel TUI^)
  echo        Install it from: https://www.python.org/downloads/
  set "PROBLEMS=1"
) else (
  for /f "delims=" %%v in ('!PYTHON! --version 2^>^&1') do echo [ok] Python found: %%v ^(!PYTHON!^)
)

rem --- Python dependencies --------------------------------------------------
if defined PYTHON (
  set "MISSING="
  for /f "delims=" %%m in ('!PYTHON! tui-panel\check_deps.py') do (
    if not defined MISSING (set "MISSING=%%m") else (set "MISSING=!MISSING!, %%m")
  )
  if defined MISSING (
    echo [WARN] Missing Python packages: !MISSING!
    echo        Install them with: !PYTHON! -m pip install -r tui-panel\requirements-panel.txt
    set "PROBLEMS=1"
  ) else (
    echo [ok] Python dependencies OK ^(textual, psutil, cryptography^)
  )
)

if "%PROBLEMS%"=="1" (
  echo.
  echo [ERROR] missing prerequisites above -- install them and re-run this script
  pause
  exit /b 1
)

echo.
echo [cfg] All prerequisites OK, launching server panel...
echo.
!PYTHON! tui-panel\server-panel.py %*
