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
const profileCard = $('profileCard');
const profileBadge = $('profileBadge');
const profileState = $('profileState');
const profileMeta = $('profileMeta');
const profileImportBtn = $('profileImportBtn');
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
const doctorBtn = $('doctorBtn');
const doctorModal = $('doctorModal');
const doctorStatus = $('doctorStatus');
const doctorResults = $('doctorResults');
const doctorClose = $('doctorClose');
const doctorCopy = $('doctorCopy');

const REQUIRED_FIELDS = ['vkLink'];
const STORAGE_KEY = 'goodturn-config';
const SENSITIVE_FIELDS = new Set(['hyPassword']);

let state = 'disconnected';
let metricsInterval = null;

function hasRequiredInputs() {
  return REQUIRED_FIELDS.every(key => String(el[key].value || '').trim()) && hasProfileReady();
}

function hasProfileReady() {
  return Boolean(String(el.peerAddr.value || '').trim() && String(el.hyPassword.value || '').trim());
}

function resetMetricsDisplay() {
  infoUp.textContent = '0 B';
  infoDown.textContent = '0 B';
  infoUptime.textContent = '0s';
}

function refreshStaticInfo() {
  const sp = parseInt(el.socksPort.value, 10) || 1080;
  const hp = parseInt(el.httpPort.value, 10) || 8080;
  const streams = Math.max(parseInt(el.streams.value, 10) || 2, 1);
  const mode = (el.turnMode.value || 'tcp').toUpperCase();

  infoSocks.textContent = '127.0.0.1:' + sp;
  infoHttp.textContent = '127.0.0.1:' + hp;
  infoProfile.textContent = mode + ' · x' + streams;
}

function refreshProfileInfo() {
  const addr = String(el.peerAddr.value || '').trim();
  const sni = String(el.sni.value || '').trim();
  const ready = hasProfileReady();

  if (!profileBadge || !profileState || !profileMeta) return;

  profileBadge.textContent = ready ? 'Profile loaded' : 'Profile missing';
  profileBadge.classList.toggle('is-ready', ready);
  profileBadge.classList.toggle('is-missing', !ready);

  if (ready) {
    profileState.textContent = addr;
    profileMeta.textContent = 'SNI: ' + (sni || 'auto') + ' · password loaded from profile';
  } else {
    profileState.textContent = 'Import the gt:// profile from your server.';
    profileMeta.textContent = 'Relay address and password will be filled automatically.';
  }
}

function applyFieldState() {
  const idle = (state === 'disconnected' || state === 'error');
  const ready = hasRequiredInputs();
  const profileReady = hasProfileReady();

  document.querySelectorAll('.field-card').forEach(card => {
    card.classList.toggle('is-disabled', !idle);
  });

  REQUIRED_FIELDS.forEach(key => {
    const card = el[key].closest('.field-card');
    if (!card) return;
    card.classList.toggle('needs-input', idle && !ready && !String(el[key].value || '').trim());
  });

  if (profileCard) {
    profileCard.classList.toggle('needs-input', idle && !profileReady);
  }

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
      button: 'Connecting',
      status: 'Connecting',
      reactor: 'Starting relay, Hysteria2 and local proxy.',
      mood: 'connecting',
    };
  }

  if (newState === 'connected') {
    return {
      button: 'Disconnect',
      status: message || 'Connected',
      reactor: 'Traffic is routed through the local SOCKS5 and HTTP ports.',
      mood: 'connected',
    };
  }

  if (newState === 'error') {
    return {
      button: 'Retry',
      status: message || 'Error',
      reactor: message || 'Check the profile, VK link and diagnostics report.',
      mood: 'error',
    };
  }

  if (ready) {
    return {
      button: 'Connect',
      status: 'Ready',
      reactor: 'Profile and VK link are ready. Balanced mode uses 2 streams by default.',
      mood: 'ready',
    };
  }

  return {
    button: 'Connect',
    status: 'Setup needed',
    reactor: 'Import a profile and paste a live VK call link.',
    mood: 'idle',
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
  refreshProfileInfo();
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
    streams: parseInt(el.streams.value, 10) || 2,
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

if (profileImportBtn) {
  profileImportBtn.addEventListener('click', () => {
    importBtn.click();
  });
}

importCancel.addEventListener('click', () => {
  importModal.classList.remove('visible');
});

importOk.addEventListener('click', async () => {
  const raw = importInput.value.trim();
  if (!raw) return;
  importModal.classList.remove('visible');

  try {
    // Parse via Go backend (shared validation with server/CLI)
    const p = await window.go.main.App.ImportProfile(raw);
    if (p.addr) el.peerAddr.value = p.addr;
    if (p.password) {
      el.hyPassword.value = p.password;
      try { sessionStorage.setItem('gt-pw', p.password); } catch (_) {}
    }
    if (p.sni) el.sni.value = p.sni;
    // Stash raw profile string so Doctor can use it without re-encoding
    try { sessionStorage.setItem('gt-profile-raw', raw); } catch (_) {}
    log('Profile imported: server=' + (p.addr || '') + ', sni=' + (p.sni || ''));
    saveConfig();
    refreshIdleUi();
  } catch (e) {
    log('Invalid profile: ' + e);
  }
});

importModal.addEventListener('click', event => {
  if (event.target === importModal) importModal.classList.remove('visible');
});

// === Connection Doctor ===
function statusLabel(status) {
  if (status === 'pass') return 'Pass';
  if (status === 'warn') return 'Warn';
  return 'Fail';
}

function escapeHtml(value) {
  return String(value || '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#039;');
}

let lastDoctorReport = null;

doctorBtn.addEventListener('click', async () => {
  doctorResults.innerHTML = '';
  doctorStatus.textContent = 'Running diagnostics...';
  doctorModal.classList.add('visible');

  const profileStr = sessionStorage.getItem('gt-profile-raw') || '';
  const vkLink = el.vkLink.value.trim();
  const socksPort = parseInt(el.socksPort.value, 10) || 1080;
  const noDtls = el.noDtls.checked;

  try {
    const report = await window.go.main.App.RunDoctor(profileStr, vkLink, socksPort, noDtls);
    lastDoctorReport = report;

    doctorStatus.textContent = 'Platform: ' + (report.platform || 'unknown');
    const html = (report.checks || []).map(c => {
      const status = escapeHtml(c.status);
      const hint = c.hint ? '<div class="doctor-hint">' + escapeHtml(c.hint) + '</div>' : '';
      return '<div class="doctor-row doctor-row--' + status + '">' +
        '<div class="doctor-row-head"><span class="doctor-icon"></span><strong>' + escapeHtml(c.name) + '</strong><span>' + statusLabel(c.status) + '</span></div>' +
        '<div class="doctor-detail">' + escapeHtml(c.detail) + '</div>' +
        hint +
        '</div>';
    }).join('');
    doctorResults.innerHTML = html;
  } catch (e) {
    doctorStatus.textContent = 'Doctor failed: ' + e;
  }
});

doctorClose.addEventListener('click', () => doctorModal.classList.remove('visible'));
doctorModal.addEventListener('click', event => {
  if (event.target === doctorModal) doctorModal.classList.remove('visible');
});

doctorCopy.addEventListener('click', async () => {
  if (!lastDoctorReport) return;
  // Copy sanitized version (server addresses redacted)
  try {
    const sanitized = await window.go.main.App.RunDoctorSanitized(lastDoctorReport);
    const text = JSON.stringify(sanitized, null, 2);
    await navigator.clipboard.writeText(text);
    doctorStatus.textContent = 'Sanitized report copied to clipboard';
  } catch (e) {
    doctorStatus.textContent = 'Could not copy sanitized report';
  }
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

  window.runtime.EventsOn('captcha-manual', url => {
    log('Manual verification needed — opening browser...');
    try { window.open(url, '_blank'); } catch (_) {}
    // Show URL in log so user can copy if browser didn't open
    log('If browser did not open, go to: ' + url);
    log('Complete the verification, then click Connect again.');
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

refreshProfileInfo();
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
