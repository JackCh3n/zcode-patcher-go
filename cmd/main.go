package main

// main.go — ZCode 补丁工具统一入口（Go 版，替代 python scripts/zcode_patcher.py）。
//
// 用法：
//   zcode-patcher check [安装目录]            只读检查全部补丁状态
//   zcode-patcher install [安装目录]           打全部补丁（按各补丁自身的跳过逻辑）
//   zcode-patcher revert [安装目录]            还原全部补丁（外科手术式，精确到字节）
//   zcode-patcher restore [--latest] [--backup-dir DIR]  从干净备份整包恢复（紧急）
//   zcode-patcher <patch> <check|install|revert> [目录]  单补丁操作：kernel/tps/usage-chart/
//                                                        model-width/model-puller
//   zcode-patcher model-pull --all [--dry-run] [--refresh]  拉取供应商模型到 config.json
//
// 安装位置探测：运行中进程 → 注册表卸载信息 → 常规目录（Windows/macOS/Linux）；
// macOS 的 .app 包自动展开到 Contents。

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func usage() {
	fmt.Println(`用法:
  zcode-patcher check [目录]            只读检查全部补丁状态
  zcode-patcher install [目录]           打全部补丁
  zcode-patcher revert [目录]            还原全部补丁
  zcode-patcher restore [--latest]      从干净备份整包恢复（需先完全退出 ZCode）
  zcode-patcher kernel/tps/usage-chart/model-width/model-puller check|install|revert [目录]
  zcode-patcher model-pull --all [--dry-run] [--refresh] [--provider 子串]`)
	os.Exit(2)
}

func normName(s string) string { return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "-", "")) }

// ---- 探测：运行中进程（仅 Windows） ----

func probeRunning(found *[]string) {
	if runtime.GOOS != "windows" {
		return
	}
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"Get-Process | Where-Object {$_.Path} | Select-Object -ExpandProperty Path -Unique").Output()
	if err != nil {
		return
	}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasSuffix(strings.ToLower(line), ".exe") {
			continue
		}
		name := strings.ToLower(filepath.Base(line))
		if strings.Contains(normName(name), "zcode") {
			*found = append(*found, filepath.Dir(line))
		}
	}
}

// ---- 探测：注册表（仅 Windows） ----

func regValue(run, key, name string) (string, error) {
	out, err := exec.Command("reg", "query", key, "/v", name).CombinedOutput()
	if err != nil {
		return "", err
	}
	for _, ln := range strings.Split(string(out), "\n") {
		f := strings.Fields(ln)
		if len(f) >= 3 && f[len(f)-2] == "REG_" && f[0] == key[:strings.LastIndex(key, "\\")] {
			_ = run
			val := strings.Join(f[1:len(f)-2], " ")
			// 取最后一个 REG_xxx 之后的部分
			for i := len(f) - 1; i >= 0; i-- {
				if strings.HasPrefix(f[i], "REG_") {
					if i+1 < len(f) {
						val = strings.Join(f[i+1:], " ")
					}
					break
				}
			}
			return val, nil
		}
	}
	return "", fmt.Errorf("no value")
}

func probeRegistry(found *[]string) {
	if runtime.GOOS != "windows" {
		return
	}
	roots := []string{
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
		`HKLM\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
		`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
	}
	for _, base := range roots {
		// 单次递归拉取整个卸载树；只对含 zcode 的键解析安装位置
		out, err := exec.Command("reg", "query", base, "/s").CombinedOutput()
		if err != nil || len(out) == 0 {
			continue
		}
		curKey := ""
		for _, ln := range strings.Split(string(out), "\n") {
			t := strings.TrimSpace(ln)
			if strings.HasPrefix(t, "HKEY_") {
				curKey = t
				continue
			}
			if curKey == "" || !strings.Contains(strings.ToLower(curKey), "zcode") {
				continue
			}
			// 值行形态：<名称>  REG_xxx  <数据…>（数据可含空格）
			f := strings.Fields(t)
			if len(f) < 3 {
				continue
			}
			name, typ, data := f[0], f[1], strings.Join(f[2:], " ")
			if typ == "REG_EXPAND_SZ" && strings.HasPrefix(data, `"`) {
				data = strings.Trim(data, `"`)
			}
			switch name {
			case "InstallLocation", "DisplayIcon", "UninstallString":
				p := data
				if i := strings.Index(p, ","); i >= 0 {
					p = p[:i]
				}
				p = strings.Trim(p, `"`)
				candidates := []string{p}
				if !filepath.IsAbs(p) {
					candidates = append(candidates, filepath.Join(`C:\Program Files`, p))
				}
				candidates = append(candidates, filepath.Dir(p))
				for _, root := range candidates {
					if info, err := os.Stat(filepath.Join(root, "resources", "glm", "zcode.cjs")); err == nil && !info.IsDir() {
						uniqAdd(found, root)
						break
					}
				}
			}
		}
	}
}

func uniqAdd(list *[]string, v string) {
	for _, x := range *list {
		if x == v {
			return
		}
	}
	*list = append(*list, v)
}

// ---- 探测：常规目录 ----

func probeCommonDirs(found *[]string) {
	bases := []string{}
	if runtime.GOOS == "windows" {
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432"} {
			if v := os.Getenv(env); v != "" {
				bases = append(bases, v)
			}
		}
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			bases = append(bases, filepath.Join(v, "Programs"))
		}
	} else {
		bases = append(bases, "/Applications", filepath.Join(home(), "Applications"), "/opt", "/usr/share")
	}
	for _, base := range bases {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !strings.Contains(normName(e.Name()), "zcode") {
				continue
			}
			root := base
			if runtime.GOOS != "windows" && strings.HasSuffix(e.Name(), ".app") {
				root = filepath.Join(base, e.Name(), "Contents")
			} else {
				root = filepath.Join(base, e.Name())
			}
			info, err := os.Stat(filepath.Join(root, "resources", "glm", "zcode.cjs"))
			if err == nil && !info.IsDir() {
				*found = append(*found, root)
			}
		}
	}
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// discover：返回所有探测到的 zcode.cjs 路径（去重保序）
func discover() []string {
	var roots []string
	probeRunning(&roots)
	probeRegistry(&roots)
	probeCommonDirs(&roots)
	seen := map[string]bool{}
	result := []string{}
	sort.Strings(roots)
	for _, r := range roots {
		if seen[r] {
			continue
		}
		cjs := filepath.Join(r, "resources", "glm", "zcode.cjs")
		info, err := os.Stat(cjs)
		if err != nil || info.IsDir() {
			continue
		}
		seen[r] = true
		result = append(result, cjs)
	}
	return result
}

// resolveTarget：显式参数兼容多种形态；缺省=自动探测
func resolveTarget(arg string) []string {
	if arg == "" {
		return discover()
	}
	p := arg
	roots := []string{p}
	if strings.HasSuffix(strings.ToLower(p), ".app") {
		roots = []string{p, filepath.Join(p, "Contents")}
	}
	for _, root := range roots {
		cjs := filepath.Join(root, "resources", "glm", "zcode.cjs")
		info, err := os.Stat(cjs)
		if err == nil && !info.IsDir() {
			return []string{cjs}
		}
	}
	fail("[!] 指定路径下找不到 zcode.cjs：%s", arg)
	return nil
}

// resolveAsars：由 zcode.cjs 定位 app.asar（resources/app.asar）
func resolveAsars(cjsPaths []string) []string {
	var out []string
	for _, cjs := range cjsPaths {
		res := filepath.Dir(filepath.Dir(cjs)) // resources/glm/zcode.cjs -> resources
		asar := filepath.Join(res, "app.asar")
		info, err := os.Stat(asar)
		if err == nil && !info.IsDir() {
			out = append(out, asar)
		}
	}
	return out
}

// ---- 统一分发 ----

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
	}
	target := ""
	var patchNames []string
	var mode string
	var rest []string
	i := 0
	switch args[0] {
	case "restore":
		mode = "restore"
		rest = args[1:]
		return
	case "model-pull":
		runModelPull(args[1:])
		return
	case "check", "install", "revert":
		mode = args[0]
		if len(args) > 1 {
			target = args[1]
		}
	default:
		if len(args) >= 2 && (args[1] == "check" || args[1] == "install" || args[1] == "revert") {
			patchNames = []string{args[0]}
			mode = args[1]
			i = 2
		} else {
			usage()
		}
	}
	for ; i < len(args); i++ {
		a := args[i]
		switch {
		case target == "" && (filepath.Base(a) == "ZCode.app" || a == "app.asar" || strings.HasSuffix(a, "\\resources\\glm\\zcode.cjs") || strings.HasSuffix(a, "/resources/glm/zcode.cjs")):
			target = a
		case a == "--backup-dir":
			if i+1 < len(args) {
				restoreBackupDir = args[i+1]
				i++
			}
		case a == "--latest":
			restoreLatest = true
		default:
			rest = append(rest, a)
		}
	}
	if mode == "restore" {
		doRestore(rest)
		return
	}
	if len(patchNames) == 0 {
		patchNames = []string{kernelPatchName, chartPatchName, widthPatchName, tpsPatchName, pullerPatchName}
	}

	var cjsPaths []string
	for _, name := range patchNames {
		if name == modelPullName {
			runModelPull(rest)
			return
		}
	}
	if target != "" {
		cjsPaths = resolveTarget(target)
	} else {
		cjsPaths = discover()
	}
	if len(cjsPaths) == 0 {
		fail("[!] 未探测到任何 ZCode 安装；请把安装目录路径作为参数传入")
	}
	modeLabel := map[string]string{"check": "检查", "install": "打补丁", "revert": "还原"}[mode]
	fmt.Printf("=== %s（%d 处，模式：%s）===\n", strings.Join(patchNames, "+"), len(cjsPaths), modeLabel)

	for _, name := range patchNames {
		if name == kernelPatchName {
			for _, cjs := range cjsPaths {
				kernelPath = cjs
				switch mode {
				case "check":
					doKernelCheck()
				case "install":
					doKernelInstall()
				default:
					doKernelRevert()
				}
			}
			continue
		}
		for _, asar := range resolveAsars(cjsPaths) {
			asarPath = asar
			switch name {
			case tpsPatchName:
				if mode == "check" {
					doTPSCheck()
				} else if mode == "install" {
					doTPSInstall()
				} else {
					doTPSRevert()
				}
			case chartPatchName:
				if mode == "check" {
					doChartCheck()
				} else if mode == "install" {
					doChartInstall()
				} else {
					doChartRevert()
				}
			case widthPatchName:
				if mode == "check" {
					doWidthCheck()
				} else if mode == "install" {
					doWidthInstall()
				} else {
					doWidthRevert()
				}
			case pullerPatchName:
				if mode == "check" {
					doPullerCheck()
				} else if mode == "install" {
						doPullerInstall()
				} else {
					doPullerRevert()
				}
			}
		}
	}

	if mode != "check" && mode != "revert" {
		fmt.Println("=== 提示：完全退出并重启 ZCode 后生效；升级后需重新执行 ===")
	}
}