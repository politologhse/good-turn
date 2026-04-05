// === DOM ===
const $ = id => document.getElementById(id);

const el = {
  vkLink: $('vkLink'),
  peerAddr: $('peerAddr'),
  hyPassword: $('hyPassword'),
  sni: $('sni'),
  turnMode: $('turnMode'),
  streams: $('streams'),
  socksPort: $('socksPort'),
  httpPort: $('httpPort'),
  turnHost: $('turnHost'),
  turnPort: $('turnPort'),
  systemProxy: $('systemProxy'),
  insecure: $('insecure'),
  noDtls: $('noDtls'),
};

const powerBtn = $('powerBtn');
const powerLabel = $('powerLabel');
const reactorText = $('reactorText');
const reactorMood = $('reactorMood');
const statusBar = $('statusBar');
const statusDot = $('statusDot');
const statusText = $('statusText');
const connInfo = $('connInfo');
const infoSocks = $('infoSocks');
const infoHttp = $('infoHttp');
const infoUp = $('infoUp');
const infoDown = $('infoDown');
const infoUptime = $('infoUptime');
const infoProfile = $('infoProfile');
const logArea = $('logArea');
const importBtn = $('importBtn');
const importModal = $('importModal');
const importInput = $('importInput');
const importCancel = $('importCancel');
const importOk = $('importOk');

const REQUIRED_FIELDS = ['vkLink', 'peerAddr', 'hyPassword'];
const STORAGE_KEY = 'goodturn-config';
const SENSITIVE_FIELDS = new Set(['hyPassword']);

let state = 'disconnected';
let metricsInterval = null;

function hasRequiredInputs() {
  return REQUIRED_FIELDS.every(key => String(el[key].value || '').trim());
}

function resetMetricsDisplay() {
  infoUp.textContent = '0 B';
  infoDown.textContent = '0 B';
  infoUptime.textContent = '0s';
}

function refreshStaticInfo() {
  const sp = parseInt(el.socksPort.value, 10) || 1080;
  const hp = parseInt(el.httpPort.value, 10) || 8080;
  const streams = Math.max(parseInt(el.streams.value, 10) || 1, 1);
  const mode = (el.turnMode.value || 'tcp').toUpperCase();

  infoSocks.textContent = '127.0.0.1:' + sp;
  infoHttp.textContent = '127.0.0.1:' + hp;
  infoProfile.textContent = mode + ' · x' + streams;
}

function applyFieldState() {
  const idle = (state === 'disconnected' || state === 'error');
  const ready = hasRequiredInputs();

  document.querySelectorAll('.field-card').forEach(card => {
    card.classList.toggle('is-disabled', !idle);
  });

  REQUIRED_FIELDS.forEach(key => {
    const card = el[key].closest('.field-card');
    if (!card) return;
    card.classList.toggle('needs-input', idle && !ready && !String(el[key].value || '').trim());
  });

  document.querySelectorAll('.input-group').forEach(group => {
    const input = group.querySelector('input');
    group.classList.toggle('disabled', !!input && input.disabled);
  });
}

function updateActionAvailability() {
  const idle = (state === 'disconnected' || state === 'error');
  const ready = hasRequiredInputs();

  powerBtn.classList.toggle('armed', idle && ready);
  powerBtn.disabled = state === 'connecting' || (idle && !ready);
}

function getUiCopy(newState, message) {
  const ready = hasRequiredInputs();

  if (newState === 'connecting') {
    return {
      button: 'Warping goo',
      status: 'Brewing portal',
      reactor: 'The relay is wobbling into shape through VK cover traffic. Give the goo a second.',
      mood: 'portal mood: fizzy',
    };
  }

  if (newState === 'connected') {
    return {
      button: 'Portal stable',
      status: message || 'Portal open',
      reactor: 'The tunnel is alive, drooling neon and ready to forward traffic through the glowing ports below.',
      mood: 'portal mood: spicy',
    };
  }

  if (newState === 'error') {
    return {
      button: 'Kick again',
      status: message || 'Portal burped',
      reactor: message || 'The portal sneezed itself shut. Check the coordinates, key, or transport knobs and poke it again.',
      mood: 'portal mood: grumpy',
    };
  }

  if (ready) {
    return {
      button: 'Kick the portal',
      status: 'Portal primed',
      reactor: 'Everything is loaded. Hit the reactor and let the tunnel ooze into place.',
      mood: 'portal mood: hungry',
    };
  }

  return {
    button: 'Prime portal',
    status: 'Need coordinates',
    reactor: 'Feed the machine a live VK call, relay address and secret key so the portal slime knows where to go.',
    mood: 'portal mood: sleepy',
  };
}

// === State ===
function setState(newState, message) {
  state = newState;
  const copy = getUiCopy(newState, message);

  powerBtn.className = 'power-btn ' + newState;
  statusBar.className = 'status-bar ' + newState;
  statusDot.className = 'status-dot ' + newState;

  powerLabel.textContent = copy.button;
  statusText.textContent = copy.status;
  reactorText.textContent = copy.reactor;
  if (reactorMood) reactorMood.textContent = copy.mood;

  if (newState === 'connected') {
    connInfo.classList.add('visible');
    refreshStaticInfo();
    startMetricsPolling();
  } else {
    connInfo.classList.remove('visible');
    stopMetricsPolling();
    resetMetricsDisplay();
    refreshStaticInfo();
  }

  const idle = (newState === 'disconnected' || newState === 'error');
  document.querySelectorAll('.input-group input').forEach(input => {
    input.disabled = !idle;
  });
  document.querySelectorAll('.sm-input, .toggle').forEach(input => {
    input.disabled = !idle;
  });

  applyFieldState();
  updateActionAvailability();
}

function refreshIdleUi() {
  refreshStaticInfo();
  applyFieldState();
  updateActionAvailability();

  if (state === 'disconnected' || state === 'error') {
    const copy = getUiCopy(state);
    powerLabel.textContent = copy.button;
    statusText.textContent = copy.status;
    reactorText.textContent = copy.reactor;
    if (reactorMood) reactorMood.textContent = copy.mood;
  }
}

function log(msg) {
  const ts = new Date().toLocaleTimeString('en-GB', { hour12: false });
  logArea.value += '[' + ts + '] ' + msg + '\n';
  logArea.scrollTop = logArea.scrollHeight;
}

// === Actions ===
async function connect() {
  const config = {
    vkLink: el.vkLink.value.trim(),
    peerAddr: el.peerAddr.value.trim(),
    hyPassword: el.hyPassword.value,
    sni: el.sni.value.trim(),
    turnHost: el.turnHost.value.trim(),
    turnPort: el.turnPort.value.trim(),
    udp: el.turnMode.value === 'udp',
    noDtls: el.noDtls.checked,
    streams: parseInt(el.streams.value, 10) || 1,
    socksPort: parseInt(el.socksPort.value, 10) || 1080,
    httpPort: parseInt(el.httpPort.value, 10) || 8080,
    systemProxy: el.systemProxy.checked,
    insecure: el.insecure.checked,
  };

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
  else if (state !== 'connecting' && hasRequiredInputs()) connect();
});

// === Config string import ===
// Format: gt://base64({"a":"addr","p":"pass","s":"sni"})
importBtn.addEventListener('click', () => {
  importInput.value = '';
  importModal.classList.add('visible');
  importInput.focus();
});

importCancel.addEventListener('click', () => {
  importModal.classList.remove('visible');
});

importOk.addEventListener('click', () => {
  const raw = importInput.value.trim();
  if (!raw) return;

  try {
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
    refreshIdleUi();
  } catch (e) {
    log('Invalid config string: ' + e.message);
  }

  importModal.classList.remove('visible');
});

importModal.addEventListener('click', event => {
  if (event.target === importModal) importModal.classList.remove('visible');
});

// === Persist config ===
function saveConfig() {
  const data = {};
  for (const [key, input] of Object.entries(el)) {
    if (SENSITIVE_FIELDS.has(key)) continue;
    data[key] = input.type === 'checkbox' ? input.checked : input.value;
  }
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(data)); } catch (_) {}
}

function loadConfig() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return;

    const data = JSON.parse(raw);
    for (const [key, input] of Object.entries(el)) {
      if (!(key in data)) continue;
      if (input.type === 'checkbox') input.checked = data[key];
      else input.value = data[key];
    }
  } catch (_) {}
}

for (const input of Object.values(el)) {
  const handler = () => {
    saveConfig();
    refreshIdleUi();
  };
  input.addEventListener('change', handler);
  input.addEventListener('input', handler);
}

// === Wails events ===
function initEvents() {
  if (!window.runtime) return;

  window.runtime.EventsOn('state-change', data => {
    setState(data.state, data.message);
  });

  window.runtime.EventsOn('log', msg => {
    log(msg);
  });
}

// === Metrics polling ===
function formatBytes(bytes) {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
  if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB';
  return (bytes / 1073741824).toFixed(2) + ' GB';
}

function formatDuration(sec) {
  if (sec < 60) return sec + 's';
  if (sec < 3600) return Math.floor(sec / 60) + 'm ' + (sec % 60) + 's';
  return Math.floor(sec / 3600) + 'h ' + Math.floor((sec % 3600) / 60) + 'm';
}

function startMetricsPolling() {
  if (metricsInterval) return;

  metricsInterval = setInterval(async () => {
    if (!window.go || !window.go.main || !window.go.main.App) return;

    try {
      const metrics = await window.go.main.App.GetMetrics();
      infoUp.textContent = formatBytes(metrics.bytesUp);
      infoDown.textContent = formatBytes(metrics.bytesDown);
      infoUptime.textContent = formatDuration(metrics.uptimeSec);
    } catch (_) {}
  }, 1000);
}

function stopMetricsPolling() {
  if (!metricsInterval) return;
  clearInterval(metricsInterval);
  metricsInterval = null;
}

// === Init ===
loadConfig();
try {
  const pw = sessionStorage.getItem('gt-pw');
  if (pw) el.hyPassword.value = pw;
} catch (_) {}

refreshStaticInfo();
resetMetricsDisplay();
setState('disconnected');

const readyCheck = setInterval(() => {
  if (!window.go || !window.go.main || !window.go.main.App) return;

  clearInterval(readyCheck);
  initEvents();
  window.go.main.App.GetStatus().then(info => {
    setState(info.state, info.message);
  });
}, 100);
