package main

// patches_puller.go — 模型拉取按钮注入（asar 重打包级）
// renderer 脚本 vendored 自 scripts/zcode-model-puller.js (HHQ-666/zcode-model-puller, MIT)，
// preload 桥与 main IPC handler 在此内置。锚点用语义字符串 + \w+ 捕获压缩符号，跨版本存活。

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	pullerScriptPath = "out/renderer/zcode-model-puller.js"
	pullerIndexPath  = "out/renderer/index.html"
	pullerPreloadPath = "out/preload/index.cjs"
	pullerMainPath   = "out/main/index.js"
	pullerSideSuffix = ".puller-patch.json"
	pullerBakSuffix  = ".puller.bak"
)

//go:embed embed/zcode-model-puller.js
var pullerScript []byte

var pullerTag        = `<script type="module" src="./zcode-model-puller.js"></script>`
var pullerMarker    = []byte("zcode:read-model-config")
var pullerPreloadRe = regexp.MustCompile(`(\w+)\.contextBridge\.exposeInMainWorld\("zcode",\{`)
var pullerMainRe    = regexp.MustCompile(`(\w+)\.handle\(\w+\.SaveMcpToUserDirectory`)
var pullerPreloadStripRe = regexp.MustCompile(
	`readConfigFile:\(\)=>\w+\.ipcRenderer\.invoke\("zcode:read-model-config"\),` +
		`writeConfigFile:t=>\w+\.ipcRenderer\.invoke\("zcode:write-model-config",t\),` +
		`fetchModelsFromUrl:\(t,n\)=>\w+\.ipcRenderer\.invoke\("zcode:fetch-models-from-url",\{baseUrl:t,apiKey:n\}\),`)
var pullerMainStripRe = regexp.MustCompile(`\w+\.handle\("zcode:read-model-config"`)

func pullerSidecarValid() (map[string]interface{}, bool) {
	data, err := os.ReadFile(asarPath + pullerSideSuffix)
	if err != nil {
		return nil, false
	}
	var rec map[string]interface{}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if dec.Decode(&rec) != nil {
		return nil, false
	}
	if n, _ := rec["asar_size"].(json.Number); n.String() != itoa(int(fileSize(asarPath))) {
		return nil, false
	}
	return rec, true
}

func pullerPreloadInjection(alias string) string {
	return fmt.Sprintf(
		`readConfigFile:()=>%s.ipcRenderer.invoke("zcode:read-model-config"),`+
			`writeConfigFile:t=>%s.ipcRenderer.invoke("zcode:write-model-config",t),`+
			`fetchModelsFromUrl:(t,n)=>%s.ipcRenderer.invoke("zcode:fetch-models-from-url",{baseUrl:t,apiKey:n}),`,
		alias, alias, alias)
}

func pullerMainInjection(alias string) string {
	readH := fmt.Sprintf(`%s.handle("zcode:read-model-config",async()=>{try{let{default:e}=await import("node:fs"),{default:t}=await import("node:path"),{default:n}=await import("node:os");let r=t.join(n.homedir(),".zcode","v2","config.json");return{success:!0,data:JSON.parse(e.readFileSync(r,"utf-8"))}}catch(e){return{success:!1,error:String(e)}}});\n`, alias)
	writeH := fmt.Sprintf(`%s.handle("zcode:write-model-config",async(e,t)=>{try{if(typeof t!="object"||t===null)throw new Error("invalid config payload");let{default:n}=await import("node:fs"),{default:r}=await import("node:path"),{default:i}=await import("node:os");let o=r.join(i.homedir(),".zcode","v2","config.json");try{n.writeFileSync(o+".puller-bak",n.readFileSync(o,"utf-8"))}catch(a){}n.writeFileSync(o,JSON.stringify(t,null,2),"utf-8");return{success:!0}}catch(n){return{success:!1,error:String(n)}}});\n`, alias)
	fetchH := fmt.Sprintf(`%s.handle("zcode:fetch-models-from-url",async(e,t)=>{try{let{default:ht}=await import("node:https"),{default:hh}=await import("node:http");let u=(t&&t.baseUrl||"").trim().replace(/\/+$/,""),k=(t&&t.apiKey||"").trim();if(!u)return{success:!1,error:"baseUrl 为空"};let cs=[];if(u.endsWith("/v1")){cs.push(u+"/models");cs.push(u.replace(/\/v1$/,"")+"/models")}else{cs.push(u+"/v1/models");cs.push(u+"/models")}if(u.endsWith("/api"))cs.unshift(u+"/v1/models");for(let cur of cs){try{let res=await new Promise(resolve=>{let mod=cur.startsWith("https:")?ht:hh;let hs={"User-Agent":"ZCode","Accept":"application/json"};if(k){hs.Authorization="Bearer "+k;hs["x-api-key"]=k}let req=mod.request(cur,{method:"GET",headers:hs,timeout:8000},r=>{let b="";r.on("data",c=>b+=c);r.on("end",()=>{if(r.statusCode>=200&&r.statusCode<300){try{let j=JSON.parse(b);let l=Array.isArray(j)?j:Array.isArray(j.data)?j.data:Array.isArray(j.models)?j.models:[];let ids=[],metas={};for(let it of l){let id,ctx=0,out=0,eff=null,def=null;if(typeof it=="string")id=it.trim();else if(it){id=(it.id||it.name||"").trim();ctx=+it.context_length||+it.max_input_tokens||0;out=+it.max_output_tokens||+it.max_completion_tokens||+(it.top_provider?it.top_provider.max_completion_tokens:0)||0;if(Array.isArray(it.supported_efforts))eff=it.supported_efforts.filter(x=>typeof x=="string"&&x.trim());if(typeof it.default_effort=="string")def=it.default_effort;}if(id&&!metas[id]){metas[id]={context:ctx,output:out,efforts:eff,def:def};ids.push(id)}}if(ids.length>0)return resolve({success:!0,models:ids,metas:metas})}catch(e){}}resolve(null)})});req.on("error",()=>resolve(null));req.on("timeout",()=>{req.destroy();resolve(null)});req.end()});if(res&&res.success)return res}catch(e){}}return{success:!1,error:"未能获取到模型列表，请检查 Base URL 和 API Key"}}catch(e){return{success:!1,error:String(e)}}});\n`, alias)
	return readH + writeH + fetchH
}

type pullerState struct {
	injected bool
	synced   bool
	clean    []byte // 能安全剥离时的原始内容；无法剥离时 nil
	alias    string
}

func pullerPreloadState(blob []byte) pullerState {
	if blob == nil {
		return pullerState{}
	}
		a := pullerPreloadRe.FindSubmatchIndex(blob)
		if a == nil {
			return pullerState{}
		}
		alias := string(blob[a[2]:a[3]])
		if bytesIndex(blob, pullerMarker) < 0 {
			return pullerState{injected: false, synced: false, clean: blob, alias: alias}
		}
		m := pullerPreloadStripRe.FindSubmatchIndex(blob)
		if m == nil {
			return pullerState{injected: true, synced: false, clean: nil, alias: alias}
		}
		stripped := append([]byte{}, blob[:m[0]]...)
		stripped = append(stripped, blob[m[1]:]...)
		synced := false
		if string(blob[m[0]:m[1]]) == pullerPreloadInjection(alias) {
			synced = true
		}
	return pullerState{injected: true, synced: synced, clean: stripped, alias: alias}
}

func pullerMainState(blob []byte) pullerState {
	if blob == nil {
		return pullerState{}
	}
	a := pullerMainRe.FindSubmatchIndex(blob)
	if a == nil {
		return pullerState{}
	}
	alias := string(blob[a[2]:a[3]])
	if bytesIndex(blob, pullerMarker) < 0 {
		return pullerState{injected: false, synced: false, clean: blob, alias: alias}
	}
	m := pullerMainStripRe.FindSubmatchIndex(blob)
	if m == nil || m[0] >= a[0] {
		return pullerState{injected: true, synced: false, clean: nil, alias: alias}
	}
	stripped := append([]byte{}, blob[:m[0]]...)
	stripped = append(stripped, blob[a[0]:]...)
	synced := false
	if string(blob[m[0]:a[0]]) == pullerMainInjection(alias) {
		synced = true
	}
	return pullerState{injected: true, synced: synced, clean: stripped, alias: alias}
}

func doPullerCheck() {
	a := cachedReadAsar(asarPath)
	bak := asarPath + pullerBakSuffix
	_, savedOK := pullerSidecarValid()

	idxEnt, ok := a.findEntry(pullerIndexPath)
	if !ok {
		fmt.Printf("[!] %s\n    未找到 %s，版本结构可能已变，跳过\n", asarPath, pullerIndexPath)
		return
	}
	idxBytes := a.entryBytes(idxEnt)
	scriptEnt, scriptPresent := a.findEntry(pullerScriptPath)
	preBlob, mainBlob := []byte(nil), []byte(nil)
	if ent, ok := a.findEntry(pullerPreloadPath); ok {
		preBlob = a.entryBytes(ent)
	}
	if ent, ok := a.findEntry(pullerMainPath); ok {
		mainBlob = a.entryBytes(ent)
	}
	preSt := pullerPreloadState(preBlob)
	mainSt := pullerMainState(mainBlob)
	installed := scriptPresent && strings.Contains(string(idxBytes), pullerTag) && preSt.injected && mainSt.injected

	curScript := []byte(nil)
	if scriptPresent {
		curScript = a.entryBytes(scriptEnt)
	}
	oldScript := len(curScript) > 0 && string(curScript) != string(pullerScript)

	state := "未打"
	if installed {
		state = "已打"
	} else if scriptPresent || strings.Contains(string(idxBytes), pullerTag) || preSt.injected || mainSt.injected {
		state = "不完整"
	}
	if installed && (!preSt.synced || !mainSt.synced || oldScript) {
		state = "已打（含旧版组件，重跑可自动更新）"
	}
	ver := func(ok2 bool, synced2 bool) string {
		if !ok2 {
			return "无"
		}
		if synced2 {
			return "有"
		}
		return "有（版本旧）"
	}
	comp := fmt.Sprintf("renderer 脚本: %s | index.html 挂载: %s | preload 桥: %s | main handler: %s",
		yesNo(scriptPresent), yesNo(strings.Contains(string(idxBytes), pullerTag)), ver(preSt.injected, preSt.synced), ver(mainSt.injected, mainSt.synced))
	fmt.Printf("[*] %s\n    模型拉取按钮注入: %s | %s | sidecar: %s | 备份: %s\n", asarPath, state, comp, yesNo(savedOK), yesNo(fileExists(bak)))
}

func doPullerInstall() {
	a := cachedReadAsar(asarPath)
	side := asarPath + pullerSideSuffix
	bak := asarPath + pullerBakSuffix

	idxEnt, ok := a.findEntry(pullerIndexPath)
	if !ok {
		fmt.Printf("[!] %s\n    未找到 %s，版本结构可能已变，跳过\n", asarPath, pullerIndexPath)
		return
	}
	idxBytes := a.entryBytes(idxEnt)
	scriptEnt, scriptPresent := a.findEntry(pullerScriptPath)
	preBlob, mainBlob := []byte(nil), []byte(nil)
	if ent, ok := a.findEntry(pullerPreloadPath); ok {
		preBlob = a.entryBytes(ent)
	}
	if ent, ok := a.findEntry(pullerMainPath); ok {
		mainBlob = a.entryBytes(ent)
	}
	preSt := pullerPreloadState(preBlob)
	mainSt := pullerMainState(mainBlob)
	curScript := []byte(nil)
	if scriptPresent {
		curScript = a.entryBytes(scriptEnt)
	}
	installed := scriptPresent && strings.Contains(string(idxBytes), pullerTag) && preSt.injected && mainSt.injected

	saved, savedOK := pullerSidecarValid()

	if installed && string(curScript) == string(pullerScript) && preSt.synced && mainSt.synced {
		fmt.Printf("[=] %s\n    已打模型拉取注入（四组件均为当前版本），跳过\n", asarPath)
		return
	}

	if strings.Count(string(idxBytes), "</body>") != 1 {
		fmt.Printf("[!] %s\n    index.html 的 </body> 出现 %d 次（期望 1），拒绝盲改\n", asarPath, strings.Count(string(idxBytes), "</body>"))
		return
	}
	preHits := pullerPreloadRe.FindAllSubmatch(preBlob, -1)
	mainHits := pullerMainRe.FindAllSubmatch(mainBlob, -1)
	if len(preHits) != 1 || len(mainHits) != 1 {
		fmt.Printf("[!] %s\n    锚点异常：preload 命中 %d 次、main 命中 %d 次（各期望 1），版本结构可能已变，拒绝盲改\n    preload 锚点: %s\n    main 锚点: %s\n", asarPath, len(preHits), len(mainHits), pullerPreloadRe.String(), pullerMainRe.String())
		return
	}

	if !fileExists(bak) {
		copyFile(bak, asarPath)
	}

	rec := map[string]interface{}{}
	if savedOK {
		for k, v := range saved {
			rec[k] = v
		}
	}
	overwrite := map[string][]byte{}
	if string(curScript) != string(pullerScript) {
		overwrite[pullerScriptPath] = pullerScript
	}
	idxTagged := strings.Contains(string(idxBytes), pullerTag)
	if !idxTagged {
		newIdx := strings.Replace(string(idxBytes), "</body>", pullerTag+"</body>", 1)
		overwrite[pullerIndexPath] = []byte(newIdx)
		if _, has := rec["index_original_b64"]; !has {
			rec["index_original_b64"] = b64encode(idxBytes)
		}
	}
	if !preSt.synced {
		if preSt.clean == nil {
			fmt.Printf("[!] %s\n    preload 已注入但形态不符（旧版/被改过），无法安全更新，拒绝（先 revert 或重装 ZCode）\n", asarPath)
			return
		}
		m := pullerPreloadRe.FindSubmatchIndex(preSt.clean)
		anchorEnd := m[1]
		injection := []byte(pullerPreloadInjection(preSt.alias))
		newPreload := append([]byte{}, preSt.clean[:anchorEnd]...)
		newPreload = append(newPreload, injection...)
		newPreload = append(newPreload, preSt.clean[anchorEnd:]...)
		overwrite[pullerPreloadPath] = newPreload
		if _, has := rec["preload_original_b64"]; !has {
			rec["preload_original_b64"] = b64encode(preSt.clean)
		}
	}
	if !mainSt.synced {
		if mainSt.clean == nil {
			fmt.Printf("[!] %s\n    main 已注入但形态不符（旧版/被改过），无法安全更新，拒绝（先 revert 或重装 ZCode）\n", asarPath)
			return
		}
		m := pullerMainRe.FindSubmatchIndex(mainSt.clean)
		anchorStart := m[0]
		injection := []byte(pullerMainInjection(mainSt.alias))
		newMain := append([]byte{}, mainSt.clean[:anchorStart]...)
		newMain = append(newMain, injection...)
		newMain = append(newMain, mainSt.clean[anchorStart:]...)
		overwrite[pullerMainPath] = newMain
		if _, has := rec["main_original_b64"]; !has {
			rec["main_original_b64"] = b64encode(mainSt.clean)
		}
	}

	newSize := repack(asarPath, overwrite, nil)
	rec["asar_size"] = json.Number(itoa(int(newSize)))
	rec["index_path"] = pullerIndexPath
	rec["script_entry"] = pullerScriptPath
	rec["preload_path"] = pullerPreloadPath
	rec["main_path"] = pullerMainPath
	if b, err := marshalCompact(rec); err == nil {
		os.WriteFile(side, b, 0644)
	}
	refreshSidecarBySuffix(asarPath, ".chart-patch.json")
	refreshSidecarBySuffix(asarPath, ".width-patch.json")

	done := []string{}
	if true { // renderer 脚本总是需要就位
		done = append(done, "renderer")
	}
	if pullerIndexPath2(overwrite) {
		done = append(done, "index")
	}
	if _, ok := overwrite[pullerPreloadPath]; ok {
		done = append(done, "preload")
	}
	if _, ok := overwrite[pullerMainPath]; ok {
		done = append(done, "main")
	}
	fmt.Printf("[+] %s\n    模型拉取按钮注入完成（%s，zcode-model-puller.js %d 字节）\n    原件备份: %s | 记录: %s\n", asarPath, strings.Join(done, "+"), len(pullerScript), filepath.Base(bak), filepath.Base(side))
}

func pullerIndexPath2(overwrite map[string][]byte) bool {
	_, ok := overwrite[pullerIndexPath]
	return ok
}

func doPullerRevert() {
	a := cachedReadAsar(asarPath)
	side := asarPath + pullerSideSuffix
	bak := asarPath + pullerBakSuffix

	idxEnt, ok := a.findEntry(pullerIndexPath)
	if !ok {
		fmt.Printf("[!] %s\n    未找到 %s，跳过\n", asarPath, pullerIndexPath)
		return
	}
	idxBytes := a.entryBytes(idxEnt)
	tagged := strings.Contains(string(idxBytes), pullerTag)
	_, scriptPresent := a.findEntry(pullerScriptPath)
	installed := scriptPresent && tagged

	preBlob, mainBlob := []byte(nil), []byte(nil)
	if ent, ok := a.findEntry(pullerPreloadPath); ok {
		preBlob = a.entryBytes(ent)
	}
	if ent, ok := a.findEntry(pullerMainPath); ok {
		mainBlob = a.entryBytes(ent)
	}
	preSt := pullerPreloadState(preBlob)
	mainSt := pullerMainState(mainBlob)
	_, savedOK := pullerSidecarValid()

	if !installed && !savedOK {
		fmt.Printf("[.] %s\n    未打模型拉取注入，跳过\n", asarPath)
		return
	}

	stripped := strings.ReplaceAll(strings.ReplaceAll(string(idxBytes), pullerTag+"\n", ""), pullerTag, "")
	overwrite := map[string][]byte{pullerIndexPath: []byte(stripped)}
	if preSt.injected {
		if preSt.clean == nil {
			fmt.Printf("[!] %s\n    preload 注入段形态不符（可能被手工改过），拒绝还原\n", asarPath)
			return
		}
		overwrite[pullerPreloadPath] = preSt.clean
	}
	if mainSt.injected {
		if mainSt.clean == nil {
			fmt.Printf("[!] %s\n    main 注入段形态不符（可能被手工改过），拒绝还原\n", asarPath)
			return
		}
		overwrite[pullerMainPath] = mainSt.clean
	}
	remove := map[string]bool{pullerScriptPath: scriptPresent}
	newSize := repack(asarPath, overwrite, remove)
	os.Remove(side)
	os.Remove(bak)
	refreshSidecarBySuffix(asarPath, ".chart-patch.json")
	refreshSidecarBySuffix(asarPath, ".width-patch.json")
	fmt.Printf("[+] %s\n    已还原模型拉取注入（新大小 %d 字节，备份已清理）\n", asarPath, newSize)
}