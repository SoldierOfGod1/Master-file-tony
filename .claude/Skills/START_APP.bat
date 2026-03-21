@echo off
setlocal
TITLE Antigravity Skills App

echo ===================================================
echo      Antigravity Awesome Skills - Web App
echo ===================================================

:: Check for Node.js
WHERE node >nul 2>nul
IF %ERRORLEVEL% NEQ 0 (
    echo [ERROR] Node.js is not installed. Please install it from https://nodejs.org/
    pause
    exit /b 1
)

:: Check/Install dependencies
if not exist "web-app\node_modules" (
    echo [INFO] First time run detected. Installing dependencies...
    cd web-app
    call npm install
    if %ERRORLEVEL% NEQ 0 (
        echo [ERROR] Failed to install dependencies.
        pause
        exit /b 1
    )
    cd ..
)

:: Run setup script
echo [INFO] Updating skills data...
call npm run app:setup

:: Start App
echo [INFO] Starting Web App...
echo [INFO] Opening default browser...
cd web-app
call npx vite --open

endlocal
