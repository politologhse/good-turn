// === DOM ===
const $ = id => document.getElementById(id);

const el = {
  vkLink:      $('vkLink'),
  peerAddr:    $('peerAddr'),
  hyPassword:  $('hyPassword'),
  sni:         $('sni'),
  turnMode:    $('turnMode'),
  streams:     $('streams'),
  socksPort:   $('socksPort'),
  httpPort:    $('httpPort'),
  turnHost:    $('turnHost'),
  turnPort:    $('turnPort'),
  systemProxy: $('systemProxy'),
  insecure:    $('insecure'),
  noDtls:      $('noDtls'),
};

const powerBtn   = $('powerBtn');
const powerLabel  = $('powerLabel');
const statusDot   = $('statusDot');
const statusText  = $('statusText');
const connInfo    = $('connInfo');
const infoSocks   = $('infoSocks');
const infoHttp    = $('infoHttp');
const logArea     = $('logArea');
const importModal = $('importModal');
const importInput = $('importInput');

let state = 'disconnected';

// === State ===
function setState(newState, message) {
  state = newState;

  // Button
  powerBtn.className = 'power-btn ' + newState;
  powerBtn.disabled = (newState === 'connecting');

  const labels = {
    disconnected: 'Connect',
    connecting:   'Connecting...',
    connected:    'Connected',
    error:        'Connect',
  };
  powerLabel.textContent = labels[newState] || newState;

  // Status dot + text
  statusDot.className = 'status-dot ' + newState;
  const statusLabels = {
    disconnected: 'Disconnected',
    connecting:   'Establishing tunnel...',
    connected:    message || 'Connected',
    error:        message || 'Error',
  };
  statusText.textContent = statusLabels[newState] || newState;

  // Connection info + metrics
  if (newState === 'connected') {
    connInfo.classList.add('visible');
    const sp = parseInt(el.socksPort.value) || 1080;
    const hp = parseInt(el.httpPort.value) || 8080;
    infoSocks.textContent = '127.0.0.1:' + sp;
    infoHttp.textContent = '127.0.0.1:' + hp;
    startMetricsPolling();
  } else {
    connInfo.classList.remove('visible');
    stopMetricsPolling();
  }

  // Disable inputs when not idle
  const idle = (newState === 'disconnected' || newState === 'error');
  document.querySelectorAll('.input-group').forEach(g => {
    g.classList.toggle('disabled', !idle);
    g.querySelector('input').disabled = !idle;
  });
  document.querySelectorAll('.sm-input, .toggle').forEach(inp => {
    inp.disabled = !idle;
  });
}

function log(msg) {
  const ts = new Date().toLocaleTimeString('en-GB', { hour12: false });
  logArea.value += '[' + ts + '] ' + msg + '\n';
  logArea.scrollTop = logArea.scrollHeight;
}

// === Actions ===
async function connect() {
  const config = {
    vkLink:      el.vkLink.value.trim(),
    peerAddr:    el.peerAddr.value.trim(),
    hyPassword:  el.hyPassword.value,
    sni:         el.sni.value.trim(),
    turnHost:    el.turnHost.value.trim(),
    turnPort:    el.turnPort.value.trim(),
    udp:         el.turnMode.value === 'udp',
    noDtls:      el.noDtls.checked,
    streams:     parseInt(el.streams.value) || 1,
    socksPort:   parseInt(el.socksPort.value) || 1080,
    httpPort:    parseInt(el.httpPort.value) || 8080,
    systemProxy: el.systemProxy.checked,
    insecure:    el.insecure.checked,
  };

  // Basic validation
  if (!config.peerAddr) { log('Enter server address'); return; }
  if (!config.vkLink) { log('Enter VK link'); return; }
  if (!config.hyPassword) { log('Enter password'); return; }

  saveConfig();

  try {
    await window.go.main.App.Connect(config);
  } catch (err) {
    log('Error: ' + err);
  }
}

async function disconnect() {
  try {
    await window.go.main.App.Disconnect();
  } catch (err) {
    log('Error: ' + err);
  }
}

powerBtn.addEventListener('click', () => {
  if (state === 'connected') disconnect();
  else if (state !== 'connecting') connect();
});

// === Config string import ===
// Format: gt://base64({"a":"addr","p":"pass","s":"sni"})
$('importBtn').addEventListener('click', () => {
  importInput.value = '';
  importModal.classList.add('visible');
  importInput.focus();
});

$('importCancel').addEventListener('click', () => {
  importModal.classList.remove('visible');
});

$('importOk').addEventListener('click', () => {
  const raw = importInput.value.trim();
  if (!raw) return;

  try {
    // Strip prefix, whitespace, invisible chars
    let data = raw.replace(/^[\s\uFEFF\u200B]*/, '');
    const gtIdx = data.indexOf('gt://');
    if (gtIdx !== -1) data = data.slice(gtIdx + 5);
    data = data.replace(/[\s\uFEFF\u200B]/g, '');

    const json = JSON.parse(atob(data));
    if (json.a) el.peerAddr.value = json.a;
    if (json.p) {
      el.hyPassword.value = json.p;
      try { sessionStorage.setItem('gt-pw', json.p); } catch (_) {}
    }
    if (json.s) el.sni.value = json.s;
    log('Config imported: server=' + (json.a || '') + ', sni=' + (json.s || ''));
    saveConfig();
  } catch (e) {
    log('Invalid config string: ' + e.message);
  }
  importModal.classList.remove('visible');
});

// Close modal on overlay click
importModal.addEventListener('click', (e) => {
  if (e.target === importModal) importModal.classList.remove('visible');
});

// === Persist config ===
const STORAGE_KEY = 'goodturn-config';

// #6 fix: never persist password to disk
const SENSITIVE_FIELDS = new Set(['hyPassword']);

function saveConfig() {
  const data = {};
  for (const [k, inp] of Object.entries(el)) {
    if (SENSITIVE_FIELDS.has(k)) continue;
    data[k] = inp.type === 'checkbox' ? inp.checked : inp.value;
  }
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(data)); } catch (_) {}
}

function loadConfig() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return;
    const data = JSON.parse(raw);
    for (const [k, inp] of Object.entries(el)) {
      if (k in data) {
        if (inp.type === 'checkbox') inp.checked = data[k];
        else inp.value = data[k];
      }
    }
  } catch (_) {}
}

// Auto-save
for (const inp of Object.values(el)) {
  inp.addEventListener('change', saveConfig);
  inp.addEventListener('input', saveConfig);
}

// === Wails events ===
function initEvents() {
  if (!window.runtime) return;
  window.runtime.EventsOn('state-change', (data) => {
    setState(data.state, data.message);
  });
  window.runtime.EventsOn('log', (msg) => {
    log(msg);
  });
}

// === Metrics polling ===
function formatBytes(b) {
  if (b < 1024) return b + ' B';
  if (b < 1048576) return (b / 1024).toFixed(1) + ' KB';
  if (b < 1073741824) return (b / 1048576).toFixed(1) + ' MB';
  return (b / 1073741824).toFixed(2) + ' GB';
}

function formatDuration(sec) {
  if (sec < 60) return sec + 's';
  if (sec < 3600) return Math.floor(sec / 60) + 'm ' + (sec % 60) + 's';
  return Math.floor(sec / 3600) + 'h ' + Math.floor((sec % 3600) / 60) + 'm';
}

let metricsInterval = null;

function startMetricsPolling() {
  if (metricsInterval) return;
  metricsInterval = setInterval(async () => {
    if (!window.go || !window.go.main || !window.go.main.App) return;
    try {
      const m = await window.go.main.App.GetMetrics();
      const up = document.getElementById('infoUp');
      const down = document.getElementById('infoDown');
      const uptime = document.getElementById('infoUptime');
      if (up) up.textContent = formatBytes(m.bytesUp);
      if (down) down.textContent = formatBytes(m.bytesDown);
      if (uptime) uptime.textContent = formatDuration(m.uptimeSec);
    } catch (_) {}
  }, 1000);
}

function stopMetricsPolling() {
  if (metricsInterval) {
    clearInterval(metricsInterval);
    metricsInterval = null;
  }
}

// === Init ===
loadConfig();
// Restore password from sessionStorage (survives within session, not across restarts)
try {
  const pw = sessionStorage.getItem('gt-pw');
  if (pw) el.hyPassword.value = pw;
} catch (_) {}

// Poll for Wails runtime readiness
const readyCheck = setInterval(() => {
  if (window.go && window.go.main && window.go.main.App) {
    clearInterval(readyCheck);
    initEvents();
    window.go.main.App.GetStatus().then(info => {
      setState(info.state, info.message);
    });
  }
}, 100);
