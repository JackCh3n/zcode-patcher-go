package main

// restore.go — 紧急还原：从干净备份整包恢复 app.asar / zcode.cjs（补丁异常致 ZCode 起不来时用）。

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

var (
	restoreBackupDir = `D:/ZCode-clean-backup`
	restoreLatest    bool
)

func findResources() string {
	if runtime.GOOS == "windows" {
		for _, root := range []string{`C:\Program Files\ZCode`, `D:\ZCode`} {
			if res := filepath.Join(root, "resources"); asarExists(res) && cjsExists(filepath.Join(res, "glm")) {
				return res
			}
		}
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			res := filepath.Join(v, "Programs", "ZCode", "resources")
			if asarExists(res) && cjsExists(filepath.Join(res, "glm")) {
				return res
			}
		}
	} else {
		for _, base := range []string{"/Applications/ZCode.app/Contents", "/opt/ZCode", "/usr/share/ZCode"} {
			res := filepath.Join(base, "resources")
			if asarExists(res) && cjsExists(filepath.Join(res, "glm")) {
				return res
			}
		}
	}
	return ""
}

func asarExists(res string) bool {
	info, err := os.Stat(filepath.Join(res, "app.asar"))
	return err == nil && !info.IsDir()
}

func cjsExists(glmDir string) bool {
	info, err := os.Stat(filepath.Join(glmDir, "zcode.cjs"))
	return err == nil && !info.IsDir()
}

// listBackups 列出可用备份，按时间倒序
type bk struct {
	ver   string
	asar  string
	cjs   string
	mtime time.Time
}

func listBackups(bdir string) []bk {
	entries, err := os.ReadDir(bdir)
	if err != nil {
		return nil
	}
	var out []bk
	var names []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "app.asar.") && strings.HasSuffix(n, ".clean") {
			names = append(names, n)
		}
	}
	sort.Slice(names, func(i, j int) bool { return names[i] > names[j] })
	for _, n := range names {
		ver := strings.TrimSuffix(strings.TrimPrefix(n, "app.asar."), ".clean")
		cjsPath := filepath.Join(bdir, fmt.Sprintf("zcode.cjs.%s.clean", ver))
		if _, err := os.Stat(cjsPath); err != nil {
			cjsPath = ""
		}
		st, _ := os.Stat(filepath.Join(bdir, n))
		out = append(out, bk{ver: ver, asar: filepath.Join(bdir, n), cjs: cjsPath, mtime: st.ModTime()})
	}
	return out
}

func doRestore(args []string) {
	// 解析参数
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--backup":
			doRestoreBackup()
			return
		case "--latest":
			restoreLatest = true
		case "--backup-dir":
			if i+1 < len(args) {
				restoreBackupDir = args[i+1]
				i++
			}
		default:
			fail("[!] 未知参数: %s", args[i])
		}
	}

	res := findResources()
	if res == "" {
		fail("[!] 未找到 ZCode 安装（可用 --backup-dir 或手动复制）")
	}
	tgtAsar := filepath.Join(res, "app.asar")
	tgtCjs := filepath.Join(res, "glm", "zcode.cjs")

	if _, err := os.Stat(restoreBackupDir); os.IsNotExist(err) {
		os.MkdirAll(restoreBackupDir, 0755)
	}
	backups := listBackups(restoreBackupDir)
	if len(backups) == 0 {
		fail("[!] %s 下没有可用备份；先运行 --backup 保存干净副本", restoreBackupDir)
	}

	pick := backups[0]
	if !restoreLatest {
		fmt.Printf("可用备份（%s）：\n", restoreBackupDir)
		for _, b := range backups {
			cjsHas := "无"
			if b.cjs != "" {
				cjsHas = "有"
			}
			fmt.Printf("  %-24s app.asar %13dB  zcode.cjs %s  %s\n",
				b.ver, fileSizeOf(b.asar), cjsHas, b.mtime.Local().Format("2006-01-02 15:04"))
		}
		fmt.Println("\n用 --latest 或 --version <版本> 执行还原")
		return
	}
	fmt.Printf("准备从备份 %s 还原：\n  源: %s\n  目标: %s\n", pick.ver, pick.asar, tgtAsar)
	copyFile(tgtAsar, pick.asar)
	fmt.Printf("  ✓ app.asar 已还原（%d 字节）\n", fileSizeOf(pick.asar))
	if pick.cjs != "" {
		copyFile(tgtCjs, pick.cjs)
		fmt.Printf("  ✓ zcode.cjs 已还原（%d 字节）\n", fileSizeOf(pick.cjs))
	} else {
		fmt.Println("  ⚠ 该版本无 zcode.cjs 备份，内核未改回")
	}
	// 清理本机补丁产物，避免还原后 sidecar 与实际不符
	filepath.Walk(res, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.Contains(name, ".bak") || strings.Contains(name, "-patch.json") ||
			strings.HasSuffix(name, ".tps-tmp") {
			os.Remove(p)
			fmt.Printf("  · 已清理 %s\n", name)
		}
		return nil
	})
	fmt.Println("\n✓ 还原完成，请启动 ZCode 验证")
}

func doRestoreBackup() {
	res := findResources()
	if res == "" {
		fail("[!] 未找到 ZCode 安装")
	}
	tgtAsar := filepath.Join(res, "app.asar")
	tgtCjs := filepath.Join(res, "glm", "zcode.cjs")
	ver := "manual-" + time.Now().Format("20060102-150405")
	os.MkdirAll(restoreBackupDir, 0755)
	copyFile(filepath.Join(restoreBackupDir, fmt.Sprintf("app.asar.%s.clean", ver)), tgtAsar)
	copyFile(filepath.Join(restoreBackupDir, fmt.Sprintf("zcode.cjs.%s.clean", ver)), tgtCjs)
	fmt.Printf("[+] 已备份当前安装 → %s（版本标记 %s）\n    app.asar: %d 字节\n", restoreBackupDir, ver, fileSizeOf(tgtAsar))
}

func fileSizeOf(p string) int64 {
	info, err := os.Stat(p)
	if err != nil {
		return -1
	}
	return info.Size()
}