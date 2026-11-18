package main

// patches_tps.go — TPS 状态胶囊注入（asar 重打包级）。
// 移植自 python 版 process_tps_footer，含备份保鲜修复与脚本热更新。

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	tpsPatchName   = "tps"
	tpsScriptPath = "out/renderer/zcode-tps.js"
	tpsIndexPath  = "out/renderer/index.html"
	tpsTag        = `<script src="./zcode-tps.js"></script>`
	sideSuffix    = ".tps-patch.json"
	bakSuffix     = ".tps.bak"
)

//go:embed embed/zcode-tps.js
var tpsScript []byte

func sidecarOf() string { return asarPath + sideSuffix }
func bakOf() string     { return asarPath + bakSuffix }

func sidecarValid() (map[string]interface{}, bool) {
	data, err := os.ReadFile(sidecarOf())
	if err != nil {
		return nil, false
	}
	var rec map[string]interface{}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if dec.Decode(&rec) != nil {
		return nil, false
	}
	cur := fileSize(asarPath)
	if n, _ := rec["asar_size"].(json.Number); n.String() != itoa(int(cur)) {
		return nil, false
	}
	return rec, true
}

func doTPSCheck() {
	a := cachedReadAsar(asarPath)
	idxEnt, ok := a.findEntry(tpsIndexPath)
	if !ok {
		fmt.Printf("[!] %s\n    未找到 %s，版本结构可能已变\n", asarPath, tpsIndexPath)
		return
	}
	idxBytes := a.entryBytes(idxEnt)
	tagged := strings.Contains(string(idxBytes), tpsTag)
	_, hasScript := a.findEntry(tpsScriptPath)
	installed := tagged && hasScript
	_, savedOK := sidecarValid()
	bakStale := fileExists(bakOf()) && fileSize(bakOf()) != fileSize(asarPath)
	state := "未打"
	if installed {
		state = "已打"
	} else if tagged {
		state = "不完整（index.html 有 tag 但缺脚本条目）"
	}
	bakDesc := "无"
	if fileExists(bakOf()) {
		if installed && bakStale {
			bakDesc = "有（补丁前原件）"
		} else if !installed && bakStale {
			bakDesc = "有（陈旧：为旧版 ZCode 的 asar，重打时自动刷新）"
		} else {
			bakDesc = "有"
		}
	}
	fmt.Printf("[*] %s\n    TPS 统计栏注入: %s | sidecar: %s | 备份: %s\n", asarPath, state, yesNo(savedOK), bakDesc)
}

func doTPSInstall() {
	a := cachedReadAsar(asarPath)
	side := sidecarOf()
	bak := bakOf()
	asarSize := fileSize(asarPath)

	idxEnt, ok := a.findEntry(tpsIndexPath)
	if !ok {
		fmt.Printf("[!] %s\n    未找到 %s，版本结构可能已变，跳过\n", asarPath, tpsIndexPath)
		return
	}
	idxBytes := a.entryBytes(idxEnt)
	tagged := strings.Contains(string(idxBytes), tpsTag)
	scriptEnt, hasScript := a.findEntry(tpsScriptPath)
	installed := tagged && hasScript
	saved, savedOK := sidecarValid()

	if fileExists(bak) && fileSize(bak) != asarSize {
		fmt.Printf("[~] %s\n    备份为旧版 ZCode 的 asar（%d ≠ %d 字节），刷新为新版原件\n", asarPath, fileSize(bak), asarSize)
		os.Remove(bak)
	}

	if installed {
		var cur []byte
		if hasScript {
			cur = a.entryBytes(scriptEnt)
		}
		if string(cur) == string(tpsScript) {
			fmt.Printf("[=] %s\n    已打 TPS 注入（脚本同源），跳过\n", asarPath)
			return
		}
		newSize := repack(asarPath, map[string][]byte{tpsScriptPath: tpsScript}, nil)
		refreshSidecarBySuffix(asarPath, ".chart-patch.json")
	refreshSidecarBySuffix(asarPath, ".width-patch.json")
		fmt.Printf("[+] %s\n    TPS 脚本已热更新（zcode-tps.js %d 字节，其余组件不变；新大小 %d 字节）\n", asarPath, len(tpsScript), newSize)
		return
	}

	if strings.Count(string(idxBytes), "</body>") != 1 {
		fmt.Printf("[!] %s\n    index.html 的 </body> 出现 %d 次（期望 1），拒绝盲改\n", asarPath, strings.Count(string(idxBytes), "</body>"))
		return
	}

	if !fileExists(bak) {
		copyFile(bak, asarPath)
	}

	newIdx := strings.Replace(string(idxBytes), "</body>", tpsTag+"</body>", 1)
	newSize := repack(asarPath, map[string][]byte{
		tpsIndexPath:  []byte(newIdx),
		tpsScriptPath: tpsScript,
	}, nil)

	sideRec := map[string]interface{}{
		"asar_size":          json.Number(itoa(int(newSize))),
		"index_path":         tpsIndexPath,
		"script_entry":       tpsScriptPath,
		"index_original_b64": "",
	}
	if savedOK {
		if b64, ok := saved["index_original_b64"].(string); ok && b64 != "" {
			sideRec["index_original_b64"] = b64 // 沿用 python 版记录的原件
		}
	}
	if b, err := marshalCompact(sideRec); err == nil {
		os.WriteFile(side, b, 0644)
	}
	refreshSidecarBySuffix(asarPath, ".chart-patch.json")
	refreshSidecarBySuffix(asarPath, ".width-patch.json")
	fmt.Printf("[+] %s\n    TPS 统计栏注入完成（zcode-tps.js %d 字节 -> %s，index.html 已挂载；新大小 %d 字节）\n    原件备份: %s | 记录: %s\n",
		asarPath, len(tpsScript), tpsScriptPath, newSize, filepath.Base(bak), filepath.Base(side))
}

func doTPSRevert() {
	a := cachedReadAsar(asarPath)
	side := sidecarOf()
	bak := bakOf()

	idxEnt, ok := a.findEntry(tpsIndexPath)
	if !ok {
		fmt.Printf("[!] %s\n    未找到 %s，跳过\n", asarPath, tpsIndexPath)
		return
	}
	idxBytes := a.entryBytes(idxEnt)
	tagged := strings.Contains(string(idxBytes), tpsTag)
	_, hasScript := a.findEntry(tpsScriptPath)
	installed := tagged && hasScript
	saved, savedOK := sidecarValid()

	if !installed && !savedOK {
		fmt.Printf("[.] %s\n    未打 TPS 注入，跳过\n", asarPath)
		return
	}
	stripped := strings.ReplaceAll(strings.ReplaceAll(string(idxBytes), tpsTag+"\n", ""), tpsTag, "")
	if stripped == string(idxBytes) && savedOK {
		if b64, ok := saved["index_original_b64"].(string); ok && b64 != "" {
			stripped = b64 // python 版以 base64 存原件；Go 版兼容解码
		}
	}
	var dec []byte
	if isB64(stripped) {
		dec = b64decode(stripped)
	} else {
		dec = []byte(stripped)
	}
	newSize := repack(asarPath, map[string][]byte{tpsIndexPath: dec},
		map[string]bool{tpsScriptPath: true})
	os.Remove(side)
	os.Remove(bak)
	refreshSidecarBySuffix(asarPath, ".chart-patch.json")
	refreshSidecarBySuffix(asarPath, ".width-patch.json")
	fmt.Printf("[+] %s\n    已移除 TPS 注入（新大小 %d 字节，备份已清理）\n", asarPath, newSize)
}

func yesNo(b bool) string {
	if b {
		return "有"
	}
	return "无"
}

func isB64(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=') {
			return false
		}
	}
	return true
}

func b64decode(s string) []byte {
	const tbl = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out []byte
	var buf uint32
	var bits int
	for _, c := range s {
		if c == '=' {
			break
		}
		i := strings.IndexRune(tbl, c)
		if i < 0 {
			continue
		}
		buf = buf<<6 | uint32(i)
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(buf>>bits))
		}
	}
	return out
}

func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}