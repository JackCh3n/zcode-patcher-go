@echo off
rem ============================================================
rem  结束 zcode-patcher 相关进程（补丁运行中会锁定 asar，需先退出）
rem ============================================================
tasklist /FI "IMAGENAME eq zcode-patcher.exe" 2>nul | find /I "zcode-patcher.exe" >nul
if %errorlevel%==0 (
  taskkill /F /IM zcode-patcher.exe 2>nul
  echo [OK] 已结束 zcode-patcher.exe
) else (
  echo [.] 没有运行中的 zcode-patcher.exe
)
tasklist /FI "IMAGENAME eq ZCode.exe" 2>nul | find /I "ZCode.exe" >nul
if %errorlevel%==0 (
  echo [!] 注意：ZCode 仍在运行。Program Files 下的 app.asar 被占用，
  echo     请先完全退出 ZCode（含托盘）再打补丁。
)
pause
