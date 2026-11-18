@echo off
rem ============================================================
rem  ZCode TPS 状态栏 一键安装（Go 二进制版，无需 Python/Go 环境）
rem  前提：完全退出 ZCode（含托盘）后，管理员运行本脚本
rem ============================================================

fltmc >nul 2>&1
if %errorlevel% neq 0 (
  echo [X] 当前不是管理员权限。请右键本脚本 -^> "以管理员身份运行"。
  pause
  exit /b 1
)

tasklist /FI "IMAGENAME eq ZCode.exe" 2>nul | find /I "ZCode.exe" >nul
if %errorlevel%==0 (
  echo [X] 检测到 ZCode 正在运行！请完全退出 ZCode（含托盘），再重新运行本脚本。
  pause
  exit /b 1
)

cd /d "%~dp0"

echo [1/3] 只读预检...
zcode-patcher.exe check
echo [2/3] 打补丁（首打自动整包备份 app.asar.tps.bak）...
zcode-patcher.exe install
if %errorlevel% neq 0 (
  echo [X] 打补丁失败。
  pause
  exit /b 1
)

echo [3/3] 复核（显示"已打"才算成功）...
zcode-patcher.exe check

echo.
echo 完成后启动 ZCode，输入框工具栏中部出现速度胶囊。
echo 还原：管理员运行 revert_tps_patch.bat
pause
