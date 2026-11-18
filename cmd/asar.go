package main

// asar.go — app.asar 的解析与重打包（移植自 python 版 _repack_asar 等）。
//
// 与 python 版行为对齐的关键点：
//   - JSON 解码必须 UseNumber：integrity/size 均为整数字符串，
//     float64 会丢精度且断言失败。
//   - 递归传「节点本身」（ent），各层自行解 node["files"]；
//     早期版本传 children 导致双重解引用，整棵目录树被当叶子跳过。
//   - repack：purge 删除条目与空目录 → 快照旧位置 → relayout 重算
//     offset/size/integrity → 头 JSON compact + 4 字节补齐 → 临时文件回读
//     校验后原子替换。

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"crypto/sha256"
	"encoding/hex"
)

const blockSize = 4194304

type asarFile struct {
	raw       []byte
	header    map[string]interface{}
	dataStart int
	path      string
}

var asarPath string

func fail(format string, args ...interface{}) {
	fmt.Printf("[!] "+format+"\n", args...)
	os.Exit(1)
}

var asarCache = map[string]*asarFile{}

// cachedReadAsar：进程内同一 asar 只全量读取一次（385MB×多次=分钟级）。
// 仅限只读流程（check）；任何写操作后必须 dropAsarCache 失效。
func cachedReadAsar(path string) *asarFile {
	if c, ok := asarCache[path]; ok {
		return c
	}
	c := readAsar(path)
	asarCache[path] = c
	return c
}

func dropAsarCache(path string) {
	delete(asarCache, path)
}

func readAsar(path string) *asarFile {
	raw, err := os.ReadFile(path)
	if err != nil {
		fail("无法读取 asar: %v", err)
	}
	if len(raw) < 16 {
		fail("asar 文件过小: %s", path)
	}
	f0 := binary.LittleEndian.Uint32(raw[0:4])
	f1 := binary.LittleEndian.Uint32(raw[4:8])
	f3 := binary.LittleEndian.Uint32(raw[12:16])
	if f0 != 4 {
		fail("asar 头格式不符（首 uint32=%d，期望 4）: %s", f0, path)
	}
	if int64(len(raw)) < 16+int64(f3) {
		fail("asar 头长度越界: %s", path)
	}
	var header map[string]interface{}
	dec := json.NewDecoder(strings.NewReader(string(raw[16:16+f3])))
	dec.UseNumber()
	if err := dec.Decode(&header); err != nil {
		fail("asar 头 JSON 解析失败: %v", err)
	}
	return &asarFile{raw: raw, header: header, dataStart: 8 + int(f1), path: path}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

type pathEnt struct {
	path string
	ent  map[string]interface{}
}

// walk：按名字排序产出叶子条目（目录与 unpacked 条目跳过）。
// 递归传「节点本身」，各层自行解 files（避免双重解引用）。
func walk(node map[string]interface{}, prefix string, out *[]pathEnt) {
	files, _ := node["files"].(map[string]interface{})
	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		ent, _ := files[name].(map[string]interface{})
		p := name
		if prefix != "" {
			p = prefix + "/" + name
		}
		if ent == nil {
			continue
		}
		if _, isDir := ent["files"]; isDir {
			walk(ent, p, out)
		} else if b, _ := ent["unpacked"].(bool); !b {
			*out = append(*out, pathEnt{p, ent})
		}
	}
}

func (a *asarFile) entries() []pathEnt {
	var out []pathEnt
	walk(a.header, "", &out)
	return out
}

func (a *asarFile) findEntry(p string) (map[string]interface{}, bool) {
	for _, pe := range a.entries() {
		if pe.path == p {
			return pe.ent, true
		}
	}
	return nil, false
}

func (a *asarFile) entryBytes(ent map[string]interface{}) []byte {
	offS, _ := ent["offset"].(string)
	size, _ := ent["size"].(json.Number)
	sizeN, _ := size.Int64()
	start := a.dataStart + atoi(offS)
	end := start + int(sizeN)
	if start < 0 || end > len(a.raw) || start > end {
		fail("asar 条目越界: offset=%s size=%s", offS, size.String())
	}
	return a.raw[start:end]
}

func integrity(data []byte) map[string]interface{} {
	whole := sha256.Sum256(data)
	blocks := make([]string, 0, len(data)/blockSize+1)
	for i := 0; i < len(data); i += blockSize {
		end := i + blockSize
		if end > len(data) {
			end = len(data)
		}
		b := sha256.Sum256(data[i:end])
		blocks = append(blocks, hex.EncodeToString(b[:]))
	}
	return map[string]interface{}{
		"algorithm": "SHA256",
		"hash":      hex.EncodeToString(whole[:]),
		"blockSize": blockSize,
		"blocks":    blocks,
	}
}


// 薄封装：内部全部走标准库（SIMD 加速），避免手写朴素 O(n×m) 扫描
func bytesIndex(h, needle []byte) int {
	return bytes.Index(h, needle)
}

func bytesCount(h, needle []byte) int {
	return bytes.Count(h, needle)
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func fileSize(p string) int64 {
	info, err := os.Stat(p)
	if err != nil {
		return -1
	}
	return info.Size()
}

// marshalCompact：对齐 python json.dumps(separators=(',',':'), ensure_ascii=False)
func marshalCompact(v interface{}) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(b.String(), "\n")), nil
}

// b64encode：与 python base64 对齐
func b64encode(data []byte) string {
	const tbl = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out strings.Builder
	for i := 0; i < len(data); i += 3 {
		var n uint32
		rem := len(data) - i
		switch rem {
		case 1:
			n = uint32(data[i]) << 16
			out.WriteString(string(tbl[(n>>18)&63]))
			out.WriteString(string(tbl[(n>>12)&63]))
			out.WriteString("==")
		case 2:
			n = uint32(data[i])<<16 | uint32(data[i+1])<<8
			out.WriteString(string(tbl[(n>>18)&63]))
			out.WriteString(string(tbl[(n>>12)&63]))
			out.WriteString(string(tbl[(n>>6)&63]))
			out.WriteString("=")
		default:
			n = uint32(data[i])<<16 | uint32(data[i+1])<<8 | uint32(data[i+2])
			out.WriteString(string(tbl[(n>>18)&63]))
			out.WriteString(string(tbl[(n>>12)&63]))
			out.WriteString(string(tbl[(n>>6)&63]))
			out.WriteString(string(tbl[n&63]))
		}
	}
	return out.String()
}

// repack：删除 remove、覆盖/新增 overwrite，重算 integrity 与 offset；
// 写临时文件回读校验后原子替换。返回新文件大小。
func repack(path string, overwrite map[string][]byte, remove map[string]bool) int64 {
	a := readAsar(path)

	// 新增文件按层级插入占位条目
	for p := range overwrite {
		parts := strings.Split(p, "/")
		node := a.header
		for _, part := range parts[:len(parts)-1] {
			files, _ := node["files"].(map[string]interface{})
			if files == nil {
				files = map[string]interface{}{}
				node["files"] = files
			}
			next, ok := files[part].(map[string]interface{})
			if !ok {
				next = map[string]interface{}{"files": map[string]interface{}{}}
				files[part] = next
			}
			node = next
		}
		files, _ := node["files"].(map[string]interface{})
		if files == nil {
			files = map[string]interface{}{}
			node["files"] = files
		}
		if _, ok := files[parts[len(parts)-1]]; !ok {
			files[parts[len(parts)-1]] = map[string]interface{}{"size": json.Number("0"), "offset": "0"}
		}
	}

	var purge func(node map[string]interface{}, prefix string)
	purge = func(node map[string]interface{}, prefix string) {
		files, _ := node["files"].(map[string]interface{})
		for name, e := range files {
			ent, _ := e.(map[string]interface{})
			p := name
			if prefix != "" {
				p = prefix + "/" + name
			}
			if ent == nil {
				continue
			}
			if _, isDir := ent["files"]; isDir {
				sub, _ := ent["files"].(map[string]interface{})
				purge(sub, p)
				if len(sub) == 0 {
					delete(files, name)
				}
			} else if remove[p] {
				delete(files, name)
			}
		}
	}
	purge(a.header, "")

	type pos struct{ off, size int }
	old := map[string]pos{}
	for _, pe := range a.entries() {
		off := atoi(pe.ent["offset"].(string))
		size, _ := pe.ent["size"].(json.Number)
		sizeN, _ := size.Int64()
		old[pe.path] = pos{off, int(sizeN)}
	}
	cursor := 0

	entryData := func(p string) []byte {
		if d, ok := overwrite[p]; ok {
			return d
		}
		o := old[p]
		return a.raw[a.dataStart+o.off : a.dataStart+o.off+o.size]
	}

	var relayout func(node map[string]interface{}, prefix string)
	relayout = func(node map[string]interface{}, prefix string) {
		files, _ := node["files"].(map[string]interface{})
		names := make([]string, 0, len(files))
		for k := range files {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			ent, _ := files[name].(map[string]interface{})
			p := name
			if prefix != "" {
				p = prefix + "/" + name
			}
			if ent == nil {
				continue
			}
			if _, isDir := ent["files"]; isDir {
				relayout(ent, p)
			} else if b, _ := ent["unpacked"].(bool); b {
				continue
			} else {
				d := entryData(p)
				ent["size"] = json.Number(itoa(len(d)))
				ent["offset"] = itoa(cursor)
				if _, isOver := overwrite[p]; isOver {
					ent["integrity"] = integrity(d)
				}
				cursor += len(d)
			}
		}
	}
	relayout(a.header, "")

	jb, err := marshalCompact(a.header)
	if err != nil {
		fail("头序列化失败: %v", err)
	}
	pad := (4 - len(jb)%4) % 4
	head := make([]byte, 16+len(jb)+pad)
	binary.LittleEndian.PutUint32(head[0:4], 4)
	binary.LittleEndian.PutUint32(head[4:8], uint32(8+len(jb)+pad))
	binary.LittleEndian.PutUint32(head[8:12], uint32(4+len(jb)+pad))
	binary.LittleEndian.PutUint32(head[12:16], uint32(len(jb)))
	copy(head[16:], jb)
	for i := len(jb); i < len(jb)+pad; i++ {
		head[16+i] = 0
	}

	tmp := path + ".tps-tmp"
	out, err := os.Create(tmp)
	if err != nil {
		fail("无法创建临时文件: %v", err)
	}
	if _, err := out.Write(head); err != nil {
		out.Close()
		os.Remove(tmp)
		fail("写头失败: %v", err)
	}
	emitErr := error(nil)
	var emit func(node map[string]interface{}, prefix string)
	emit = func(node map[string]interface{}, prefix string) {
		if emitErr != nil {
			return
		}
		files, _ := node["files"].(map[string]interface{})
		names := make([]string, 0, len(files))
		for k := range files {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			ent, _ := files[name].(map[string]interface{})
			p := name
			if prefix != "" {
				p = prefix + "/" + name
			}
			if ent == nil {
				continue
			}
			if _, isDir := ent["files"]; isDir {
				emit(ent, p)
			} else if b, _ := ent["unpacked"].(bool); b {
				continue
			} else {
				if _, err := out.Write(entryData(p)); err != nil {
					emitErr = err
				}
			}
		}
	}
	emit(a.header, "")
	out.Close()
	if emitErr != nil {
		os.Remove(tmp)
		fail("写数据失败: %v", emitErr)
	}

	v := readAsar(tmp)
	for _, pe := range v.entries() {
		if d, ok := overwrite[pe.path]; ok {
			if string(v.entryBytes(pe.ent)) != string(d) {
				os.Remove(tmp)
				fail("重打包校验失败: %s", pe.path)
			}
		}
	}
	info, err := os.Stat(tmp)
	if err != nil {
		os.Remove(tmp)
		fail("临时文件状态异常: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		fail("原子替换失败（ZCode 是否在运行？）: %v", err)
	}
	dropAsarCache(path)
	return info.Size()
}

// syncIntegrity：同长度原地更新条目 integrity（sha256 hex 定长 64 字符，头部零改动）。
// 与 python 版一致：哈希在头 JSON 里出现 >2 次（共享内容）时保守跳过。
func syncIntegrity(path string, entryPath string, newBytes []byte) {
	a := readAsar(path)
	ent, ok := a.findEntry(entryPath)
	if !ok {
		return
	}
	itg, _ := ent["integrity"].(map[string]interface{})
	if itg == nil {
		return
	}
	newItg := integrity(newBytes)

	f3 := binary.LittleEndian.Uint32(a.raw[12:16])
	hdr := a.raw[16 : 16+int(f3)] // 头 JSON 原始字节

	pairs := [][2]string{}
	if h, _ := itg["hash"].(string); h != newItg["hash"].(string) {
		pairs = append(pairs, [2]string{h, newItg["hash"].(string)})
	}
	oldBlocks, _ := itg["blocks"].([]interface{})
	newBlocks, _ := newItg["blocks"].([]string)
	if len(oldBlocks) == len(newBlocks) {
		for i := range oldBlocks {
			os_, ns_ := oldBlocks[i].(string), newBlocks[i]
			if os_ != ns_ {
				pairs = append(pairs, [2]string{os_, ns_})
			}
		}
	}

	if len(pairs) == 0 {
		return
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		fmt.Printf("    [!] %s integrity 同步失败：无法打开 %v\n", entryPath, err)
		return
	}
	defer f.Close()
	dropAsarCache(path)
	for _, pair := range pairs {
		oldB, newB := []byte(pair[0]), []byte(pair[1])
		count := bytesCount(hdr, oldB)
		if count == 0 {
			continue
		}
		if count > 2 {
			fmt.Printf("    [!] %s 的 integrity 哈希出现 %d 次（存在同内容条目），跳过同步\n", entryPath, count)
			continue
		}
		start := 0
		for start <= len(hdr)-len(oldB) {
			j := bytes.Index(hdr[start:], oldB)
			if j < 0 {
				break
			}
			pos := start + j
			if _, err := f.WriteAt(newB, int64(16+pos)); err != nil {
				break
			}
			start = pos + len(oldB)
		}
	}
}

// bytesReplaceOnce：只替换第一次出现（对齐 python str.replace(old,new,1)）
func bytesReplaceOnce(raw, pattern, replacement []byte) []byte {
	if len(pattern) == 0 {
		return raw
	}
	var out []byte
	count := 0
	i := 0
	for {
		j := bytesIndex(raw[i:], pattern)
		if j < 0 || count >= 1 {
			out = append(out, raw[i:]...)
			break
		}
		out = append(out, raw[i:i+j]...)
		out = append(out, replacement...)
		i += j + len(pattern)
		count++
	}
	return out
}

// chartPatchRec：usage-chart 补丁的通用记录加载（兼容旧单条格式）
func loadSidecarPatches(path string, asarSize int64) []map[string]interface{} {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var rec interface{}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber() // 数字保持字符串形态，与文件里 json.Number 匹配（否则按 asar_size 指纹过滤全空）
	if dec.Decode(&rec) != nil {
		return nil
	}
	var list []map[string]interface{}
	switch v := rec.(type) {
	case map[string]interface{}:
		if ps, ok := v["patches"].([]interface{}); ok {
			for _, p := range ps {
				if pm, ok := p.(map[string]interface{}); ok {
					list = append(list, pm)
				}
			}
		} else {
			list = append(list, v)
		}
	case []interface{}:
		for _, p := range v {
			if pm, ok := p.(map[string]interface{}); ok {
				list = append(list, pm)
			}
		}
	}
	if asarSize > 0 {
		filtered := list[:0]
		for _, r := range list {
			if n, _ := r["asar_size"].(json.Number); n.String() == itoa(int(asarSize)) {
				filtered = append(filtered, r)
			}
		}
		list = filtered
	}
	return list
}

func sidecarSave(path string, saved []map[string]interface{}) error {
	b, err := marshalCompact(map[string]interface{}{"patches": saved})
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

// refreshSidecarBySuffix：重打包后数据区位移，记录里的绝对 offset 需重定位。
// 按「路径+尺寸」重新定位条目，更新 offset 与 asar_size；找不到（=升级被整体替换）则不动。
func refreshSidecarBySuffix(asar string, suffix string) {
	side := asar + suffix
	data, err := os.ReadFile(side)
	if err != nil {
		return
	}
	var rec struct {
		Patches []map[string]interface{} `json:"patches"`
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber() // 与 loadSidecarPatches 同因：数字保持字符串形态，断言才匹配
	if dec.Decode(&rec) != nil || len(rec.Patches) == 0 {
		return
	}
	cur := fileSize(asar)
	a := readAsar(asar)
	changed := false
	for _, r := range rec.Patches {
		pathS, _ := r["path"].(string)
		sizeN, ok2 := r["size"].(json.Number)
		if !ok2 {
			continue
		}
		for _, pe := range a.entries() {
			sizeEnt, ok3 := pe.ent["size"].(json.Number)
			if ok3 && pe.path == pathS && sizeEnt.String() == sizeN.String() {
				offS, _ := r["offset"].(string)
				sizeF, _ := r["asar_size"].(json.Number)
				if offS != pe.ent["offset"].(string) || sizeF.String() != itoa(int(cur)) {
					r["offset"] = pe.ent["offset"].(string)
					r["asar_size"] = json.Number(itoa(int(cur)))
					changed = true
				}
				break
			}
		}
	}
	if changed {
		if b, err := marshalCompact(rec); err == nil {
			os.WriteFile(side, b, 0644)
		}
	}
}

var _ = filepath.Base