package web

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>ollamaHtop</title>
<style>
:root {
  --bg: #0d1117; --surface: #161b22; --border: #30363d;
  --text: #e6edf3; --dim: #7d8590; --green: #3fb950;
  --amber: #d29922; --red: #f85149; --blue: #58a6ff;
  --pink: #f778ba;
}
* { box-sizing: border-box; margin: 0; padding: 0; }
body { background: var(--bg); color: var(--text); font-family: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace; font-size: 13px; padding: 16px; }
h1 { color: var(--blue); font-size: 18px; font-weight: bold; }
.header { display: flex; justify-content: space-between; align-items: baseline; margin-bottom: 16px; border-bottom: 1px solid var(--border); padding-bottom: 8px; }
.meta { color: var(--dim); font-size: 11px; margin-top: 2px; }
.status-ok { color: var(--green); }
.status-err { color: var(--red); }
section { margin-bottom: 20px; }
h2 { color: var(--blue); font-size: 13px; border-bottom: 1px solid var(--border); padding-bottom: 4px; margin-bottom: 8px; }
table { width: 100%; border-collapse: collapse; }
th { color: var(--dim); font-weight: normal; text-align: left; padding: 2px 8px 4px 0; white-space: nowrap; }
td { padding: 3px 8px 3px 0; border-top: 1px solid var(--border); white-space: nowrap; vertical-align: top; }
.tok { color: var(--green); font-weight: bold; }
.idle { color: var(--dim); }
.running { color: var(--green); }
.thinking { color: var(--pink); }
.green { color: var(--green); font-weight: bold; }
.amber { color: var(--amber); font-weight: bold; }
.red { color: var(--red); font-weight: bold; }
.dim { color: var(--dim); }
.bar-wrap { display: flex; align-items: center; gap: 6px; margin-bottom: 4px; }
.bar { height: 8px; border-radius: 2px; background: var(--border); width: 120px; flex-shrink: 0; }
.bar-fill { height: 100%; border-radius: 2px; background: var(--green); transition: width 0.4s; }
.bar-fill.warm { background: var(--amber); }
.bar-fill.hot  { background: var(--red); }
.pct { color: var(--dim); min-width: 40px; }
.temp { min-width: 50px; }
.temp.warm { color: var(--amber); }
.temp.hot  { color: var(--red); }
.sparkline { display: inline-flex; align-items: flex-end; gap: 1px; height: 20px; vertical-align: middle; }
.sparkline span { width: 4px; background: var(--green); border-radius: 1px; min-height: 1px; }
.metric-row { display: flex; align-items: center; gap: 10px; margin-bottom: 5px; flex-wrap: wrap; }
.metric-row .label { color: var(--dim); min-width: 58px; }
.detail-row td { border-top: none; color: var(--dim); font-size: 11px; padding-top: 0; }
</style>
</head>
<body>
<div class="header">
  <div>
    <h1>ollamaHtop</h1>
    <div class="meta" id="hostinfo"></div>
  </div>
  <div style="text-align:right">
    <div id="status" class="meta">—</div>
    <div id="timestamp" class="meta"></div>
  </div>
</div>

<section>
  <h2>Models</h2>
  <table id="models-table">
    <thead><tr>
      <th>Model</th><th>Size</th><th>VRAM</th>
      <th>Tok/s</th><th>Prompt/s</th><th>TTFT</th><th>Status</th><th>Expires</th>
    </tr></thead>
    <tbody id="models-body"></tbody>
  </table>
</section>

<section id="throughput-section">
  <h2>Throughput</h2>
  <div id="throughput"></div>
</section>

<section>
  <h2>System</h2>
  <div id="system"></div>
</section>

<script>
const fmt = {
  mb:      b  => b >= 1024 ? (b/1024).toFixed(1)+' GB' : b.toFixed(0)+' MB',
  pct:     p  => p.toFixed(0)+'%',
  tps:     t  => t > 0 ? '<span class="tok">'+t.toFixed(1)+'</span>' : '<span class="idle">—</span>',
  ms:      ms => ms <= 0 ? '—' : ms < 1000 ? ms.toFixed(0)+'ms' : (ms/1000).toFixed(2)+'s',
  expires: s  => s <= 0 ? '—' : s >= 60 ? Math.floor(s/60)+'m'+String(Math.floor(s%60)).padStart(2,'0')+'s' : s.toFixed(0)+'s',
};

function barHtml(pct, warn=60, crit=85) {
  const cls = pct >= crit ? 'hot' : pct >= warn ? 'warm' : '';
  return '<div class="bar"><div class="bar-fill '+cls+'" style="width:'+Math.min(pct,100).toFixed(0)+'%"></div></div>';
}

function tempHtml(t) {
  if (!t) return '';
  const cls = t >= 85 ? 'hot' : t >= 70 ? 'warm' : '';
  return '<span class="temp '+cls+'">'+t.toFixed(0)+'°C</span>';
}

function sparkHtml(hist) {
  if (!hist || !hist.length) return '<span class="sparkline"></span>';
  const max = Math.max(...hist, 1);
  const bars = hist.slice(-30).map(v => {
    const h = Math.max(1, Math.round(v/max*20));
    return '<span style="height:'+h+'px"></span>';
  }).join('');
  return '<span class="sparkline">'+bars+'</span>';
}

function statusHtml(s) {
  switch (s) {
    case 'running':  return '<span class="running">● running</span>';
    case 'thinking': return '<span class="thinking">● thinking</span>';
    default:         return '<span class="idle">○ idle</span>';
  }
}

function renderModels(d) {
  const has = d.has_capture;
  const tbody = document.getElementById('models-body');
  if (!d.models || d.models.length === 0) {
    tbody.innerHTML = '<tr><td colspan="8" class="idle">No models loaded</td></tr>';
    return;
  }
  const rows = [];
  for (const m of d.models) {
    rows.push(
      '<tr>'+
      '<td>'+m.name+'</td>'+
      '<td>'+fmt.mb(m.size_mb)+'</td>'+
      '<td>'+fmt.mb(m.vram_used_mb)+'</td>'+
      '<td>'+(has ? fmt.tps(m.output_tps)  : '<span class="idle">—</span>')+'</td>'+
      '<td>'+(has ? fmt.tps(m.input_tps)   : '<span class="idle">—</span>')+'</td>'+
      '<td>'+(has ? fmt.ms(m.ttft_ms)      : '—')+'</td>'+
      '<td>'+statusHtml(m.status)+'</td>'+
      '<td>'+fmt.expires(m.until_expiry_sec)+'</td>'+
      '</tr>'
    );
    if (has && m.last_total_dur_ms > 0 && m.status !== 'running' && m.status !== 'thinking') {
      let detail = 'last ' + fmt.ms(m.last_total_dur_ms);
      if (m.last_ms_per_token > 0) detail += '  &middot;  ' + m.last_ms_per_token.toFixed(1) + ' ms/tok';
      rows.push('<tr class="detail-row"><td></td><td colspan="7">'+detail+'</td></tr>');
    }
  }
  tbody.innerHTML = rows.join('');
}

function renderThroughput(d) {
  const section = document.getElementById('throughput-section');
  const el      = document.getElementById('throughput');
  if (!d.has_capture) {
    section.style.display = 'none';
    return;
  }
  section.style.display = '';
  const tp  = d.perf;
  const sys = d.system;
  let html = '';

  html +=
    '<div class="metric-row">'+
    '<span class="label">tok/s</span>'+
    sparkHtml(tp.output_history)+
    '<span class="'+(tp.output_tps > 0 ? 'green' : 'idle')+'">'+tp.output_tps.toFixed(1)+' tok/s</span>'+
    (tp.peak_output_tps > 0 ? '<span class="dim">max '+tp.peak_output_tps.toFixed(1)+'</span>' : '')+
    '</div>';

  html +=
    '<div class="metric-row">'+
    '<span class="label">prompt</span>'+
    sparkHtml(tp.input_history || [])+
    '<span class="'+(tp.input_tps > 0 ? 'green' : 'idle')+'">'+tp.input_tps.toFixed(1)+' tok/s</span>'+
    (tp.peak_input_tps > 0 ? '<span class="dim">max '+tp.peak_input_tps.toFixed(1)+'</span>' : '')+
    '</div>';

  if (tp.completions_per_sec > 0 || tp.mean_latency_ms > 0) {
    html +=
      '<div class="metric-row">'+
      '<span class="label">latency</span>'+
      '<span class="green">'+fmt.ms(tp.mean_latency_ms)+'</span><span class="dim"> avg</span>'+
      '&ensp;<span class="amber">'+fmt.ms(tp.p95_latency_ms)+'</span><span class="dim"> p95</span>'+
      '&ensp;<span class="red">'+fmt.ms(tp.p99_latency_ms)+'</span><span class="dim"> p99</span>'+
      '&ensp;<span class="dim">'+tp.completions_per_sec.toFixed(2)+' req/s</span>'+
      (tp.mean_ms_per_token > 0 ? '&ensp;<span class="dim">'+tp.mean_ms_per_token.toFixed(1)+' ms/tok</span>' : '')+
      '</div>';
  }

  if (sys.gpu_power_w > 0) {
    html +=
      '<div class="metric-row">'+
      '<span class="label">power</span>'+
      '<span class="green">'+sys.gpu_power_w.toFixed(1)+'W</span><span class="dim"> GPU</span>'+
      (sys.tok_per_watt > 0 ? '&ensp;<span class="green">'+sys.tok_per_watt.toFixed(1)+'</span><span class="dim"> tok/W</span>' : '')+
      '</div>';
  }

  el.innerHTML = html;
}

function renderSystem(d) {
  const sys = d.system;
  let html = '';

  html +=
    '<div class="bar-wrap">'+
    '<span style="min-width:42px;color:var(--dim)">CPU</span>'+
    barHtml(sys.cpu_percent)+
    '<span class="pct">'+fmt.pct(sys.cpu_percent)+'</span>'+
    tempHtml(sys.cpu_temp_c)+
    '</div>';

  if (sys.gpu_avail) {
    const gpuLabel = sys.gpu_name ? sys.gpu_name.substring(0, 10) : 'GPU';
    html +=
      '<div class="bar-wrap">'+
      '<span style="min-width:42px;color:var(--dim)">GPU</span>'+
      barHtml(sys.gpu_percent, 50, 80)+
      '<span class="pct">'+fmt.pct(sys.gpu_percent)+'</span>'+
      tempHtml(sys.gpu_temp_c)+
      (sys.gpu_name ? '<span class="dim">'+sys.gpu_name+'</span>' : '')+
      (sys.gpu_power_w > 0 ? '&ensp;<span class="dim">'+sys.gpu_power_w.toFixed(1)+'W</span>' : '')+
      (sys.tok_per_watt > 0 ? '<span class="dim">&ensp;'+sys.tok_per_watt.toFixed(1)+' tok/W</span>' : '')+
      '</div>';
  }

  html +=
    '<div class="bar-wrap">'+
    '<span style="min-width:42px;color:var(--dim)">RAM</span>'+
    barHtml(sys.mem_percent, 60, 85)+
    '<span class="pct">'+fmt.pct(sys.mem_percent)+'</span>'+
    '<span class="dim">'+fmt.mb(sys.mem_used_mb)+' / '+fmt.mb(sys.mem_total_mb)+'</span>'+
    '</div>';

  html +=
    '<div class="meta" style="margin-top:6px">'+
    'Load: '+sys.load_avg_1.toFixed(2)+'&ensp;'+sys.load_avg_5.toFixed(2)+'&ensp;'+sys.load_avg_15.toFixed(2)+
    '</div>';

  document.getElementById('system').innerHTML = html;
}

function render(d) {
  document.getElementById('status').innerHTML = d.connected
    ? '<span class="status-ok">● connected</span>&ensp;Ollama v'+d.version
    : '<span class="status-err">✕ disconnected</span>';
  document.getElementById('timestamp').textContent =
    new Date(d.timestamp).toLocaleTimeString('en-US');
  document.getElementById('hostinfo').textContent =
    [d.system.hostname, d.system.os_version].filter(Boolean).join('  ·  ');

  renderModels(d);
  renderThroughput(d);
  renderSystem(d);
}

const es = new EventSource('/api/events');
es.onmessage = e => { try { render(JSON.parse(e.data)); } catch(ex) { console.error(ex); } };
es.onerror = () => {
  document.getElementById('status').innerHTML = '<span class="status-err">✕ SSE connection lost</span>';
};
</script>
</body>
</html>`
