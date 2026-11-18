package main

// patches_other.go — 思考档位透传（内核）+ 用量页去截断 + 模型弹窗加宽。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	kernelPath         = ""
	kernelPatchName    = "kernel"
	chartPatchName     = "usage-chart"
	widthPatchName     = "model-width"
	pullerPatchName    = "model-puller"
	modelPullName      = "model-pull"
	kernelMarker       = []byte("zCfgEffort")
	kernelBakSuffix    = ".cjs.bak"
	chartSideSuffix    = ".chart-patch.json"
	widthSideSuffix    = ".width-patch.json"
)

// ---- ① 思考档位透传（zcode.cjs 内核，锚点匹配 + 结构提取）----

// 兜底合成器：与 python HELPER 完全一致
const kernelHelper = `function zCfgEffort(e){let t=String(e).toLowerCase();if(t==="disabled"||t==="none"||t==="off"||t==="nothink")return{anthropic:{thinking:{type:"disabled"}},openaiCompatible:{reasoningEffort:"off"}};if(t==="enabled"||t==="on")return{anthropic:{effort:"high",thinking:{type:"enabled",budgetTokens:16e3}},openaiCompatible:{reasoningEffort:"high"}};let r={low:4e3,medium:8e3,high:16e3,xhigh:32e3,max:32e3}[t]??16e3,n={anthropic:{thinking:{type:"enabled",budgetTokens:r}},openaiCompatible:{},openai:{}};["low","medium","high","xhigh","max"].includes(t)&&(n.anthropic.effort=t);["none","minimal","low","medium","high","xhigh","max"].includes(t)&&(n.openaiCompatible.reasoningEffort=t,n.openai.reasoningEffort=t);return n}`

var kernelAnchors = map[string]string{
	"3.8.1": `function sD(e,t,r){if(!e)return;let n=G_e(e,r);if(!n?.enabled||n.levels.length===0)return;let o=t?.trim(),i=Fgo(e,o,n.levels);if(o&&!i)return;let a=i??gXe(n);return a?{level:a,providerOptions:n.providerOptionsByLevel?.[a]}:void 0}`,
	"3.9.1": `function AD(e,t,r){if(!e)return;let n=Zye(e,r);if(!n?.enabled||n.levels.length===0)return;let o=t?.trim(),i=fxo(e,o,n.levels);if(o&&!i)return;let a=i??utt(n);return a?{level:a,providerOptions:n.providerOptionsByLevel?.[a]}:void 0}`,
	"3.9.2": `function RD(e,t,r){if(!e)return;let n=Xye(e,r);if(!n?.enabled||n.levels.length===0)return;let o=t?.trim(),i=Sxo(e,o,n.levels);if(o&&!i)return;let a=i??ptt(n);return a?{level:a,providerOptions:n.providerOptionsByLevel?.[a]}:void 0}`,
	"3.11.2": `function mN(e,t,r){if(!e)return;let n=k2e(e,r);if(!n?.enabled||n.levels.length===0)return;let o=t?.trim(),i=CIo(e,o,n.levels);if(o&&!i)return;let s=i??_nt(n);return s?{level:s,providerOptions:n.providerOptionsByLevel?.[s]}:void 0}`,
}

// kernelReplacementFor：在锚点函数里定位「表查档位」的返回表达式，
// 把查表结果追加 ??zCfgEffort(VAR) 兜底。与 python 版 replacement_for 语义一致。
// Go 的 regexp（RE2）不支持反向引用，故用字符串结构特征定位。
func kernelReplacementFor(anchor string) (string, error) {
	keyIdx := strings.Index(anchor, "providerOptionsByLevel?.[")
	if keyIdx < 0 {
		return "", fmt.Errorf("锚点无查表表达式：%s", anchor[len(anchor)-160:])
	}
	varStart := -1
	for k := keyIdx - len("return "); k >= 0; k-- {
		if strings.HasPrefix(anchor[k:], "return ") {
			varStart = k + len("return ")
			break
		}
	}
	if varStart < 0 {
		return "", fmt.Errorf("锚点返回表达式未找到：%s", anchor[len(anchor)-160:])
	}
	j := varStart
	for j < len(anchor) && isWordByte(anchor[j]) {
		j++
	}
	varS := anchor[varStart:j]
	if !strings.Contains(anchor, "level:"+varS+",providerOptions:n.providerOptionsByLevel?.["+varS+"]") {
		return "", fmt.Errorf("锚点形态校验失败：%s", anchor[len(anchor)-160:])
	}
	patchedReturn := fmt.Sprintf(
		"return %s?{level:%s,providerOptions:n.providerOptionsByLevel?.[%s]??zCfgEffort(%s)}:void 0}",
		varS, varS, varS, varS)
	return kernelHelper + anchor[:varStart-len("return ")] + patchedReturn, nil
}

func isWordByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func doKernelCheck() {
	data, err := os.ReadFile(kernelPath)
	if err != nil {
		fail("[!] 无权限读取 %s（ZCode 是否在运行？Program Files 需要管理员）", kernelPath)
	}
	bak := kernelPath + kernelBakSuffix
	state := "已打"
	if bytesIndex(data, kernelMarker) < 0 {
		state = "未打"
		if fileExists(bak) {
			state = "已还原/未打"
		}
	}
	fmt.Printf("[*] %s\n    {size: %d} 字节 | 补丁: %s | 备份: %s\n", kernelPath, len(data), state, yesNo(fileExists(bak)))
}

func doKernelInstall() {
	data, err := os.ReadFile(kernelPath)
	if err != nil {
		fmt.Printf("[!] 无权限读取 %s（ZCode 是否在运行？）\n", kernelPath)
		return
	}
	bak := kernelPath + kernelBakSuffix
	if bytesIndex(data, kernelMarker) >= 0 {
		fmt.Println("[=] 已打过补丁，跳过")
		return
	}
	matched := []string{}
	for ver, a := range kernelAnchors {
		if bytesCount(data, []byte(a)) == 1 {
			matched = append(matched, ver)
		}
	}
	verSeen := map[string]bool{}
	uniq := []string{}
	for _, v := range matched {
		if !verSeen[v] {
			verSeen[v] = true
			uniq = append(uniq, v)
		}
	}
	if len(uniq) != 1 {
		detail := ""
		for ver, a := range kernelAnchors {
			detail += fmt.Sprintf("%s=%d, ", ver, bytesCount(data, []byte(a)))
		}
		fmt.Printf("[!] 锚点匹配异常（%s期望恰有一版=1），版本可能不在已支持列表，按文档重新提取锚点后添加\n", strings.TrimRight(detail, ", "))
		return
	}
	ver := uniq[0]
	anchor := kernelAnchors[ver]
	repl, err := kernelReplacementFor(anchor)
	if err != nil {
		fmt.Printf("[!] %v\n", err)
		return
	}
	if !fileExists(bak) {
		copyFile(kernelPath, bak) // 原文件 → 备份（勿颠倒）
	}
	patched := []byte(strings.Replace(string(data), anchor, repl, 1))
	if err := os.WriteFile(kernelPath, patched, 0644); err != nil {
		fmt.Printf("[!] 无权限写入（Program Files 需管理员），未修改\n")
		return
	}
	ok := bytesIndex(patched, kernelMarker) >= 0
	msg := "写入成功"
	if !ok {
		msg = "写入失败"
	}
	fmt.Printf("    [+] 匹配版本锚点: %s\n    [%s] 补丁%s\n", ver, map[bool]string{true: "+", false: "!"}[ok], msg)
}

func doKernelRevert() {
	bak := kernelPath + kernelBakSuffix
	if !fileExists(bak) {
		fmt.Println("    [.] 没有备份，跳过")
		return
	}
	copyFile(kernelPath, bak)
	fmt.Printf("    [+] 已从备份还原 %s\n", kernelPath)
}

// ---- ② 用量页去截断（asar 内同长度原地改字节）----

type inPlacePatch struct {
	key         string
	pattern     []byte
	replacement []byte
	desc        string
}

var chartPatches = []inPlacePatch{
	{"AppUsageDailyModelTrendChart", []byte("n.models.slice(0,6)"), []byte("n.models"), "每日趋势图：去掉 Top6 截断，全部模型出线"},
	{"AppUsageModelUsagePieChart", []byte("i=n.length>Q,a=i?Q-1:Q"), []byte("i=!1,a=1/0"), "模型用量饼图：去掉 Top5+其他模型 合并，全部模型出块"},
}

func findEntryByKey(a *asarFile, key string) ([]pathEnt, bool) {
	var hits []pathEnt
	for _, pe := range a.entries() {
		if strings.Contains(pe.path, key) {
			hits = append(hits, pe)
		}
	}
	return hits, len(hits) == 1
}

func doChartCheck() {
	a := cachedReadAsar(asarPath)
	side := asarPath + chartSideSuffix
	saved := loadSidecarPatches(side, fileSize(asarPath))
	fmt.Printf("[*] %s\n", asarPath)
	for _, item := range chartPatches {
		hits, ok := findEntryByKey(a, item.key)
		if !ok {
			fmt.Printf("    [!] %s 命中 %d 个文件（期望 1），该项跳过\n", item.key, len(hits))
			continue
		}
		raw := a.entryBytes(hits[0].ent)
		if bytesIndex(raw, item.pattern) >= 0 {
			fmt.Printf("    [ ] %s | 未打（%s）\n", filepath.Base(hits[0].path), item.desc)
		} else {
			fmt.Printf("    [=] %s | 已打（%s）\n", filepath.Base(hits[0].path), item.desc)
		}
	}
	fmt.Printf("    sidecar: %s | 备份记录: %d 条\n", yesNo(fileExists(side)), len(saved))
}

func doChartInstall() {
	a := cachedReadAsar(asarPath)
	side := asarPath + chartSideSuffix
	saved := loadSidecarPatches(side, fileSize(asarPath))
	changed := false
	for _, item := range chartPatches {
		hits, ok := findEntryByKey(a, item.key)
		if !ok {
			fmt.Printf("    [!] %s 命中 %d 个文件（期望 1），该项跳过\n", item.key, len(hits))
			continue
		}
		path := hits[0].path
		raw := a.entryBytes(hits[0].ent)
		if bytesIndex(raw, item.pattern) < 0 {
			fmt.Printf("    [=] %s | 已打（%s）\n", filepath.Base(path), item.desc)
			syncIntegrity(asarPath, path, raw)
			continue
		}
		if bytesCount(raw, item.pattern) != 1 {
			fmt.Printf("    [!] %s | 截断表达式出现 %d 次（期望 1），拒绝盲改\n", filepath.Base(path), bytesCount(raw, item.pattern))
			continue
		}
		new := bytesReplaceOnce(raw, item.pattern, item.replacement)
		padded := append(new, make([]byte, len(raw)-len(new))...)
		hasOff := false
		offs := itoa(int(a.dataStart + atoi(hits[0].ent["offset"].(string))))
		for _, r := range saved {
			if off, _ := r["offset"].(string); off == offs {
				hasOff = true
			}
		}
		if !hasOff {
			saved = append(saved, map[string]interface{}{
				"path":         path,
				"offset":       itoa(int(a.dataStart + atoi(hits[0].ent["offset"].(string)))),
				"size":         json.Number(itoa(len(raw))),
				"asar_size":    json.Number(itoa(int(fileSize(asarPath)))),
				"original_b64": b64encode(raw),
			})
		}
		off := int64(a.dataStart + atoi(hits[0].ent["offset"].(string)))
		f, err := os.OpenFile(asarPath, os.O_RDWR, 0644)
		if err != nil {
			continue
		}
		defer f.Close()
		f.WriteAt(padded, off)
		dropAsarCache(asarPath)
		f.Seek(off, 0)
		back := make([]byte, len(raw))
		f.Read(back)
		ok = bytesIndex(back, item.pattern) < 0 && len(back) == len(raw)
		if ok {
			syncIntegrity(asarPath, path, padded)
		}
		fmt.Printf("    [%s] %s | %s写入%s\n", map[bool]string{true: "+", false: "!"}[ok], filepath.Base(path), item.desc, map[bool]string{true: "成功", false: "失败"}[ok])
		changed = true
	}
	if changed {
		sidecarSave(side, saved)
		fmt.Printf("    原始字节备份: %s\n", filepath.Base(side))
	}
}

func doChartRevert() {
	side := asarPath + chartSideSuffix
	saved := loadSidecarPatches(side, fileSize(asarPath))
	if len(saved) == 0 {
		fmt.Printf("[.] %s\n    没有当前版本的 sidecar 备份，跳过\n", asarPath)
		return
	}
	for _, rec := range saved {
		offset, _ := rec["offset"].(string)
		f, err := os.OpenFile(asarPath, os.O_RDWR, 0644)
		if err != nil {
			continue
		}
		f.Seek(int64(atoi(offset)), 0)
		orig := b64decode(rec["original_b64"].(string))
		f.Write(orig)
		f.Close()
		path, _ := rec["path"].(string)
		syncIntegrity(asarPath, path, orig)
	}
	os.Remove(side)
	fmt.Printf("[+] %s\n    已还原 %d 处原始字节\n", asarPath, len(saved))
}

// ---- ③ 模型弹窗加宽（w-48 → w-80，同长度原地）----

var widthPatches = []inPlacePatch{
	{"", []byte("j??`w-48`"), []byte("j??`w-80`"), "渠道子菜单宽度 192px → 320px"},
	{"", []byte(":`w-48 max-h-72 overflow-y-auto`"), []byte(":`w-80 max-h-72 overflow-y-auto`"), "模型弹窗宽度 192px → 320px"},
}

func doWidthCheck() {
	a := cachedReadAsar(asarPath)
	side := asarPath + widthSideSuffix
	saved := loadSidecarPatches(side, fileSize(asarPath))
	fmt.Printf("[*] %s\n", asarPath)
	for _, item := range widthPatches {
		hits := scanWidthHits(a, item.pattern)
		if len(hits) == 0 {
			done := scanWidthHits(a, item.replacement)
			if len(done) == 1 {
				fmt.Printf("    [=] %s | 已打（%s）\n", filepath.Base(done[0].pe.path), item.desc)
			} else {
				fmt.Printf("    [!] %s | 未找到锚点（版本结构可能已变），跳过\n", item.desc)
			}
			continue
		}
		if len(hits) > 1 || hits[0].cnt != 1 {
			fmt.Printf("    [!] %s | 锚点在 %d 处出现（期望 1），拒绝盲改\n", item.desc, len(hits))
			continue
		}
		fmt.Printf("    [ ] %s | 未打（%s）\n", filepath.Base(hits[0].pe.path), item.desc)
	}
	fmt.Printf("    sidecar: %s | 备份记录: %d 条\n", yesNo(fileExists(side)), len(saved))
}

type widthHit struct {
	pe  pathEnt
	cnt int
}

func scanWidthHits(a *asarFile, needle []byte) []widthHit {
	var out []widthHit
	for _, pe := range a.entries() {
		if !strings.HasPrefix(pe.path, "out/renderer/") || !strings.HasSuffix(pe.path, ".js") {
			continue
		}
		size, _ := pe.ent["size"].(json.Number)
		sizeN, _ := size.Int64()
		if sizeN <= 1000 || sizeN > 80_000_000 {
			continue
		}
		raw := a.entryBytes(pe.ent)
		// stdlib bytes.Index 快速判存在；计数只在命中文件上做（极少）
		if bytes.Index(raw, needle) < 0 {
			continue
		}
		out = append(out, widthHit{pe, bytes.Count(raw, needle)})
	}
	return out
}

func doWidthInstall() {
	a := cachedReadAsar(asarPath)
	side := asarPath + widthSideSuffix
	saved := loadSidecarPatches(side, fileSize(asarPath))
	changed := false
	for _, item := range widthPatches {
		hits := scanWidthHits(a, item.pattern)
		if len(hits) == 0 {
			done := scanWidthHits(a, item.replacement)
			if len(done) == 1 && done[0].cnt == 1 {
				fmt.Printf("    [=] %s | 已打（%s）\n", filepath.Base(done[0].pe.path), item.desc)
				syncIntegrity(asarPath, done[0].pe.path, a.entryBytes(done[0].pe.ent))
				continue
			}
			fmt.Printf("    [!] %s | 未找到锚点（版本结构可能已变），跳过\n", item.desc)
			continue
		}
		if len(hits) > 1 || hits[0].cnt != 1 {
			fmt.Printf("    [!] %s | 锚点出现异常，拒绝盲改\n", item.desc)
			continue
		}
		pe := hits[0].pe
		path := pe.path
		raw := a.entryBytes(pe.ent)
		new := bytesReplaceOnce(raw, item.pattern, item.replacement)
		if len(new) != len(raw) {
			fmt.Printf("    [!] %s | 替换不等长（%d→%d），拒绝改写\n", filepath.Base(path), len(raw), len(new))
			continue
		}
		off := itoa(int(a.dataStart + atoi(pe.ent["offset"].(string))))
		hasOff := false
		for _, r := range saved {
			if o, _ := r["offset"].(string); o == off {
				hasOff = true
			}
		}
		if !hasOff {
			saved = append(saved, map[string]interface{}{
				"path": path, "offset": off,
				"size": json.Number(itoa(len(raw))),
				"asar_size": json.Number(itoa(int(fileSize(asarPath)))),
				"original_b64": b64encode(raw),
			})
		}
		f, err := os.OpenFile(asarPath, os.O_RDWR, 0644)
		if err != nil {
			continue
		}
		defer f.Close()
		pos := int64(a.dataStart + atoi(pe.ent["offset"].(string)))
		f.WriteAt(new, pos)
		f.Seek(pos, 0)
		back := make([]byte, len(raw))
		f.Read(back)
		ok := bytesIndex(back, item.pattern) < 0 && len(back) == len(raw)
		if ok {
			syncIntegrity(asarPath, path, new)
		}
		fmt.Printf("    [%s] %s | %s写入%s\n", map[bool]string{true: "+", false: "!"}[ok], filepath.Base(path), item.desc, map[bool]string{true: "成功", false: "失败"}[ok])
		changed = true
	}
	if changed {
		sidecarSave(side, saved)
		fmt.Printf("    原始字节备份: %s\n", filepath.Base(side))
	}
}

func doWidthRevert() {
	side := asarPath + widthSideSuffix
	saved := loadSidecarPatches(side, fileSize(asarPath))
	if len(saved) == 0 {
		fmt.Printf("[.] %s\n    没有当前版本的 sidecar 备份，跳过\n", asarPath)
		return
	}
	for _, rec := range saved {
		offset, _ := rec["offset"].(string)
		f, err := os.OpenFile(asarPath, os.O_RDWR, 0644)
		if err != nil {
			continue
		}
		f.Seek(int64(atoi(offset)), 0)
		orig := b64decode(rec["original_b64"].(string))
		f.Write(orig)
		f.Close()
		path, _ := rec["path"].(string)
		syncIntegrity(asarPath, path, orig)
	}
	os.Remove(side)
	fmt.Printf("[+] %s\n    已还原 %d 处原始宽度字节\n", asarPath, len(saved))
}