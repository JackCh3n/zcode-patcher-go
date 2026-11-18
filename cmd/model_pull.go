package main

// model_pull.go — 拉取供应商模型到 ~/.zcode/v2/config.json（CLI，不动 asar）。
// 移植自 scripts/model_pull.py：元数据感知模板 + --refresh + dry-run + 写前备份。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const fallbackLimitContext = 1000000
const fallbackLimitOutput = 128000

var fallbackEfforts = []string{"off", "high", "max"}

func configPath() string {
	h, err := os.UserHomeDir()
	if err != nil {
		fail("[!] 无法定位用户目录")
	}
	return filepath.Join(h, ".zcode", "v2", "config.json")
}

func parseModelItem(item interface{}) (string, map[string]interface{}) {
	switch v := item.(type) {
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return "", nil
		}
		return s, nil
	case map[string]interface{}:
		mid := ""
		if id, ok := v["id"].(string); ok && strings.TrimSpace(id) != "" {
			mid = strings.TrimSpace(id)
		} else if name, ok := v["name"].(string); ok && strings.TrimSpace(name) != "" {
			mid = strings.TrimSpace(name)
		}
		if mid == "" {
			return "", nil
		}
		top := map[string]interface{}{}
		if tp, ok := v["top_provider"].(map[string]interface{}); ok {
			top = tp
		}
		ctx := toInt(getAnyStr(v, []string{"context_length", "max_input_tokens"}))
		out := toInt(getAnyStr(v, []string{"max_output_tokens", "max_completion_tokens"}))
		if out == 0 {
			out = toInt(top["max_completion_tokens"])
		}
		var efforts []string
		if eff, ok := v["supported_efforts"].([]interface{}); ok {
			for _, x := range eff {
				if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
					efforts = append(efforts, strings.TrimSpace(s))
				}
			}
		}
		def, _ := v["default_effort"].(string)
		meta := map[string]interface{}{
			"context": ctx, "output": out, "efforts": efforts, "def": def,
		}
		return mid, meta
	default:
		return "", nil
	}
}

func getAnyStr(m map[string]interface{}, keys []string) interface{} {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		var i int
		fmt.Sscanf(strings.TrimSpace(n), "%d", &i)
		return i
	}
	return 0
}

func buildEfforts(meta map[string]interface{}) []string {
	var efforts []string
	if arr, ok := meta["efforts"].([]string); ok && len(arr) > 0 {
		efforts = append(efforts, arr...)
	} else {
		efforts = append(efforts, fallbackEfforts...)
	}
	if !contains(efforts, "off") {
		efforts = append([]string{"off"}, efforts...)
	}
	return efforts
}

func contains(arr []string, s string) bool {
	for _, x := range arr {
		if x == s {
			return true
		}
	}
	return false
}

func buildEntry(meta map[string]interface{}, withReasoning bool) map[string]interface{} {
	var m map[string]interface{}
	if meta != nil {
		m = meta
	}
	efforts := buildEfforts(m)
	def := ""
	if d, _ := m["def"].(string); contains(efforts, d) {
		def = d
	} else {
		def = efforts[len(efforts)-1]
	}
	tpl := map[string]interface{}{
		"limit": map[string]interface{}{
			"context": ctxOf(m), "output": outOf(m),
		},
		"modalities": map[string]interface{}{"input": []string{"text", "image"}, "output": []string{"text"}},
		"zcode":      map[string]interface{}{"modalitiesConfigured": true, "modified": true},
	}
	if withReasoning {
		tpl["reasoning"] = map[string]interface{}{
			"enabled":        true,
			"variants":       efforts,
			"defaultVariant": def,
		}
	}
	return tpl
}

func ctxOf(m map[string]interface{}) int {
	c, _ := m["context"].(float64)
	if c == 0 {
		n, _ := m["context"].(json.Number)
		c, _ = n.Float64()
	}
	if c == 0 {
		return fallbackLimitContext
	}
	return int(c)
}

func outOf(m map[string]interface{}) int {
	o, _ := m["output"].(float64)
	if o == 0 {
		n, _ := m["output"].(json.Number)
		o, _ = n.Float64()
	}
	if o == 0 {
		return fallbackLimitOutput
	}
	return int(o)
}

func fetchModelsFromAPI(baseURL, apiKey string, timeoutSecs int) (bool, string, []string, map[string]interface{}) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return false, "Base URL 为空", nil, nil
	}
	var candidates []string
	if strings.HasSuffix(baseURL, "/v1") {
		candidates = append(candidates, baseURL+"/models")
		candidates = append(candidates, baseURL[:len(baseURL)-3]+"/models")
	} else {
		candidates = append(candidates, baseURL+"/v1/models")
		candidates = append(candidates, baseURL+"/models")
	}
	if strings.Contains(baseURL, "/api") {
		candidates = append(candidates, baseURL+"/v1/models")
	}
	headers := map[string]string{"User-Agent": "ZCode", "Accept": "application/json"}
	key := strings.TrimSpace(apiKey)
	if key != "" {
		headers["Authorization"] = "Bearer " + key
		headers["x-api-key"] = key
	}
	lastErr := ""
	client := &http.Client{Timeout: time.Duration(timeoutSecs) * time.Second}
	for _, url := range candidates {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Sprintf("HTTP %d", resp.StatusCode)
			continue
		}
		var data interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			lastErr = "响应非 JSON"
			continue
		}
		var raw []interface{}
		switch v := data.(type) {
		case []interface{}:
			raw = v
		case map[string]interface{}:
			if arr, ok := v["data"].([]interface{}); ok {
				raw = arr
			} else if arr, ok := v["models"].([]interface{}); ok {
				raw = arr
			}
		}
		var ids []string
		metas := map[string]interface{}{}
		for _, item := range raw {
			mid, meta := parseModelItem(item)
			if mid == "" {
				continue
			}
			if _, has := metas[mid]; !has {
				metas[mid] = meta
				ids = append(ids, mid)
			}
		}
		if len(ids) > 0 {
			sort.Strings(ids)
			return true, "成功从 " + url + " 获取", ids, metas
		}
		lastErr = "未能解析到有效模型列表"
	}
	return false, "拉取失败: " + lastErr, nil, nil
}

func refreshEntry(ent map[string]interface{}, meta map[string]interface{}) []string {
	changed := []string{}
	if hasMeta(meta) {
		oldLimit, _ := ent["limit"].(map[string]interface{})
		newLimit := map[string]interface{}{}
		nc, _ := meta["context"].(json.Number)
		if nc.String() != "0" {
			newLimit["context"] = nc
		} else if oc, _ := oldLimit["context"].(json.Number); oc.String() != "" {
			newLimit["context"] = oc
		}
		no, _ := meta["output"].(json.Number)
		if no.String() != "0" {
			newLimit["output"] = no
		} else if oo, _ := oldLimit["output"].(json.Number); oo.String() != "" {
			newLimit["output"] = oo
		}
		if !mapEqual(oldLimit, newLimit) {
			ent["limit"] = newLimit
			changed = append(changed, fmt.Sprintf("limit %d/%d", toInt(newLimit["context"]), toInt(newLimit["output"])))
		}
	}
	if reasoning, _ := ent["reasoning"].(map[string]interface{}); reasoning != nil {
		if enabled, _ := reasoning["enabled"].(bool); enabled {
			efforts := buildEfforts(meta)
			def := ""
			if d, _ := meta["def"].(string); contains(efforts, d) {
				def = d
			} else {
				def = efforts[len(efforts)-1]
			}
			newReasoning := map[string]interface{}{
				"enabled": true, "variants": efforts, "defaultVariant": def,
			}
			if !mapEqual(reasoning, newReasoning) {
				ent["reasoning"] = newReasoning
				changed = append(changed, "档位 "+strings.Join(efforts, "/")+" 默认 "+def)
			}
		}
	}
	return changed
}

func mapEqual(a, b map[string]interface{}) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(ab, bb)
}

// hasMeta：服务器元数据里 context 或 output 至少有一个非零值
func hasMeta(meta map[string]interface{}) bool {
	if meta == nil {
		return false
	}
	if c, _ := meta["context"].(json.Number); c.String() != "0" {
		return true
	}
	if o, _ := meta["output"].(json.Number); o.String() != "0" {
		return true
	}
	return false
}

func runModelPull(args []string) {
	all, dryRun, refresh, noReasoning, providerKw := false, false, false, false, ""
	var testTargets []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--all":
			all = true
		case "--dry-run":
			dryRun = true
		case "--refresh":
			refresh = true
		case "--no-reasoning":
			noReasoning = true
		case "--provider":
			if i+1 < len(args) {
				providerKw = args[i+1]
				i++
			}
		case "--test":
			testTargets = args[i+1:]
		default:
			fail("[!] 未知参数: %s", args[i])
		}
	}

	if len(testTargets) > 0 {
		url := testTargets[0]
		key := ""
		if len(testTargets) > 1 {
			key = testTargets[1]
		}
		ok, msg, models, metas := fetchModelsFromAPI(url, key, 10)
		fmt.Println(msgOk(ok) + msg)
		if ok {
			fmt.Printf("共 %d 个模型：\n", len(models))
			for _, m := range models {
				meta := getMeta(metas, m)
				fmt.Printf("  %-35s ctx %8s  out %7s  档位 %s\n", m, fmtCtx(meta), fmtOut(meta), fmtEfforts(meta))
			}
		}
		os.Exit(exitCodeBool(ok))
	}

	cp := configPath()
	data, err := os.ReadFile(cp)
	if err != nil {
		fail("[!] 找不到配置文件: %s", cp)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		fail("[!] 配置文件解析失败: %v", err)
	}
	customs := customProvers(cfg)
	if len(customs) == 0 {
		fail("[!] config 里没有自定义供应商（builtin 之外、source=custom）")
	}

	var selected [][2]map[string]interface{}
	if all || providerKw != "" {
		k := strings.ToLower(providerKw)
		for _, c := range customs {
			pid := getStr(c[0], "pid")
			name := strings.ToLower(strings.TrimSpace(getStr(c[1], "name")))
			baseURL := strings.ToLower(strings.TrimSpace(getStrOpt(c[1], "baseURL")))
			if !all && !(k != "" && (strings.Contains(pid, k) || strings.Contains(name, k) || strings.Contains(baseURL, k))) {
				continue
			}
			selected = append(selected, c)
		}
		if len(selected) == 0 {
			names := []string{}
			for _, c := range customs {
				names = append(names, strDefault(getStr(c[1], "name"), getStr(c[0], "pid")))
			}
			fail("[!] 没有匹配「%s」的供应商；现有：%s", providerKw, strings.Join(names, ", "))
		}
	} else {
		fmt.Printf("找到 %d 个自定义供应商：\n", len(customs))
		for i, c := range customs {
			fmt.Printf("  [%d] %s  %s  （已有 %d 模型）\n", i+1, strDefault(getStr(c[1], "name"), "未命名"), strDefault(getStrOpt(c[1], "baseURL"), "-"), len(getModelsOrNil(c[1])))
		}
		fmt.Print("选择编号（a=全部，q=退出）: ")
		var choice string
		fmt.Scanln(&choice)
		choice = strings.TrimSpace(strings.ToLower(choice))
		if choice == "q" {
			return
		}
		if choice == "a" {
			selected = customs
		} else {
			n := atoiChoice(choice)
			if n < 1 || n > len(customs) {
				fail("[!] 无效选择")
			}
			selected = [][2]map[string]interface{}{customs[n-1]}
		}
	}

	totalAdded := 0
	for _, c := range selected {
		pid := getStr(c[0], "pid")
		name := strDefault(getStr(c[1], "name"), "未命名")
		opts := toMap(getStrMapOrNil(c[1], "options"))
		baseURL := getStrOpts(opts, "baseURL")
		apiKey := getStrOpts(opts, "apiKey")

		fmt.Printf("\n🔄 供应商「%s」（%s）\n", name, pid)
		ok, msg, fetched, metas := fetchModelsFromAPI(baseURL, apiKey, 10)
		if !ok {
			fmt.Printf("  ❌ %s\n", msg)
			continue
		}
		models := getModelsOrNil(c[1])
		newModels := []string{}
		for _, m := range fetched {
			if _, has := models[m]; !has {
				newModels = append(newModels, m)
			}
		}
		fmt.Printf("  ✅ %s，共 %d 个模型，已存在 %d 个，新增 %d 个%s\n",
			msg, len(fetched), len(fetched)-len(newModels), len(newModels), suffixIf(refresh, "，刷新元数据"))
		for _, m := range newModels {
			meta := getMeta(metas, m)
			fmt.Printf("    + %s  (ctx %s / out %s / 档位 %s)\n", m,
				fmtVal(meta, "context"), fmtVal(meta, "output"), fmtArr(meta, "efforts"))
		}
		if len(newModels) == 0 && !refresh {
			continue
		}
		totalAdded += len(newModels)
		for _, m := range newModels {
			models[m] = buildEntry(getMeta(metas, m), !noReasoning)
		}
		if refresh {
			for mid, entI := range models {
				meta := getMeta(metas, mid)
				if meta == nil {
					continue
				}
				ent, okE := entI.(map[string]interface{})
				if !okE {
					continue
				}
				changed := refreshEntry(ent, meta)
				if len(changed) > 0 {
					prefix := "[预览] "
					if !dryRun {
						prefix = "↻ "
					}
					fmt.Printf("%s%s: %s\n", prefix, mid, strings.Join(changed, "；"))
				}
			}
		}
		if !dryRun {
			c[1]["models"] = models
		}
	}

	if dryRun {
		fmt.Printf("\n[dry-run] 将新增 %d 个模型，未写配置。去掉 --dry-run 执行写入。\n", totalAdded)
		return
	}
	if totalAdded > 0 || refresh {
		bak := cp + ".bak." + itoa(int(time.Now().Unix()))
		copyFile(bak, cp)
		b, err := marshalCompact(cfg)
		if err != nil {
			fail("[!] 配置序列化失败: %v", err)
		}
		os.WriteFile(cp, b, 0644)
		fmt.Printf("\n🎉 已写入配置（新增 %d 个模型，备份: %s）。\n", totalAdded, filepath.Base(bak))
		fmt.Println("    ZCode 运行中请在界面里切换一下页面刷新模型列表，或重启 ZCode。")
	}
}

// ---- model-pull 小工具 ----

func msgOk(ok bool) string {
	if ok {
		return "✅ "
	}
	return "❌ "
}

func exitCodeBool(ok bool) int {
	if ok {
		return 0
	}
	return 1
}

func strDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func getStr(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func getStrMapOrNil(m map[string]interface{}, key string) map[string]interface{} {
	v, ok := m[key].(map[string]interface{})
	if !ok {
		return nil
	}
	return v
}

func toMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return map[string]interface{}{}
	}
	return m
}

func getStrOpts(opts map[string]interface{}, key string) string {
	v, _ := opts[key].(string)
	return v
}

func toMapArr(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return map[string]interface{}{}
	}
	return m
}

func getModelsOrNil(pm map[string]interface{}) map[string]interface{} {
	v, ok := pm["models"].(map[string]interface{})
	if !ok {
		return nil
	}
	return v
}

func customProvers(cfg map[string]interface{}) [][2]map[string]interface{} {
	out := [][2]map[string]interface{}{}
	providers, _ := cfg["provider"].(map[string]interface{})
	for pid, pdata := range providers {
		pm, ok := pdata.(map[string]interface{})
		if !ok {
			continue
		}
		if strings.HasPrefix(pid, "builtin:") {
			continue
		}
		src, _ := pm["source"].(string)
		if src != "custom" && src != "user" && src != "" {
			continue
		}
		out = append(out, [2]map[string]interface{}{
			{"pid": pid}, pm,
		})
	}
	return out
}

func getMeta(metas map[string]interface{}, id string) map[string]interface{} {
	m, _ := metas[id].(map[string]interface{})
	return m
}

func fmtVal(m map[string]interface{}, key string) string {
	v, _ := m[key].(json.Number)
	if v.String() == "" || v.String() == "0" {
		return "-"
	}
	return v.String()
}

func fmtCtx(m map[string]interface{}) string { return fmtVal(m, "context") }
func fmtOut(m map[string]interface{}) string { return fmtVal(m, "output") }
func fmtEfforts(m map[string]interface{}) string {
	var efforts []string
	if arr, ok := m["efforts"].([]string); ok {
		efforts = arr
	}
	if len(efforts) == 0 {
		return "-"
	}
	return strings.Join(efforts, "/")
}

func fmtArr(m map[string]interface{}, key string) string { return fmtEfforts(m) }

func atoiChoice(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func suffixIf(cond bool, suffix string) string {
	if cond {
		return suffix
	}
	return ""
}

func getStrOpt(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}