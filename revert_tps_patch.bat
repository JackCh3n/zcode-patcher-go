@echo off
rem ============================================================
rem  一键还原：移除 TPS 状态栏（不动其他文件，精确到字节）
rem  前提：完全退出 ZCode 后，管理员运行本脚本
rem ============================================================

fltmc >nul 2>&1
if %errorlevel% neq 0 (
  echo [X] 当前不是管理员权限。请右键本脚本 -^> "以管理员身份运行"。
  pause
  exit /b 1
)

tasklist /FI "IMAGENAME eq ZCode.exe" 2>nul | find /I "ZCode.exe" >nul
if %errorlevel%==0 (
  echo [X] ZCode 正在运行！请先完全退出 ZCode，再重新运行本脚本。
  pause
  exit /b 1
)

cd /d "%~dp0"
zcode-patcher.exe revert
zcode-patcher.exe check
echo.
echo [OK] 上方应显示"未打"。启动 ZCode 即恢复原状。
pause
