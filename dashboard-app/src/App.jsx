import { useState, useEffect, useRef, useCallback } from "react";

const FONTS = `@import url('https://fonts.googleapis.com/css2?family=Space+Mono:wght@400;700&family=DM+Sans:wght@300;400;500;600&display=swap');`;

// ── Simulation helpers ─────────────────────────────────────────────────────────
const SYMBOLS = ["BTC-USD","ETH-USD","SOL-USD","AAPL","TSLA","SPY","QQQ","NVDA","XOM","WTI"];
const RISK_TYPES = ["SPOOFING","WASH TRADE","LAYERING","MOMENTUM IGN.","CROSS-VENUE"];
const SEVERITIES = ["LOW","MEDIUM","HIGH","CRITICAL"];
const SEV_WEIGHTS = [0.40, 0.35, 0.18, 0.07];

function weightedRandom(items, weights) {
  let r = Math.random(), cum = 0;
  for (let i = 0; i < items.length; i++) { cum += weights[i]; if (r < cum) return items[i]; }
  return items[items.length - 1];
}

function randBetween(a, b) { return +(a + Math.random() * (b - a)).toFixed(2); }

function genAlert() {
  const sev = weightedRandom(SEVERITIES, SEV_WEIGHTS);
  return {
    id: Math.random().toString(36).slice(2),
    ts: Date.now(),
    symbol: SYMBOLS[Math.floor(Math.random() * SYMBOLS.length)],
    type: RISK_TYPES[Math.floor(Math.random() * RISK_TYPES.length)],
    severity: sev,
    score: sev === "CRITICAL" ? randBetween(90, 100) : sev === "HIGH" ? randBetween(70, 89) : sev === "MEDIUM" ? randBetween(40, 69) : randBetween(10, 39),
    latency: Math.floor(Math.random() * 180 + 20),
    volume: Math.floor(Math.random() * 9800000 + 200000),
    status: "FLAGGED",
  };
}

function fmtTime(ts) {
  const d = new Date(ts);
  return d.toTimeString().slice(0, 8) + "." + String(d.getMilliseconds()).padStart(3, "0");
}

function fmtVol(v) {
  if (v >= 1e6) return (v / 1e6).toFixed(2) + "M";
  if (v >= 1e3) return (v / 1e3).toFixed(1) + "K";
  return v;
}

const SEV_COLOR = {
  LOW: "#3ddc97",
  MEDIUM: "#f5c518",
  HIGH: "#ff7c2a",
  CRITICAL: "#ff2d55",
};

const SEV_BG = {
  LOW: "rgba(61,220,151,0.08)",
  MEDIUM: "rgba(245,197,24,0.08)",
  HIGH: "rgba(255,124,42,0.10)",
  CRITICAL: "rgba(255,45,85,0.12)",
};

// ── Sparkline ─────────────────────────────────────────────────────────────────
function Sparkline({ data, color }) {
  if (!data || data.length < 2) return null;
  const w = 120, h = 36;
  const min = Math.min(...data), max = Math.max(...data);
  const range = max - min || 1;
  const pts = data.map((v, i) => {
    const x = (i / (data.length - 1)) * w;
    const y = h - ((v - min) / range) * (h - 4) - 2;
    return `${x},${y}`;
  }).join(" ");
  const fillPts = `0,${h} ${pts} ${w},${h}`;
  return (
    <svg width={w} height={h} style={{ display: "block" }}>
      <defs>
        <linearGradient id={`sg-${color.replace("#","")}`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.3" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <polygon points={fillPts} fill={`url(#sg-${color.replace("#","")})`} />
      <polyline points={pts} fill="none" stroke={color} strokeWidth="1.5" strokeLinejoin="round" />
    </svg>
  );
}

// ── Gauge ──────────────────────────────────────────────────────────────────────
function Gauge({ value, label, color }) {
  const r = 38, cx = 50, cy = 54;
  const startAngle = -210, endAngle = 30;
  const toRad = (d) => (d * Math.PI) / 180;
  const arcPath = (a1, a2) => {
    const x1 = cx + r * Math.cos(toRad(a1)), y1 = cy + r * Math.sin(toRad(a1));
    const x2 = cx + r * Math.cos(toRad(a2)), y2 = cy + r * Math.sin(toRad(a2));
    const large = a2 - a1 > 180 ? 1 : 0;
    return `M ${x1} ${y1} A ${r} ${r} 0 ${large} 1 ${x2} ${y2}`;
  };
  const fillAngle = startAngle + (endAngle - startAngle) * (value / 100);
  return (
    <svg viewBox="0 0 100 65" style={{ width: 100, height: 65 }}>
      <path d={arcPath(startAngle, endAngle)} fill="none" stroke="#1e2535" strokeWidth="7" strokeLinecap="round" />
      <path d={arcPath(startAngle, fillAngle)} fill="none" stroke={color} strokeWidth="7" strokeLinecap="round"
        style={{ filter: `drop-shadow(0 0 4px ${color}88)` }} />
      <text x="50" y="46" textAnchor="middle" fill={color} fontSize="14" fontFamily="'Space Mono',monospace" fontWeight="700">
        {value}
      </text>
      <text x="50" y="57" textAnchor="middle" fill="#5a6a8a" fontSize="6.5" fontFamily="'DM Sans',sans-serif" letterSpacing="0.5">
        {label}
      </text>
    </svg>
  );
}

// ── Throughput Bar ─────────────────────────────────────────────────────────────
function ThroughputBar({ tps, maxTps = 12000 }) {
  const pct = Math.min((tps / maxTps) * 100, 100);
  const color = pct > 80 ? "#ff2d55" : pct > 60 ? "#ff7c2a" : "#3ddc97";
  return (
    <div style={{ position: "relative", height: 8, background: "#1e2535", borderRadius: 4, overflow: "hidden" }}>
      <div style={{
        position: "absolute", left: 0, top: 0, height: "100%", width: `${pct}%`,
        background: `linear-gradient(90deg, ${color}88, ${color})`,
        borderRadius: 4, transition: "width 0.4s ease",
        boxShadow: `0 0 8px ${color}66`,
      }} />
    </div>
  );
}

// ── Main App ──────────────────────────────────────────────────────────────────
export default function MarketGuard() {
  const [alerts, setAlerts] = useState(() => Array.from({ length: 18 }, genAlert));
  const [tps, setTps] = useState(8420);
  const [tpsHistory, setTpsHistory] = useState(Array.from({ length: 30 }, () => Math.floor(Math.random() * 4000 + 6000)));
  const [latency, setLatency] = useState(142);
  const [latencyHistory, setLatencyHistory] = useState(Array.from({ length: 30 }, () => Math.floor(Math.random() * 80 + 80)));
  const [cacheHit, setCacheHit] = useState(94);
  const [uptime, setUptime] = useState(99.9);
  const [eventsHour, setEventsHour] = useState(1024300);
  const [filter, setFilter] = useState("ALL");
  const [selected, setSelected] = useState(null);
  const [pulse, setPulse] = useState(false);
  const [live, setLive] = useState(true);
  const intervalRef = useRef(null);

  const tick = useCallback(() => {
    const newTps = Math.floor(randBetween(7800, 11200));
    const newLat = Math.floor(randBetween(85, 195));
    setTps(newTps);
    setTpsHistory(h => [...h.slice(1), newTps]);
    setLatency(newLat);
    setLatencyHistory(h => [...h.slice(1), newLat]);
    setCacheHit(+(randBetween(91, 97)).toFixed(1));
    setEventsHour(h => h + Math.floor(randBetween(200, 600)));

    if (Math.random() < 0.55) {
      const a = genAlert();
      setPulse(true);
      setTimeout(() => setPulse(false), 600);
      setAlerts(prev => [a, ...prev].slice(0, 80));
    }
  }, []);

  useEffect(() => {
    if (live) { intervalRef.current = setInterval(tick, 800); }
    else { clearInterval(intervalRef.current); }
    return () => clearInterval(intervalRef.current);
  }, [live, tick]);

  const filtered = filter === "ALL" ? alerts : alerts.filter(a => a.severity === filter);

  const counts = alerts.reduce((acc, a) => { acc[a.severity] = (acc[a.severity] || 0) + 1; return acc; }, {});
  const totalFlags = alerts.length;

  const styles = {
    root: {
      fontFamily: "'DM Sans', sans-serif",
      background: "#080d18",
      minHeight: "100vh",
      color: "#c8d8f0",
      padding: "20px 24px",
      boxSizing: "border-box",
    },
    header: {
      display: "flex", alignItems: "center", justifyContent: "space-between",
      marginBottom: 20, paddingBottom: 16,
      borderBottom: "1px solid #1a2540",
    },
    logo: {
      display: "flex", alignItems: "center", gap: 10,
    },
    logoIcon: {
      width: 36, height: 36, borderRadius: 8,
      background: "linear-gradient(135deg, #ff2d55, #ff7c2a)",
      display: "flex", alignItems: "center", justifyContent: "center",
      fontSize: 18, boxShadow: "0 0 16px #ff2d5566",
    },
    logoText: {
      fontFamily: "'Space Mono', monospace",
      fontSize: 18, fontWeight: 700, letterSpacing: 1,
      color: "#e8f0ff",
    },
    logoSub: { fontSize: 11, color: "#5a6a8a", letterSpacing: 2, textTransform: "uppercase" },
    liveBtn: {
      display: "flex", alignItems: "center", gap: 8,
      padding: "6px 16px", borderRadius: 20,
      border: `1px solid ${live ? "#3ddc97" : "#2a3555"}`,
      background: live ? "rgba(61,220,151,0.08)" : "transparent",
      color: live ? "#3ddc97" : "#5a6a8a",
      fontSize: 12, fontFamily: "'Space Mono', monospace",
      cursor: "pointer", letterSpacing: 1,
      transition: "all 0.2s",
    },
    dot: {
      width: 6, height: 6, borderRadius: "50%",
      background: live ? "#3ddc97" : "#5a6a8a",
      boxShadow: live ? "0 0 6px #3ddc97" : "none",
      animation: live ? "blink 1.2s infinite" : "none",
    },
    grid3: {
      display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 14, marginBottom: 14,
    },
    grid4: {
      display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 14, marginBottom: 14,
    },
    card: {
      background: "#0d1526", border: "1px solid #1a2540",
      borderRadius: 12, padding: "16px 18px",
    },
    metricLabel: {
      fontSize: 10, fontFamily: "'Space Mono', monospace",
      letterSpacing: 1.5, color: "#5a6a8a",
      textTransform: "uppercase", marginBottom: 6,
    },
    metricValue: {
      fontFamily: "'Space Mono', monospace",
      fontSize: 26, fontWeight: 700, lineHeight: 1,
    },
    metricSub: { fontSize: 11, color: "#5a6a8a", marginTop: 4 },
    table: { width: "100%", borderCollapse: "collapse" },
    th: {
      fontFamily: "'Space Mono', monospace",
      fontSize: 9, letterSpacing: 1.5, color: "#3a4a6a",
      textTransform: "uppercase", padding: "0 10px 10px",
      textAlign: "left", borderBottom: "1px solid #1a2540",
    },
    td: {
      padding: "9px 10px", fontSize: 12, borderBottom: "1px solid #0f1828",
      verticalAlign: "middle",
    },
    badge: (sev) => ({
      display: "inline-flex", alignItems: "center",
      padding: "2px 8px", borderRadius: 4,
      fontSize: 10, fontFamily: "'Space Mono', monospace",
      fontWeight: 700, letterSpacing: 0.5,
      color: SEV_COLOR[sev], background: SEV_BG[sev],
      border: `1px solid ${SEV_COLOR[sev]}44`,
    }),
    filterBar: {
      display: "flex", gap: 8, marginBottom: 14,
    },
    filterBtn: (active, sev) => ({
      padding: "5px 14px", borderRadius: 6,
      border: `1px solid ${active ? (SEV_COLOR[sev] || "#3d8bff") : "#1a2540"}`,
      background: active ? (SEV_BG[sev] || "rgba(61,139,255,0.08)") : "transparent",
      color: active ? (SEV_COLOR[sev] || "#3d8bff") : "#5a6a8a",
      fontSize: 11, fontFamily: "'Space Mono', monospace",
      cursor: "pointer", letterSpacing: 0.5,
      transition: "all 0.15s",
    }),
    scoreBar: (score) => ({
      height: 3, width: `${score}%`, borderRadius: 2,
      background: score > 89 ? "#ff2d55" : score > 69 ? "#ff7c2a" : score > 39 ? "#f5c518" : "#3ddc97",
      boxShadow: `0 0 4px ${score > 89 ? "#ff2d55" : score > 69 ? "#ff7c2a" : score > 39 ? "#f5c518" : "#3ddc97"}88`,
    }),
    sectionTitle: {
      fontFamily: "'Space Mono', monospace",
      fontSize: 10, letterSpacing: 2, color: "#3a4a6a",
      textTransform: "uppercase", marginBottom: 12,
    },
  };

  return (
    <>
      <style>{`
        ${FONTS}
        * { box-sizing: border-box; margin: 0; padding: 0; }
        ::-webkit-scrollbar { width: 4px; } 
        ::-webkit-scrollbar-track { background: #080d18; }
        ::-webkit-scrollbar-thumb { background: #1a2540; border-radius: 2px; }
        @keyframes blink { 0%,100%{opacity:1} 50%{opacity:0.3} }
        @keyframes slideIn { from{opacity:0;transform:translateY(-8px)} to{opacity:1;transform:translateY(0)} }
        @keyframes flashBorder { 0%{border-color:#ff2d5588} 50%{border-color:#ff2d55} 100%{border-color:#1a2540} }
        .alert-row { animation: slideIn 0.3s ease; }
        .alert-row:hover { background: #111c30 !important; cursor: pointer; }
        .pulse-card { animation: flashBorder 0.6s ease; }
      `}</style>
      <div style={styles.root}>

        {/* Header */}
        <div style={styles.header}>
          <div style={styles.logo}>
            <div style={styles.logoIcon}>⚡</div>
            <div>
              <div style={styles.logoText}>MARKETGUARD</div>
              <div style={styles.logoSub}>Real-Time Risk Monitoring</div>
            </div>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
            <span style={{ fontFamily: "'Space Mono', monospace", fontSize: 11, color: "#3a4a6a" }}>
              {new Date().toISOString().slice(0, 19).replace("T", " ")} UTC
            </span>
            <button style={styles.liveBtn} onClick={() => setLive(l => !l)}>
              <div style={styles.dot} />
              {live ? "LIVE" : "PAUSED"}
            </button>
          </div>
        </div>

        {/* Top KPI row */}
        <div style={styles.grid4}>
          {/* TPS */}
          <div style={{ ...styles.card, borderColor: pulse ? "#ff2d5588" : "#1a2540" }} className={pulse ? "pulse-card" : ""}>
            <div style={styles.metricLabel}>Throughput</div>
            <div style={{ ...styles.metricValue, color: "#3ddc97" }}>{tps.toLocaleString()}</div>
            <div style={styles.metricSub}>transactions / sec</div>
            <div style={{ marginTop: 10 }}>
              <ThroughputBar tps={tps} />
            </div>
            <div style={{ marginTop: 8 }}><Sparkline data={tpsHistory} color="#3ddc97" /></div>
          </div>

          {/* Latency */}
          <div style={styles.card}>
            <div style={styles.metricLabel}>Detection Latency</div>
            <div style={{ ...styles.metricValue, color: latency < 200 ? "#3ddc97" : "#ff7c2a" }}>
              {latency}<span style={{ fontSize: 14, fontWeight: 400 }}>ms</span>
            </div>
            <div style={styles.metricSub}>p99 end-to-end</div>
            <div style={{ marginTop: 8 }}><Sparkline data={latencyHistory} color={latency < 200 ? "#3ddc97" : "#ff7c2a"} /></div>
          </div>

          {/* Cache Hit */}
          <div style={styles.card}>
            <div style={styles.metricLabel}>Redis Cache Hit Rate</div>
            <div style={{ ...styles.metricValue, color: "#f5c518" }}>{cacheHit}<span style={{ fontSize: 14 }}>%</span></div>
            <div style={styles.metricSub}>80% latency reduction</div>
            <div style={{ marginTop: 12 }}>
              <div style={{ display: "flex", gap: 16, justifyContent: "center" }}>
                <Gauge value={Math.round(cacheHit)} label="CACHE" color="#f5c518" />
                <Gauge value={99} label="UPTIME" color="#3ddc97" />
              </div>
            </div>
          </div>

          {/* Events */}
          <div style={styles.card}>
            <div style={styles.metricLabel}>Events / Hour</div>
            <div style={{ ...styles.metricValue, color: "#3d8bff" }}>
              {(eventsHour / 1e6).toFixed(2)}<span style={{ fontSize: 14 }}>M</span>
            </div>
            <div style={styles.metricSub}>target 1M+ sustained</div>
            <div style={{ marginTop: 12, display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
              {["LOW","MEDIUM","HIGH","CRITICAL"].map(s => (
                <div key={s} style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
                  <span style={{ fontSize: 10, color: SEV_COLOR[s], fontFamily: "'Space Mono', monospace" }}>{s.slice(0,3)}</span>
                  <span style={{ fontSize: 13, fontFamily: "'Space Mono', monospace", color: "#e8f0ff" }}>{counts[s] || 0}</span>
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* System status row */}
        <div style={{ ...styles.grid3, gridTemplateColumns: "1fr 1fr 2fr" }}>
          {/* Services */}
          <div style={styles.card}>
            <div style={styles.sectionTitle}>Microservices</div>
            {[
              { name: "Kafka Broker", status: "HEALTHY", latency: "4ms" },
              { name: "Risk Engine", status: "HEALTHY", latency: "12ms" },
              { name: "Redis Cache", status: "HEALTHY", latency: "1ms" },
              { name: "PostgreSQL", status: "HEALTHY", latency: "8ms" },
              { name: "gRPC Gateway", status: "HEALTHY", latency: "6ms" },
            ].map(svc => (
              <div key={svc.name} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "6px 0", borderBottom: "1px solid #0f1828" }}>
                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  <div style={{ width: 5, height: 5, borderRadius: "50%", background: "#3ddc97", boxShadow: "0 0 5px #3ddc97" }} />
                  <span style={{ fontSize: 12 }}>{svc.name}</span>
                </div>
                <span style={{ fontFamily: "'Space Mono', monospace", fontSize: 10, color: "#5a6a8a" }}>{svc.latency}</span>
              </div>
            ))}
          </div>

          {/* Infrastructure */}
          <div style={styles.card}>
            <div style={styles.sectionTitle}>Infrastructure</div>
            {[
              { label: "Deployment", value: "Docker Compose", icon: "🐳" },
              { label: "Cloud", value: "AWS EC2", icon: "☁️" },
              { label: "Auth", value: "JWT + HTTPS", icon: "🔐" },
              { label: "Monitoring", value: "Prometheus", icon: "📊" },
              { label: "API Docs", value: "Swagger / REST", icon: "📘" },
            ].map(item => (
              <div key={item.label} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "6px 0", borderBottom: "1px solid #0f1828" }}>
                <span style={{ fontSize: 11, color: "#5a6a8a" }}>{item.icon} {item.label}</span>
                <span style={{ fontSize: 11, fontFamily: "'Space Mono', monospace", color: "#c8d8f0" }}>{item.value}</span>
              </div>
            ))}
          </div>

          {/* Goroutine worker pool viz */}
          <div style={styles.card}>
            <div style={styles.sectionTitle}>Concurrent Risk Engine — Worker Pool</div>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 5 }}>
              {Array.from({ length: 48 }, (_, i) => {
                const active = Math.random() < 0.72;
                const busy = Math.random() < 0.4;
                return (
                  <div key={i} style={{
                    width: 14, height: 14, borderRadius: 3,
                    background: active ? (busy ? "#ff7c2a" : "#3ddc97") : "#1a2535",
                    boxShadow: active ? `0 0 5px ${busy ? "#ff7c2a" : "#3ddc97"}88` : "none",
                    transition: "all 0.3s",
                    opacity: active ? 1 : 0.4,
                  }} title={`Goroutine ${i + 1}: ${active ? (busy ? "Processing" : "Idle") : "Stopped"}`} />
                );
              })}
            </div>
            <div style={{ display: "flex", gap: 16, marginTop: 12 }}>
              {[["#3ddc97", "IDLE"], ["#ff7c2a", "BUSY"], ["#1a2535", "STOPPED"]].map(([c, l]) => (
                <div key={l} style={{ display: "flex", alignItems: "center", gap: 5 }}>
                  <div style={{ width: 8, height: 8, borderRadius: 2, background: c }} />
                  <span style={{ fontSize: 10, fontFamily: "'Space Mono', monospace", color: "#5a6a8a" }}>{l}</span>
                </div>
              ))}
            </div>
            <div style={{ marginTop: 10, display: "flex", gap: 20 }}>
              <div>
                <div style={{ fontSize: 10, color: "#5a6a8a" }}>Active Goroutines</div>
                <div style={{ fontFamily: "'Space Mono', monospace", fontSize: 18, color: "#3ddc97" }}>
                  {Math.floor(randBetween(24, 38))}
                  <span style={{ fontSize: 11, color: "#5a6a8a" }}>/48</span>
                </div>
              </div>
              <div>
                <div style={{ fontSize: 10, color: "#5a6a8a" }}>Queue Depth</div>
                <div style={{ fontFamily: "'Space Mono', monospace", fontSize: 18, color: "#f5c518" }}>
                  {Math.floor(randBetween(80, 340))}
                </div>
              </div>
              <div>
                <div style={{ fontSize: 10, color: "#5a6a8a" }}>Spoof Detected</div>
                <div style={{ fontFamily: "'Space Mono', monospace", fontSize: 18, color: "#ff2d55" }}>
                  {counts["CRITICAL"] || 0}
                </div>
              </div>
            </div>
          </div>
        </div>

        {/* Alert Feed */}
        <div style={styles.card}>
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 12 }}>
            <div style={styles.sectionTitle}>
              Live Alert Feed
              <span style={{ marginLeft: 8, padding: "1px 8px", borderRadius: 10, background: "#1a2540", fontSize: 10, color: "#3d8bff" }}>
                {filtered.length} events
              </span>
            </div>
            <div style={styles.filterBar}>
              {["ALL", "CRITICAL", "HIGH", "MEDIUM", "LOW"].map(f => (
                <button key={f} style={styles.filterBtn(filter === f, f)} onClick={() => setFilter(f)}>
                  {f}
                </button>
              ))}
            </div>
          </div>

          <div style={{ overflowY: "auto", maxHeight: 340 }}>
            <table style={styles.table}>
              <thead>
                <tr>
                  {["Time", "Symbol", "Risk Type", "Severity", "Score", "Latency", "Volume", "Status"].map(h => (
                    <th key={h} style={styles.th}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {filtered.map((a, idx) => (
                  <tr key={a.id} className="alert-row"
                    style={{ background: selected === a.id ? "#111c30" : idx % 2 === 0 ? "transparent" : "#0a1020" }}
                    onClick={() => setSelected(s => s === a.id ? null : a.id)}>
                    <td style={{ ...styles.td, fontFamily: "'Space Mono', monospace", fontSize: 11, color: "#5a6a8a" }}>
                      {fmtTime(a.ts)}
                    </td>
                    <td style={{ ...styles.td, fontFamily: "'Space Mono', monospace", color: "#3d8bff", fontSize: 12 }}>
                      {a.symbol}
                    </td>
                    <td style={{ ...styles.td, fontSize: 11 }}>{a.type}</td>
                    <td style={styles.td}><span style={styles.badge(a.severity)}>{a.severity}</span></td>
                    <td style={styles.td}>
                      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                        <div style={{ flex: 1, height: 3, background: "#1a2540", borderRadius: 2 }}>
                          <div style={styles.scoreBar(a.score)} />
                        </div>
                        <span style={{ fontFamily: "'Space Mono', monospace", fontSize: 11, color: "#c8d8f0", minWidth: 28 }}>
                          {a.score}
                        </span>
                      </div>
                    </td>
                    <td style={{ ...styles.td, fontFamily: "'Space Mono', monospace", fontSize: 11, color: a.latency < 200 ? "#3ddc97" : "#ff7c2a" }}>
                      {a.latency}ms
                    </td>
                    <td style={{ ...styles.td, fontFamily: "'Space Mono', monospace", fontSize: 11, color: "#c8d8f0" }}>
                      {fmtVol(a.volume)}
                    </td>
                    <td style={styles.td}>
                      <span style={{ fontSize: 10, fontFamily: "'Space Mono', monospace", color: "#ff2d55", letterSpacing: 0.5 }}>
                        ⚑ {a.status}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

      </div>
    </>
  );
}
