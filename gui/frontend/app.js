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
  yandexLink:  $('yandexLink'),
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

  // Connection info
  if (newState === 'connected') {
    connInfo.classList.add('visible');
    const sp = parseInt(el.socksPort.value) || 1080;
    const hp = parseInt(el.httpPort.value) || 8080;
    infoSocks.textContent = '127.0.0.1:' + sp;
    infoHttp.textContent = '127.0.0.1:' + hp;
  } else {
    connInfo.classList.remove('visible');
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
    yandexLink:  el.yandexLink.value.trim(),
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
  if (!config.vkLink && !config.yandexLink) { log('Enter VK link'); return; }
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
    let data = raw;
    if (data.startsWith('gt://')) data = data.slice(5);

    const json = JSON.parse(atob(data));
    if (json.a) el.peerAddr.value = json.a;
    if (json.p) el.hyPassword.value = json.p;
    if (json.s) el.sni.value = json.s;
    log('Config imported');
    saveConfig();
  } catch (e) {
    log('Invalid config string');
  }
  importModal.classList.remove('visible');
});

// Close modal on overlay click
importModal.addEventListener('click', (e) => {
  if (e.target === importModal) importModal.classList.remove('visible');
});

// === Persist config ===
const STORAGE_KEY = 'goodturn-config';

function saveConfig() {
  const data = {};
  for (const [k, inp] of Object.entries(el)) {
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

// === Init ===
loadConfig();
initEvents();

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
