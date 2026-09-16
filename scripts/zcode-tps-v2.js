/**
 * ZCode 状态胶囊 v2（3.12.2+ 安全版）—— 纯 DOM 观测，零端口
 * =========================================================
 * 与 v1 的根本区别：**不接触 ServicePort**
 *
 * v1 靠 window.postMessage 转交的 ServicePort 截取会话事件流。实测（Node MessagePort
 * 复现 + 用户实机验证）证明该方案在 3.12.2 会破坏客户端启动：
 *   MessagePort 的消息只分发一次给"分发时已注册的监听器"；v1 在普通脚本阶段
 *   （早于 type=module 的应用主包）就注册并 start，导致服务端的 Initialize 握手
 *   被我们消费掉 —— 应用的协议客户端永远收不到握手，启动流程卡死。
 *   复现：我们收到 ["Initialize","usage.delta"]，应用只收到 ["usage.delta"]。
 * 因此 v2 改为**只观察 DOM**：ZCode 把关键状态直接写在 composer 的隐藏节点上：
 *   <span data-testid="v4-model-config" data-usage-used="29568" data-usage-max="1000000"
 *         data-thought="high" data-thought-levels="disabled,low,high,max" data-model="..." />
 *
 * 能力（诚实说明）：上下文用量、模型、档位、生成中状态、生成耗时、由 DOM 变动速率
 * 估算的"输出速度（字符/秒，粗略）"。**拿不到**精确 usage delta / 首 token 延迟。
 *
 * 安全性：只读 DOM、只写自己的 host 元素；不 start 任何端口；不调用应用 API；
 * 任何异常静默；未找到 composer 时自清理，绝不残留。
 */
(() => {
  if (window.__ztps2) return;
  window.__ztps2 = true;

  const BAR_ATTR = "data-ztps2-bar";
  const diag = (window.__ztps2Diag = {
    version: "2.0-dom",
    loadedAt: new Date().toISOString(),
    portTouched: false,   // 恒为 false —— 本版本保证不碰端口
  });

  const ATTRS = {
    usageUsed: "data-usage-used",
    usageMax: "data-usage-max",
    model: "data-model",
    provider: "data-provider",
    thought: "data-thought",
    thoughtLevels: "data-thought-levels",
  };

  // ---------- 读取 DOM 上的会话指标 ----------
  function readConfigNode() {
    // 优先 composer 工具条里的那个（当前会话），回退到页面内任意一个
    const all = document.querySelectorAll("[data-testid='v4-model-config']");
    if (!all.length) return null;
    // 选可见的、最靠下的（composer 在底部）
    const vis = Array.from(all).filter((el) => el.offsetParent != null);
    const list = vis.length ? vis : Array.from(all);
    return list.sort((a, b) => b.getBoundingClientRect().top - a.getBoundingClientRect().top)[0];
  }

  function readStats() {
    const el = readConfigNode();
    if (!el) return null;
    const num = (v) => {
      const n = Number(String(v ?? "").replace(/[,_\s]/g, ""));
      return Number.isFinite(n) ? n : null;
    };
    return {
      used: num(el.getAttribute(ATTRS.usageUsed)),
      max: num(el.getAttribute(ATTRS.usageMax)),
      model: el.getAttribute(ATTRS.model) || "",
      provider: el.getAttribute(ATTRS.provider) || "",
      thought: el.getAttribute(ATTRS.thought) || "",
      levels: el.getAttribute(ATTRS.thoughtLevels) || "",
    };
  }

  // ---------- 生成中判定 + 输出速率估算 ----------
  // 依据：会话区文本长度 + 停止按钮存在性。3.12.2 的停止按钮 testid 为 v4-stop，
  // 生成中才渲染；同时用文本长度变化速率估算字符/秒。
  const textLen = () => {
    let n = 0;
    document.querySelectorAll("section[data-turn-id]").forEach((s) => {
      n += (s.textContent || "").length;
    });
    return n;
  };
  const isStreaming = () =>
    !!document.querySelector("[data-testid='v4-stop'], [data-testid='v4-composer-stop']");

  const speed = {
    samples: [],   // [ts, len]
    lastTps: null,
    push(len) {
      const now = Date.now();
      this.samples.push([now, len]);
      const floor = now - 5000;
      while (this.samples.length > 2 && this.samples[0][0] < floor) this.samples.shift();
    },
    value() {
      const win = this.samples.filter((s) => Date.now() - s[0] <= 4000);
      if (win.length >= 2) {
        const dt = (win[win.length - 1][0] - win[0][0]) / 1000;
        const dn = win[win.length - 1][1] - win[0][1];
        if (dt >= 1 && dn > 0) {
          this.lastTps = dn / dt;
          return this.lastTps;
        }
      }
      return this.lastTps;   // 静默期保持最近值
    },
  };

  // ---------- 工具栏行定位（与 v1 一致：发送按钮向上找 flex+items-end 行） ----------
  function findToolbarRow() {
    const card = document.querySelector("[data-testid='v4-composer']");
    if (!card) return null;
    const send = document.querySelector("[data-testid='v4-composer-send']");
    let cur = send;
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
  const fmtSpeed = (v) => (v == null ? null : (v >= 10 ? String(Math.round(v)) : String(+v.toFixed(1))));

  // ---------- 渲染 ----------
  function render() {
    try {
      const row = findToolbarRow();
      if (!row) {
        document.querySelectorAll(`[${BAR_ATTR}]`).forEach((el) => el.remove());
        return;
      }
      const st = readStats();
      if (!st || st.used == null) {
        const old = row.querySelector(`:scope > [${BAR_ATTR}]`);
        if (old) old.remove();
        return;
      }
      const streaming = isStreaming();
      const len = textLen();
      if (streaming) speed.push(len);
      const spd = streaming ? speed.value() : speed.value();

      const pct = st.max ? Math.round((st.used / st.max) * 1000) / 10 : null;
      const key = [st.used, st.max, st.thought, st.model, streaming, spd ? Math.round(spd) : null].join("|");

      let host = row.querySelector(`:scope > [${BAR_ATTR}]`);
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

      segs.push({ p: 1, nodes: [span("ctx "), span(fmtTok(st.used), "VALUE"),
                                span("/" + fmtTok(st.max))] });
      if (pct != null) segs.push({ p: 2, nodes: [span(pct + "%", "VALUE")] });
      if (spd != null && streaming) segs.push({ p: 3, nodes: [span(fmtSpeed(spd) + " 字符/s", "ACCENT")] });
      if (st.thought) segs.push({ p: 4, nodes: [span(st.thought)] });

      segs.forEach((g, i) => {
        if (i > 0) {
          const sep = span("·");
          sep.style.opacity = "0.55";
          g.nodes.unshift(sep);
        }
      });
      segs.forEach((g) => g.nodes.forEach((n) => host.appendChild(n)));

      // 溢出降级：先丢档位、再丢百分比，保留 ctx 与速度
      try {
        for (const drop of [4, 2, 1]) {
          if (host.scrollWidth <= host.clientWidth + 1) break;
          const g = segs.find((x) => x.p === drop);
          if (!g) continue;
          g.nodes.forEach((n) => n.remove());
        }
      } catch (err) { /* 静默 */ }

      diag.lastRender = { used: st.used, max: st.max, streaming, spd, thought: st.thought };
    } catch (err) { /* 静默 */ }
  }

  // ---------- 启动 ----------
  function start() {
    setInterval(render, 1000);          // 指标刷新（含流式速率采样）
    try {
      const mo = new MutationObserver(() => {
        clearTimeout(mo._t);
        mo._t = setTimeout(render, 80);  // 防抖，避免自激
      });
      mo.observe(document.body, { childList: true, subtree: true, characterData: true });
    } catch (err) { /* 静默 */ }
    render();
    diag.started = true;
  }
  if (document.body) start();
  else document.addEventListener("DOMContentLoaded", start);
})();
