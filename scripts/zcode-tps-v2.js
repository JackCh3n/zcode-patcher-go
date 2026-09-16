/**
 * ZCode 状态胶囊 v3 —— 读 ZCode 自己渲染的真实 token 数（零端口，3.12.2+ 安全）
 * =====================================================================
 * 数据源（两条，都只读 DOM，不接触 ServicePort / IPC）：
 *
 *  A) 本轮精确 token（首选）——「模型调用轨迹」面板里的元数据节点：
 *       <span data-trajectory-call-metadata>OUT 5,678 · 1.2s</span>
 *     由 ZCode 的 VNt() 渲染，text 形如 "IN 1,234 · OUT 5,678 · 1.2s · 12:34"。
 *     轨迹在工作期间默认展开（HMt(): {expanded:!0}），故生成中即可读到实时值；
 *     一轮含多次模型调用时会有多条，取最后一条（当前进行中的那次）与前缀和。
 *
 *  B) 上下文用量（回退）——composer 隐藏节点：
 *       <span data-testid="v4-model-config" data-usage-used="29568" data-usage-max="1000000" />
 *
 * 为什么不用端口：v1 截取 ServicePort 会让 ZCode 3.12.2 卡在启动界面
 * （MessagePort 消息只分发给分发瞬间已注册的监听器，我们提前 start 消费掉了
 *  服务端 Initialize 握手）。详见 SKILL.md。本版本静态扫描确认零端口接触。
 *
 * 显示：● OUT <本轮输出> · <耗时> · <tok/s>   （生成中实时；无数据时自动降级）
 * 不显示：上下文进度/百分比/思考档位（按用户要求移除）。
 */
(() => {
  if (window.__ztps3) return;
  window.__ztps3 = true;

  const BAR_ATTR = "data-ztps3-bar";
  const diag = (window.__ztps3Diag = {
    version: "3.0-trajectory",
    loadedAt: new Date().toISOString(),
    portTouched: false,
    samples: [],
  });

  // ---------- A) 轨迹元数据：解析 "IN x · OUT y · 1.2s · 12:34" ----------
  const NUM = (s) => {
    const n = Number(String(s ?? "").replace(/[,\s]/g, ""));
    return Number.isFinite(n) ? n : null;
  };
  const parseDur = (s) => {
    // 支持 "1.2s" / "12.3s" / "1m 5s" / "500ms" / "2m"
    const str = String(s || "").trim();
    let ms = 0, hit = false;
    let m;
    if ((m = /(\d+(?:\.\d+)?)\s*h/i.exec(str))) { ms += parseFloat(m[1]) * 3600e3; hit = true; }
    if ((m = /(\d+(?:\.\d+)?)\s*m(?!s)/i.exec(str))) { ms += parseFloat(m[1]) * 60e3; hit = true; }
    if ((m = /(\d+(?:\.\d+)?)\s*s/i.exec(str))) { ms += parseFloat(m[1]) * 1e3; hit = true; }
    if (!hit && (m = /(\d+(?:\.\d+)?)\s*ms/i.exec(str))) { ms += parseFloat(m[1]); hit = true; }
    return hit ? Math.round(ms) : null;
  };

  function readTrajectory() {
    const nodes = document.querySelectorAll("[data-trajectory-call-metadata]");
    if (!nodes.length) return null;
    // 可见性判据（不依赖 offsetParent——虚拟滚动/父子链异常时会误伤）：
    // 排除被 display:none 的祖先隐藏的节点，用 getClientRects() 判断实际渲染盒。
    const visible = (n) => {
      try {
        if (n.getClientRects && n.getClientRects().length > 0) return true;
        // jsdom / 非布局环境回退：检查自身与祖先链上的 hidden 类
        let cur = n;
        while (cur && cur !== document.body) {
          const c = typeof cur.className === "string" ? cur.className : "";
          if (/(^|\s)hidden(\s|$)/.test(c)) return false;
          if (cur.getAttribute && cur.getAttribute("aria-hidden") === "true") return false;
          cur = cur.parentElement;
        }
        return true;
      } catch (err) { return true; }
    };
    let last = null;         // 最后一条 = 当前进行中的调用
    let sumOut = 0;          // 本轮累计输出（所有调用的 OUT 之和）
    let sumIn = 0;
    let counted = 0;
    for (const n of nodes) {
      if (!visible(n)) continue;
      const txt = n.textContent || "";
      const mo = /OUT\s*([\d,]+)/i.exec(txt);
      const mi = /IN\s*([\d,]+)/i.exec(txt);
      const md = /·\s*([0-9.]+\s*(?:ms|s|m|h)[^·]*)/i.exec(txt);
      const item = {
        out: mo ? NUM(mo[1]) : null,
        inTok: mi ? NUM(mi[1]) : null,
        durMs: md ? parseDur(md[1]) : null,
        raw: txt.slice(0, 80),
      };
      if (item.out != null) sumOut += item.out;
      if (item.inTok != null) sumIn += item.inTok;
      last = item;
      counted++;
    }
    if (counted === 0) return null;
    return { last, sumOut, sumIn, count: counted };
  }

  // ---------- B) 回退：composer 上下文用量 ----------
  function readContextUsage() {
    const all = document.querySelectorAll("[data-testid='v4-model-config']");
    for (const el of all) {
      const used = NUM(el.getAttribute("data-usage-used"));
      if (used != null) return { used, max: NUM(el.getAttribute("data-usage-max")) };
    }
    return null;
  }

  // ---------- 生成中判定（停止按钮存在即生成中） ----------
  const isStreaming = () =>
    !!document.querySelector("[data-testid='v4-stop'], [data-testid='v4-composer-stop']");

  // ---------- 速率：用 OUT 增量 + 时间戳算，比字符估算准得多 ----------
  const rate = {
    hist: [],   // [ts, sumOut]
    last: null,
    push(ts, out) {
      this.hist.push([ts, out]);
      const floor = ts - 6000;
      while (this.hist.length > 2 && this.hist[0][0] < floor) this.hist.shift();
    },
    value(now) {
      const win = this.hist.filter((h) => now - h[0] <= 4000);
      if (win.length >= 2) {
        const dt = (win[win.length - 1][0] - win[0][0]) / 1000;
        const dn = win[win.length - 1][1] - win[0][1];
        if (dt >= 1 && dn > 0) { this.last = dn / dt; return this.last; }
      }
      return this.last;
    },
  };

  // ---------- 工具栏行定位（发送按钮向上找 flex+items-end） ----------
  function findToolbarRow() {
    const card = document.querySelector("[data-testid='v4-composer']");
    if (!card) return null;
    let cur = document.querySelector("[data-testid='v4-composer-send']");
    while (cur && cur !== card.parentElement) {
      if (cur.tagName === "DIV") {
        const c = typeof cur.className === "string" ? cur.className : "";
        if (/(^|\s)flex(\s|$)/.test(c) && /items-end/.test(c)) return cur;
      }
      cur = cur.parentElement;
    }
    const rows = Array.from(card.querySelectorAll("div")).filter((el) => {
      const c = typeof el.className === "string" ? el.className : "";
      return /(^|\s)flex(\s|$)/.test(c) && /items-end/.test(c);
    });
    return rows.find((el) => el.querySelector("button")) || rows[rows.length - 1] || null;
  }

  // ---------- 格式化 ----------
  const fmtTok = (v) => {
    if (v == null) return "-";
    if (v < 1000) return String(v);
    const t = (n) => String(+(n.toFixed(n < 10 ? 1 : 0)));
    if (v < 1e6) return t(v / 1e3) + "k";
    if (v < 1e9) return t(v / 1e6) + "m";
    return t(v / 1e9) + "b";
  };
  const fmtMs = (ms) => {
    if (ms == null) return null;
    const s = ms / 1000;
    return s < 10 ? (+s.toFixed(1)) + "s" : Math.round(s) + "s";
  };
  const fmtRate = (v) => (v == null ? null : (v >= 10 ? String(Math.round(v)) : String(+v.toFixed(1))));

  // ---------- 渲染 ----------
  function render() {
    try {
      const row = findToolbarRow();
      if (!row) {
        document.querySelectorAll(`[${BAR_ATTR}]`).forEach((el) => el.remove());
        return;
      }
      const streaming = isStreaming();
      const traj = readTrajectory();
      const ctx = readContextUsage();

      // 主指标：本轮输出 token（轨迹优先）
      let outTok = null, rateVal = null, durMs = null, src = null;
      if (traj && traj.count > 0) {
        outTok = traj.sumOut > 0 ? traj.sumOut : (traj.last && traj.last.out);
        durMs = traj.last && traj.last.durMs;
        src = "trajectory";
        const now = Date.now();
        if (streaming && outTok != null) rate.push(now, outTok);
        rateVal = rate.value(now);
      } else if (ctx) {
        // 回退：只有上下文用量，无法拆出输出量
        src = "context";
      }

      const hasData = outTok != null || ctx != null;
      let host = row.querySelector(`:scope > [${BAR_ATTR}]`);
      if (!hasData) {
        if (host) host.remove();
        return;
      }

      const key = [outTok, durMs, rateVal ? Math.round(rateVal) : null, streaming, src].join("|");
      if (!host) {
        host = document.createElement("div");
        host.setAttribute(BAR_ATTR, "1");
        Object.assign(host.style, {
          display: "inline-flex", alignItems: "center", gap: "6px",
          flex: "0 1 auto", minWidth: "0", maxWidth: "50%",
          marginLeft: "auto", marginRight: "auto", alignSelf: "center",
          fontSize: "11px", height: "22px", userSelect: "none",
          fontVariantNumeric: "tabular-nums", whiteSpace: "nowrap", overflow: "hidden",
          borderRadius: "999px", background: "rgba(127,127,127,0.12)", padding: "0 10px",
        });
        row.insertBefore(host, row.children[1] || null);
      }
      if (host._zk === key) return;
      host._zk = key;
      host.innerHTML = "";

      const ACCENT = "var(--color-warning, #e0983a)";
      const VALUE = "var(--color-foreground, #e8e8e8)";
      const span = (txt, cls) => {
        const s = document.createElement("span");
        s.textContent = txt;
        if (cls === "ACCENT") s.style.color = ACCENT;
        else if (cls === "VALUE") s.style.color = VALUE;
        return s;
      };
      const segs = [];
      const dot = span("●");
      dot.style.color = streaming ? "#4ade80" : "var(--color-foreground-subtle, #7a7a7a)";
      if (streaming) dot.style.textShadow = "0 0 6px rgba(74,222,128,.8)";
      segs.push({ p: 0, nodes: [dot] });

      if (outTok != null) {
        segs.push({ p: 1, nodes: [span("out "), span(fmtTok(outTok), "VALUE")] });
      } else if (ctx) {
        segs.push({ p: 1, nodes: [span("ctx "), span(fmtTok(ctx.used), "VALUE")] });
      }
      const rv = fmtRate(rateVal);
      if (rv != null && streaming) segs.push({ p: 2, nodes: [span(rv + " tok/s", "ACCENT")] });
      const dv = fmtMs(durMs);
      if (dv != null) segs.push({ p: 3, nodes: [span(dv)] });

      segs.forEach((g, i) => {
        if (i > 0) {
          const sep = span("·");
          sep.style.opacity = "0.55";
          g.nodes.unshift(sep);
        }
      });
      segs.forEach((g) => g.nodes.forEach((n) => host.appendChild(n)));

      try {
        for (const drop of [3, 2, 1]) {
          if (host.scrollWidth <= host.clientWidth + 1) break;
          const g = segs.find((x) => x.p === drop);
          if (!g) continue;
          g.nodes.forEach((n) => n.remove());
        }
      } catch (err) { /* 静默 */ }

      diag.lastRender = { outTok, rateVal, durMs, streaming, src, trajCount: traj ? traj.count : 0 };
    } catch (err) { /* 静默 */ }
  }

  // ---------- 启动 ----------
  function start() {
    setInterval(render, 1000);
    try {
      const mo = new MutationObserver(() => {
        clearTimeout(mo._t);
        mo._t = setTimeout(render, 80);   // 防抖，避免自激
      });
      mo.observe(document.body, { childList: true, subtree: true, characterData: true });
    } catch (err) { /* 静默 */ }
    render();
    diag.started = true;
  }
  if (document.body) start();
  else document.addEventListener("DOMContentLoaded", start);
})();
