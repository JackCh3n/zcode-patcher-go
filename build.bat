@echo off
rem ============================================================
rem  编译启动脚本：先结束旧 zcode-patcher 进程，再编译，最后打开浏览器查看说明
rem ============================================================
taskkill /F /IM zcode-patcher.exe /IM zcode-patcher 2>nul

where go >nul 2>&1
if %errorlevel% neq 0 (
  echo [X] 找不到 go 命令。请安装 Go 1.22+，或改用仓库 Release 里的预编译二进制。
  pause
  exit /b 1
)

cd /d "%~dp0"
echo [1/3] 同步内嵌脚本（scripts -> cmd/embed）...
copy /y scripts\zcode-tps.js         cmd\embed\zcode-tps.js >nul
copy /y scripts\zcode-model-puller.js cmd\embed\zcode-model-puller.js >nul

echo [2/3] 编译 zcode-patcher（Windows amd64）...
go build -trimpath -ldflags="-s -w" -o zcode-patcher.exe ./cmd
if %errorlevel% neq 0 (
  echo [X] 编译失败。
  pause
  exit /b 1
)
echo     完成 -> %~dp0zcode-patcher.exe

echo [3/3] 打开说明文档...
start "" "%~dp0README.md"

echo [OK] 编译完成，可运行：zcode-patcher.exe check
pause
