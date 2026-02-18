package web

const faviconTag = `<link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>🎙️</text></svg>">`

const loginHTML = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>LiveSub</title>
` + faviconTag + `
` + i18nScript + `
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #1a1a2e; color: #eee; min-height: 100vh; display: flex; align-items: center; justify-content: center; }
  .login-box { background: #16213e; border-radius: 16px; padding: 40px; width: 360px; }
  h1 { text-align: center; margin-bottom: 30px; color: #e94560; font-size: 22px; }
  .field { margin-bottom: 20px; }
  label { display: block; margin-bottom: 6px; font-size: 14px; color: #aaa; }
  input { width: 100%; padding: 12px; border: 1px solid #333; border-radius: 8px; background: #0f3460; color: #eee; font-size: 16px; outline: none; }
  input:focus { border-color: #e94560; }
  .btn { width: 100%; padding: 14px; border: none; border-radius: 8px; background: #e94560; color: #fff; font-size: 16px; font-weight: bold; cursor: pointer; }
  .btn:hover { opacity: 0.9; }
  .error { color: #e94560; text-align: center; margin-top: 15px; font-size: 14px; display: none; }
</style>
</head>
<body>
<div class="login-box">
  <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:20px;">
    <h1 style="margin:0;">🎙️ LiveSub</h1>
    <div id="langSwitcherSlot"></div>
  </div>
  <form id="loginForm">
    <div class="field">
      <label data-i18n="username">用户名</label>
      <input type="text" name="username" id="username" autocomplete="username" required>
    </div>
    <div class="field">
      <label data-i18n="password">密码</label>
      <input type="password" name="password" id="password" autocomplete="current-password" required>
    </div>
    <button type="submit" class="btn" data-i18n="login">登录</button>
    <div class="error" id="error"></div>
  </form>
</div>
<script>
document.getElementById('langSwitcherSlot').textContent = '';
document.getElementById('langSwitcherSlot').appendChild(
  document.createRange().createContextualFragment(langSwitcher())
);
setLang(currentLang);
document.getElementById('loginForm').onsubmit = async function(e) {
  e.preventDefault();
  var form = new FormData(e.target);
  var res = await fetch('/api/login', { method: 'POST', body: new URLSearchParams(form) });
  if (res.ok) {
    window.location.href = '/';
  } else {
    var el = document.getElementById('error');
    el.textContent = t('login_error');
    el.style.display = 'block';
  }
};
</script>
</body>
</html>`

const indexHTML = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>LiveSub</title>
` + faviconTag + `
` + i18nScript + `
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #1a1a2e; color: #eee; min-height: 100vh; padding: 20px; }
  .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 30px; flex-wrap: wrap; gap: 10px; }
  h1 { font-size: 24px; color: #e94560; }
  .header-right { display: flex; gap: 10px; align-items: center; }
  .header-right span { font-size: 13px; color: #aaa; }
  .link-btn { padding: 8px 16px; border: 1px solid #555; border-radius: 6px; background: transparent; color: #aaa; cursor: pointer; font-size: 13px; text-decoration: none; }
  .link-btn:hover { border-color: #e94560; color: #e94560; }
  .streamer-card { background: #16213e; border-radius: 12px; padding: 20px; margin-bottom: 20px; }
  .streamer-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
  .streamer-name { font-size: 20px; font-weight: bold; }
  .room-id { font-size: 12px; color: #888; }
  .status { display: flex; gap: 8px; align-items: center; margin-bottom: 16px; }
  .badge { padding: 3px 10px; border-radius: 12px; font-size: 12px; font-weight: bold; }
  .badge-live { background: #e94560; }
  .badge-offline { background: #444; }
  .outputs { display: flex; flex-wrap: wrap; gap: 15px; }
  .output-card { background: #0f3460; border-radius: 8px; padding: 15px; min-width: 250px; flex: 1; }
  .output-name { font-size: 15px; font-weight: bold; margin-bottom: 8px; }
  .output-info { font-size: 12px; color: #aaa; margin-bottom: 8px; }
  .output-text { font-size: 13px; color: #ccc; min-height: 30px; margin-bottom: 10px; word-break: break-all; }
  .badge-translating { background: #16213e; }
  .badge-paused { background: #e9a045; color: #000; }
  .btn { width: 100%; padding: 10px; border: none; border-radius: 6px; font-size: 14px; cursor: pointer; font-weight: bold; transition: all 0.2s; }
  .btn-pause { background: #e94560; color: #fff; }
  .btn-resume { background: #4ecca3; color: #000; }
  .btn:hover { opacity: 0.85; }
  .empty { text-align: center; color: #666; margin-top: 60px; font-size: 16px; }
</style>
</head>
<body>
<div class="header">
  <h1 data-i18n="control_panel">🎙️ LiveSub 控制面板</h1>
  <div class="header-right">
    <div id="langSwitcherSlot"></div>
    <span id="userInfo"></span>
    <a href="/settings" class="link-btn" data-i18n="settings">⚙️ 设置</a>
    <a href="/admin" class="link-btn" id="adminLink" style="display:none" data-i18n="admin">🔧 管理</a>
    <a href="/api/logout" class="link-btn" data-i18n="logout">退出登录</a>
  </div>
</div>
<div id="content"><div class="empty">加载中...</div></div>

<div style="margin-top:30px;background:#16213e;border-radius:12px;padding:20px;">
  <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px;">
    <h2 style="font-size:18px;color:#e94560;margin:0;" data-i18n="transcripts">📄 字幕记录</h2>
    <button class="link-btn" onclick="loadTranscripts()" data-i18n="refresh">刷新</button>
  </div>
  <div id="transcripts" style="font-size:13px;color:#aaa;">点击刷新加载</div>
</div>
<script>
document.getElementById('langSwitcherSlot').textContent = '';
document.getElementById('langSwitcherSlot').appendChild(
  document.createRange().createContextualFragment(langSwitcher())
);

var currentUser = null;

async function init() {
  var res = await fetch('/api/me');
  if (res.status === 401) { window.location.href = '/login'; return; }
  currentUser = await res.json();
  document.getElementById('userInfo').textContent = currentUser.username;
  if (currentUser.is_admin) {
    document.getElementById('adminLink').style.display = '';
  }
  fetchStatus();
  setInterval(fetchStatus, 2000);
}

async function fetchStatus() {
  var res = await fetch('/api/status');
  if (res.status === 401) { window.location.href = '/login'; return; }
  var data = await res.json();
  renderStatus(data);
}

function renderStatus(data) {
  var el = document.getElementById('content');
  var streamers = data.streamers || [];

  if (streamers.length === 0) {
    el.textContent = t('no_streamers');
    return;
  }

  // Build DOM safely
  while (el.firstChild) el.removeChild(el.firstChild);

  streamers.forEach(function(s) {
    var card = document.createElement('div');
    card.className = 'streamer-card';

    var header = document.createElement('div');
    header.className = 'streamer-header';
    var nameEl = document.createElement('span');
    nameEl.className = 'streamer-name';
    nameEl.textContent = s.name || t('room_default');
    var roomEl = document.createElement('span');
    roomEl.className = 'room-id';
    roomEl.textContent = '#' + s.room_id;
    header.appendChild(nameEl);
    header.appendChild(roomEl);
    card.appendChild(header);

    var statusDiv = document.createElement('div');
    statusDiv.className = 'status';
    var badge = document.createElement('span');
    badge.className = 'badge ' + (s.live ? 'badge-live' : 'badge-offline');
    badge.textContent = s.live ? t('live') : t('offline');
    statusDiv.appendChild(badge);
    card.appendChild(statusDiv);

    var outputsDiv = document.createElement('div');
    outputsDiv.className = 'outputs';

    if (!s.outputs || s.outputs.length === 0) {
      var noOut = document.createElement('div');
      noOut.style.color = '#666';
      noOut.textContent = t('no_outputs');
      outputsDiv.appendChild(noOut);
    } else {
      s.outputs.forEach(function(o) {
        var oc = document.createElement('div');
        oc.className = 'output-card';

        var on = document.createElement('div');
        on.className = 'output-name';
        on.textContent = o.name;
        oc.appendChild(on);

        var oi = document.createElement('div');
        oi.className = 'output-info';
        oi.textContent = (o.platform || '') + ' | ' + (o.target_lang || t('source_text')) + ' | ' + t('bot_label') + ' ' + (o.bot_name || '');
        oc.appendChild(oi);

        var os = document.createElement('div');
        os.className = 'status';
        var ob = document.createElement('span');
        ob.className = 'badge ' + (o.paused ? 'badge-paused' : 'badge-translating');
        ob.textContent = o.paused ? t('paused') : t('translating');
        os.appendChild(ob);
        oc.appendChild(os);

        var ot = document.createElement('div');
        ot.className = 'output-text';
        ot.textContent = o.last_text || t('waiting_voice');
        oc.appendChild(ot);

        var btn = document.createElement('button');
        btn.className = 'btn ' + (o.paused ? 'btn-resume' : 'btn-pause');
        btn.textContent = o.paused ? t('resume_btn') : t('pause_btn');
        btn.setAttribute('data-streamer', s.name);
        btn.setAttribute('data-output', o.name);
        btn.onclick = function() { toggle(this.getAttribute('data-streamer'), this.getAttribute('data-output')); };
        oc.appendChild(btn);

        outputsDiv.appendChild(oc);
      });
    }
    card.appendChild(outputsDiv);
    el.appendChild(card);
  });
}

async function toggle(streamerName, outputName) {
  await fetch('/api/toggle?streamer=' + encodeURIComponent(streamerName) + '&output=' + encodeURIComponent(outputName));
  fetchStatus();
}

function onLangChange() { fetchStatus(); }

async function loadTranscripts() {
  var res = await fetch('/api/transcripts');
  var files = await res.json() || [];
  var el = document.getElementById('transcripts');
  if (files.length === 0) {
    el.textContent = t('no_transcripts');
    return;
  }

  while (el.firstChild) el.removeChild(el.firstChild);
  var table = document.createElement('table');
  table.style.cssText = 'width:100%;border-collapse:collapse;';

  var thead = document.createElement('tr');
  thead.style.cssText = 'color:#aaa;font-size:12px;';
  [t('filename'), t('size'), t('time'), ''].forEach(function(h, i) {
    var th = document.createElement('th');
    th.style.cssText = i === 0 ? 'text-align:left;padding:6px;' : 'text-align:right;padding:6px;';
    th.textContent = h;
    thead.appendChild(th);
  });
  table.appendChild(thead);

  files.forEach(function(f) {
    var tr = document.createElement('tr');
    tr.style.borderTop = '1px solid #0f3460';

    var td1 = document.createElement('td');
    td1.style.cssText = 'padding:6px;font-size:13px;';
    td1.textContent = f.name;
    tr.appendChild(td1);

    var td2 = document.createElement('td');
    td2.style.cssText = 'padding:6px;text-align:right;color:#666;font-size:12px;';
    td2.textContent = f.size < 1024 ? f.size + ' B' : (f.size/1024).toFixed(1) + ' KB';
    tr.appendChild(td2);

    var td3 = document.createElement('td');
    td3.style.cssText = 'padding:6px;text-align:right;color:#666;font-size:12px;';
    td3.textContent = f.mod_time;
    tr.appendChild(td3);

    var td4 = document.createElement('td');
    td4.style.cssText = 'padding:6px;text-align:right;';
    var dl = document.createElement('a');
    dl.href = '/api/transcripts/download?file=' + encodeURIComponent(f.name);
    dl.style.cssText = 'color:#4ecca3;text-decoration:none;font-size:13px;';
    dl.textContent = t('download');
    td4.appendChild(dl);
    tr.appendChild(td4);

    table.appendChild(tr);
  });
  el.appendChild(table);
}

init();
</script>
</body>
</html>`

const adminHTML = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>LiveSub</title>
` + faviconTag + `
` + i18nScript + `
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #1a1a2e; color: #eee; min-height: 100vh; padding: 20px; }
  .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 30px; }
  h1 { font-size: 24px; color: #e94560; }
  h2 { font-size: 18px; color: #e94560; margin-bottom: 15px; }
  .link-btn { padding: 8px 16px; border: 1px solid #555; border-radius: 6px; background: transparent; color: #aaa; cursor: pointer; font-size: 13px; text-decoration: none; }
  .link-btn:hover { border-color: #e94560; color: #e94560; }
  .section { background: #16213e; border-radius: 12px; padding: 20px; margin-bottom: 20px; }
  table { width: 100%; border-collapse: collapse; }
  th, td { padding: 10px 12px; text-align: left; border-bottom: 1px solid #0f3460; font-size: 14px; }
  th { color: #aaa; font-weight: normal; font-size: 13px; }
  .tag { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 11px; margin: 2px; }
  .tag-account { background: #3d1e5c; }
  .tag-admin { background: #e94560; }
  .tag-output { background: #0f3460; }
  .small-btn { padding: 5px 12px; border: 1px solid #555; border-radius: 4px; background: transparent; color: #aaa; cursor: pointer; font-size: 12px; }
  .small-btn:hover { border-color: #e94560; color: #e94560; }
  .small-btn.danger:hover { border-color: #ff4444; color: #ff4444; }
  .form-row { display: flex; gap: 10px; margin-bottom: 10px; align-items: center; flex-wrap: wrap; }
  .form-row input, .form-row select { padding: 8px 12px; border: 1px solid #333; border-radius: 6px; background: #0f3460; color: #eee; font-size: 14px; outline: none; }
  .form-row input:focus { border-color: #e94560; }
  .form-row input[type="text"], .form-row input[type="password"] { width: 160px; }
  .add-btn { padding: 8px 20px; border: none; border-radius: 6px; background: #4ecca3; color: #000; cursor: pointer; font-size: 14px; font-weight: bold; }
  .add-btn:hover { opacity: 0.9; }
  .checkbox-group { display: flex; flex-wrap: wrap; gap: 8px; }
  .checkbox-group label { display: flex; align-items: center; gap: 4px; font-size: 13px; cursor: pointer; padding: 4px 8px; border: 1px solid #333; border-radius: 6px; }
  .checkbox-group label:hover { border-color: #e94560; }
  .checkbox-group input[type="checkbox"] { cursor: pointer; }
  .msg { padding: 10px; border-radius: 6px; margin-bottom: 10px; font-size: 13px; display: none; }
  .msg.ok { background: #1a3a2a; color: #4ecca3; display: block; }
  .msg.err { background: #3a1a1a; color: #e94560; display: block; }
</style>
</head>
<body>
<div class="header">
  <div style="display:flex;align-items:center;gap:15px;">
    <h1 data-i18n="user_mgmt">⚙️ 管理面板</h1>
    <div id="langSwitcherSlot"></div>
  </div>
  <a href="/" class="link-btn" data-i18n="back">← 返回控制面板</a>
</div>

<!-- Streamer Management -->
<div class="section">
  <h2 data-i18n="stream_mgmt">📺 主播管理</h2>
  <div id="streamersTable"></div>
  <div style="margin-top:15px;">
    <h3 style="font-size:14px;color:#aaa;margin-bottom:10px;" data-i18n="add_streamer">➕ 添加/编辑主播</h3>
    <div id="streamerMsg" class="msg"></div>
    <div class="form-row">
      <input type="text" id="sName" placeholder="主播名称">
      <input type="number" id="sRoom" placeholder="房间号" style="width:120px;">
      <select id="sLang">
        <option value="ja-JP">日本語 (ja-JP)</option>
        <option value="zh-CN">中文 (zh-CN)</option>
        <option value="en-US">English (en-US)</option>
        <option value="ko-KR">한국어 (ko-KR)</option>
        <option value="fr-FR">Français (fr)</option>
        <option value="de-DE">Deutsch (de)</option>
        <option value="es-ES">Español (es)</option>
        <option value="ru-RU">Русский (ru)</option>
      </select>
      <button class="add-btn" onclick="saveStreamer()">保存</button>
    </div>
  </div>
</div>

<!-- Per-Streamer Output Management -->
<div class="section">
  <h2 data-i18n="output_mgmt">📤 输出管理</h2>
  <div class="form-row" style="margin-bottom:15px;">
    <span style="font-size:14px;color:#aaa;">选择主播:</span>
    <select id="outputStreamerSelect" onchange="loadStreamerOutputs()"></select>
  </div>
  <div id="outputsTable"></div>
  <div style="margin-top:15px;">
    <h3 style="font-size:14px;color:#aaa;margin-bottom:10px;" data-i18n="add_output">➕ 添加/编辑输出</h3>
    <div id="outputMsg" class="msg"></div>
    <div class="form-row">
      <input type="text" id="outName" placeholder="名称">
      <select id="outPlatform">
        <option value="bilibili">bilibili</option>
      </select>
      <select id="outLang">
        <option value="">(原文直传)</option>
        <option value="zh-CN">中文 (zh-CN)</option>
        <option value="en-US">English (en-US)</option>
        <option value="ja-JP">日本語 (ja-JP)</option>
        <option value="ko-KR">한국어 (ko-KR)</option>
        <option value="fr-FR">Français (fr-FR)</option>
        <option value="de-DE">Deutsch (de-DE)</option>
        <option value="es-ES">Español (es-ES)</option>
        <option value="ru-RU">Русский (ru-RU)</option>
      </select>
      <select id="outAccount">
      </select>
    </div>
    <div class="form-row">
      <input type="number" id="outRoom" placeholder="房间号 (0=默认)" style="width:120px;">
      <input type="text" id="outPrefix" placeholder="前缀" value="【" style="width:100px;">
      <input type="text" id="outSuffix" placeholder="后缀" value="】" style="width:100px;">
      <button class="add-btn" onclick="saveOutput()">保存</button>
    </div>
  </div>
</div>

<!-- User Management -->
<div class="section">
  <h2 data-i18n="user_list">👥 用户列表</h2>
  <div id="usersTable"></div>
</div>

<div class="section">
  <h2 data-i18n="add_user">➕ 添加用户</h2>
  <div id="addMsg" class="msg"></div>
  <div class="form-row">
    <input type="text" id="newUsername" placeholder="用户名">
    <input type="password" id="newPassword" placeholder="密码">
    <label style="font-size:13px;cursor:pointer;"><input type="checkbox" id="newIsAdmin"> 管理员</label>
  </div>
  <div style="margin-bottom:10px;">
    <div style="font-size:13px;color:#aaa;margin-bottom:6px;">分配B站账号:</div>
    <div class="checkbox-group" id="accountCheckboxes"></div>
  </div>
  <div style="margin-bottom:10px;">
    <div style="font-size:13px;color:#aaa;margin-bottom:6px;">分配直播间:</div>
    <div class="checkbox-group" id="roomCheckboxes"></div>
  </div>
  <button class="add-btn" onclick="addUser()">添加</button>
</div>

<!-- Bilibili Accounts -->
<div class="section">
  <h2 data-i18n="bili_accounts">🎮 B站弹幕账号</h2>
  <div id="biliTable"></div>
  <div style="margin-top:15px;">
    <button class="add-btn" onclick="startQRLogin()" id="qrBtn">📱 扫码添加账号</button>
  </div>
  <div id="qrArea" style="display:none;margin-top:15px;text-align:center;">
    <div style="font-size:14px;color:#aaa;margin-bottom:10px;" id="qrStatus">请用B站手机APP扫描二维码</div>
    <div id="qrImage" style="background:#fff;display:inline-block;padding:10px;border-radius:8px;"></div>
    <div style="margin-top:10px;"><button class="small-btn" onclick="cancelQR()">取消</button></div>
  </div>
</div>

<!-- Audit Log -->
<div class="section">
  <h2 data-i18n="audit_log">📋 操作记录</h2>
  <div style="margin-bottom:10px;">
    <button class="small-btn" onclick="loadAudit()">加载记录</button>
    <select id="auditLimit" style="padding:5px 8px;border:1px solid #333;border-radius:4px;background:#0f3460;color:#eee;font-size:12px;">
      <option value="50">最近50条</option>
      <option value="100" selected>最近100条</option>
      <option value="500">最近500条</option>
    </select>
  </div>
  <div id="auditTable" style="display:none;"></div>
</div>

<script>
document.getElementById('langSwitcherSlot').textContent = '';
document.getElementById('langSwitcherSlot').appendChild(
  document.createRange().createContextualFragment(langSwitcher())
);

var allAccounts = [];
var allStreamers = [];
var cachedOutputs = [];

function escapeHTML(str) {
  if (!str) return '';
  return str.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

// Build a table using DOM APIs for safety
function buildTable(headers, rows) {
  var table = document.createElement('table');
  var thead = document.createElement('thead');
  var tr = document.createElement('tr');
  headers.forEach(function(h) {
    var th = document.createElement('th');
    th.textContent = h;
    tr.appendChild(th);
  });
  thead.appendChild(tr);
  table.appendChild(thead);
  var tbody = document.createElement('tbody');
  rows.forEach(function(row) {
    var r = document.createElement('tr');
    row.forEach(function(cell) {
      var td = document.createElement('td');
      if (typeof cell === 'string') {
        td.textContent = cell;
      } else if (cell instanceof Node) {
        td.appendChild(cell);
      } else if (cell && cell.html) {
        td.appendChild(document.createRange().createContextualFragment(cell.html));
      }
      r.appendChild(td);
    });
    tbody.appendChild(r);
  });
  table.appendChild(tbody);
  return table;
}

function makeBtn(text, cls, onclick) {
  var b = document.createElement('button');
  b.className = cls;
  b.textContent = text;
  b.onclick = onclick;
  return b;
}

function makeTag(text, cls) {
  var s = document.createElement('span');
  s.className = 'tag ' + cls;
  s.textContent = text;
  return s;
}

function makeFragment(nodes) {
  var f = document.createDocumentFragment();
  nodes.forEach(function(n) { if (n) f.appendChild(n); });
  return f;
}

async function init() {
  var acctsRes = await fetch('/api/admin/all-accounts');
  allAccounts = await acctsRes.json() || [];
  renderCheckboxes();
  loadStreamers();
  loadUsers();
  loadBiliAccounts();
}

function renderCheckboxes() {
  var el = document.getElementById('accountCheckboxes');
  el.textContent = '';
  allAccounts.forEach(function(a) {
    var label = document.createElement('label');
    var cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.value = a;
    label.appendChild(cb);
    label.appendChild(document.createTextNode(' ' + a));
    el.appendChild(label);
  });
}

function renderRoomCheckboxes() {
  var el = document.getElementById('roomCheckboxes');
  el.textContent = '';
  allStreamers.forEach(function(s) {
    var label = document.createElement('label');
    var cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.value = String(s.room_id);
    label.appendChild(cb);
    label.appendChild(document.createTextNode(' ' + s.name + ' (#' + s.room_id + ')'));
    el.appendChild(label);
  });
}

// --- Streamer Management ---

async function loadStreamers() {
  var res = await fetch('/api/admin/streamers');
  allStreamers = await res.json() || [];
  renderStreamersTable();
  renderStreamerSelect();
  renderRoomCheckboxes();
  if (allStreamers.length > 0) {
    loadStreamerOutputs();
  }
}

function renderStreamersTable() {
  var container = document.getElementById('streamersTable');
  container.textContent = '';
  var rows = allStreamers.map(function(s) {
    var outFrag = document.createDocumentFragment();
    (s.outputs||[]).forEach(function(o) {
      outFrag.appendChild(makeTag(o.name + ' (' + (o.target_lang||'原文') + ')', 'tag-output'));
    });
    if (!s.outputs || s.outputs.length === 0) {
      var none = document.createElement('span');
      none.style.color = '#666';
      none.textContent = t('none');
      outFrag.appendChild(none);
    }
    var actions = document.createDocumentFragment();
    actions.appendChild(makeBtn(t('edit'), 'small-btn', function() { editStreamer(s.name); }));
    actions.appendChild(document.createTextNode(' '));
    actions.appendChild(makeBtn(t('delete'), 'small-btn danger', function() { deleteStreamer(s.name); }));
    return [s.name, String(s.room_id), s.source_lang||'ja-JP', outFrag, actions];
  });
  if (rows.length === 0) {
    var p = document.createElement('p');
    p.style.cssText = 'text-align:center;color:#666;padding:15px;';
    p.textContent = t('no_streamers');
    container.appendChild(p);
    return;
  }
  container.appendChild(buildTable([t('name'), t('room_id'), t('source_lang'), t('outputs'), t('actions')], rows));
}

function renderStreamerSelect() {
  var sel = document.getElementById('outputStreamerSelect');
  var prev = sel.value; // remember current selection
  sel.textContent = '';
  allStreamers.forEach(function(s) {
    var opt = document.createElement('option');
    opt.value = s.name;
    opt.textContent = s.name + ' (#' + s.room_id + ')';
    sel.appendChild(opt);
  });
  if (prev) sel.value = prev; // restore selection
}

async function saveStreamer() {
  var name = document.getElementById('sName').value.trim();
  var roomID = parseInt(document.getElementById('sRoom').value) || 0;
  var lang = document.getElementById('sLang').value;
  var msgEl = document.getElementById('streamerMsg');
  if (!name) { msgEl.className = 'msg err'; msgEl.textContent = t('name_required'); return; }
  if (!roomID) { msgEl.className = 'msg err'; msgEl.textContent = t('room_required'); return; }
  var existing = allStreamers.find(function(s) { return s.name === name; });
  var outputs = existing ? existing.outputs : [];
  var res = await fetch('/api/admin/streamers', {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({name: name, room_id: roomID, source_lang: lang, outputs: outputs})
  });
  if (res.ok) {
    msgEl.className = 'msg ok'; msgEl.textContent = t('streamer_saved') + ': ' + name;
    document.getElementById('sName').value = '';
    document.getElementById('sRoom').value = '';
    loadStreamers();
  } else {
    var data = await res.json();
    msgEl.className = 'msg err'; msgEl.textContent = data.error || t('create_failed');
  }
}

function editStreamer(name) {
  var s = allStreamers.find(function(x) { return x.name === name; });
  if (!s) return;
  document.getElementById('sName').value = s.name;
  document.getElementById('sRoom').value = s.room_id;
  document.getElementById('sLang').value = s.source_lang || 'ja-JP';
  document.getElementById('sName').scrollIntoView({behavior: 'smooth'});
}

async function deleteStreamer(name) {
  if (!confirm(t('confirm_del_streamer') + ' ' + name + '?')) return;
  await fetch('/api/admin/streamers?name=' + encodeURIComponent(name), {method: 'DELETE'});
  loadStreamers();
}

// --- Per-Streamer Output Management ---

async function loadStreamerOutputs() {
  var sel = document.getElementById('outputStreamerSelect');
  var streamerName = sel.value;
  var container = document.getElementById('outputsTable');
  if (!streamerName) {
    container.textContent = t('select_streamer');
    return;
  }
  var res = await fetch('/api/admin/streamer-outputs?streamer=' + encodeURIComponent(streamerName));
  var outputs = await res.json() || [];
  cachedOutputs = outputs;
  container.textContent = '';

  var rows = outputs.map(function(o) {
    var actions = document.createDocumentFragment();
    actions.appendChild(makeBtn(t('edit'), 'small-btn', function() { editOutput(o.name); }));
    actions.appendChild(document.createTextNode(' '));
    actions.appendChild(makeBtn(t('delete'), 'small-btn danger', function() { deleteOutput(o.name); }));
    return [o.name, o.platform||'bilibili', o.target_lang||'(原文)', o.account||'', String(o.room_id||0), o.prefix||'', o.suffix||'', actions];
  });
  if (rows.length === 0) {
    var p = document.createElement('p');
    p.style.cssText = 'text-align:center;color:#666;padding:15px;';
    p.textContent = t('no_streamer_outputs');
    container.appendChild(p);
    return;
  }
  container.appendChild(buildTable([t('name'), t('platform'), t('target_lang'), t('account'), t('room_id'), t('prefix'), t('suffix'), t('actions')], rows));

  // Populate account dropdown
  var acctSel = document.getElementById('outAccount');
  acctSel.textContent = '';
  var defOpt = document.createElement('option');
  defOpt.value = '';
  defOpt.textContent = '(' + t('select_account') + ')';
  acctSel.appendChild(defOpt);
  allAccounts.forEach(function(a) {
    var opt = document.createElement('option');
    opt.value = a;
    opt.textContent = a;
    acctSel.appendChild(opt);
  });
}

async function saveOutput() {
  var streamerName = document.getElementById('outputStreamerSelect').value;
  if (!streamerName) { alert(t('select_streamer')); return; }
  var name = document.getElementById('outName').value.trim();
  var msgEl = document.getElementById('outputMsg');
  if (!name) { msgEl.className = 'msg err'; msgEl.textContent = t('name_required'); return; }
  var body = {
    name: name,
    platform: document.getElementById('outPlatform').value,
    target_lang: document.getElementById('outLang').value.trim(),
    account: document.getElementById('outAccount').value,
    room_id: parseInt(document.getElementById('outRoom').value) || 0,
    prefix: document.getElementById('outPrefix').value,
    suffix: document.getElementById('outSuffix').value
  };
  var res = await fetch('/api/admin/streamer-outputs?streamer=' + encodeURIComponent(streamerName), {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(body)
  });
  if (res.ok) {
    msgEl.className = 'msg ok'; msgEl.textContent = t('output_saved') + ': ' + name;
    clearOutputForm();
    loadStreamerOutputs();
    loadStreamers();
  } else {
    var data = await res.json();
    msgEl.className = 'msg err'; msgEl.textContent = data.error || t('create_failed');
  }
}

function editOutput(name) {
  var o = cachedOutputs.find(function(x) { return x.name === name; });
  if (!o) return;
  document.getElementById('outName').value = o.name;
  document.getElementById('outPlatform').value = o.platform || 'bilibili';
  document.getElementById('outLang').value = o.target_lang || '';
  var sel = document.getElementById('outAccount');
  sel.value = o.account || '';
  if (sel.value !== (o.account || '') && o.account) {
    var opt = document.createElement('option');
    opt.value = o.account;
    opt.textContent = o.account;
    sel.appendChild(opt);
    sel.value = o.account;
  }
  document.getElementById('outRoom').value = o.room_id || 0;
  document.getElementById('outPrefix').value = o.prefix || '';
  document.getElementById('outSuffix').value = o.suffix || '';
  document.getElementById('outName').scrollIntoView({behavior: 'smooth'});
}

async function deleteOutput(name) {
  var streamerName = document.getElementById('outputStreamerSelect').value;
  if (!confirm(t('confirm_del_output') + ' ' + name + '?')) return;
  await fetch('/api/admin/streamer-outputs?streamer=' + encodeURIComponent(streamerName) + '&name=' + encodeURIComponent(name), {method: 'DELETE'});
  loadStreamerOutputs();
  loadStreamers();
}

function clearOutputForm() {
  document.getElementById('outName').value = '';
  document.getElementById('outLang').selectedIndex = 0;
  document.getElementById('outAccount').selectedIndex = 0;
  document.getElementById('outRoom').value = '';
  document.getElementById('outPrefix').value = '【';
  document.getElementById('outSuffix').value = '】';
}

// --- User Management ---

async function loadUsers() {
  var res = await fetch('/api/admin/users');
  var users = await res.json() || [];
  var container = document.getElementById('usersTable');
  container.textContent = '';

  var rows = users.map(function(u) {
    var acctFrag = document.createDocumentFragment();
    (u.accounts||[]).forEach(function(a) { acctFrag.appendChild(makeTag(a, 'tag-account')); });
    if (!u.accounts || u.accounts.length === 0) acctFrag.appendChild(document.createTextNode(t('none')));

    var roomFrag = document.createDocumentFragment();
    (u.rooms||[]).forEach(function(r) {
      var s = allStreamers.find(function(x) { return x.room_id === r; });
      var label = s ? s.name + ' (#' + r + ')' : '#' + r;
      roomFrag.appendChild(makeTag(label, 'tag-output'));
    });
    if (!u.rooms || u.rooms.length === 0) roomFrag.appendChild(document.createTextNode(t('none')));

    var roleEl = u.is_admin ? makeTag(t('role_admin'), 'tag-admin') : document.createTextNode(t('role_user'));

    var actions = document.createDocumentFragment();
    if (!u.is_admin) {
      actions.appendChild(makeBtn(t('edit'), 'small-btn', function() { editUser(u.id); }));
      actions.appendChild(document.createTextNode(' '));
      actions.appendChild(makeBtn(t('delete'), 'small-btn danger', function() { deleteUser(u.id, u.username); }));
    }
    return [u.username, roleEl, acctFrag, roomFrag, actions];
  });
  container.appendChild(buildTable([t('username'), t('role'), t('accounts'), t('rooms'), t('actions')], rows));
}

async function addUser() {
  var username = document.getElementById('newUsername').value.trim();
  var password = document.getElementById('newPassword').value;
  var isAdmin = document.getElementById('newIsAdmin').checked;
  var accounts = Array.from(document.querySelectorAll('#accountCheckboxes input:checked')).map(function(c) { return c.value; });
  var rooms = Array.from(document.querySelectorAll('#roomCheckboxes input:checked')).map(function(c) { return parseInt(c.value); });
  var msgEl = document.getElementById('addMsg');
  if (!username || !password) { msgEl.className = 'msg err'; msgEl.textContent = t('fill_required'); return; }
  var res = await fetch('/api/admin/users', {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({username: username, password: password, is_admin: isAdmin, accounts: accounts, rooms: rooms})
  });
  if (res.ok) {
    msgEl.className = 'msg ok'; msgEl.textContent = t('user_created') + ': ' + username;
    document.getElementById('newUsername').value = '';
    document.getElementById('newPassword').value = '';
    document.getElementById('newIsAdmin').checked = false;
    document.querySelectorAll('#accountCheckboxes input').forEach(function(c) { c.checked = false; });
    document.querySelectorAll('#roomCheckboxes input').forEach(function(c) { c.checked = false; });
    loadUsers();
  } else {
    var data = await res.json();
    msgEl.className = 'msg err'; msgEl.textContent = data.error || t('create_failed');
  }
}

async function editUser(id) {
  var res = await fetch('/api/admin/users');
  var users = await res.json();
  var u = users.find(function(x) { return x.id === id; });
  if (!u) return;
  var newPw = prompt(t('new_password'));
  var acctChoices = allAccounts.map(function(a) { return {name: a, checked: (u.accounts||[]).indexOf(a) !== -1}; });
  var acctStr = prompt(
    t('assign_accounts_prompt') + '\n' + acctChoices.map(function(a,i) { return (i+1) + '. ' + a.name + (a.checked?' ✓':''); }).join('\n'),
    acctChoices.filter(function(a) { return a.checked; }).map(function(_,i) { return i+1; }).join(',')
  );
  var roomChoices = allStreamers.map(function(s) { return {room_id: s.room_id, name: s.name, checked: (u.rooms||[]).indexOf(s.room_id) !== -1}; });
  var roomStr = prompt(
    t('assign_rooms_prompt') + '\n' + roomChoices.map(function(r,i) { return (i+1) + '. ' + r.name + ' (#' + r.room_id + ')' + (r.checked?' ✓':''); }).join('\n'),
    roomChoices.filter(function(r) { return r.checked; }).map(function(_,i) { return i+1; }).join(',')
  );
  if (acctStr === null && roomStr === null && (newPw === null || newPw === '')) return;
  var body = {};
  if (newPw) body.password = newPw;
  if (acctStr !== null) {
    body.accounts = acctStr.split(',').filter(function(s) { return s.trim(); }).map(function(s) { var idx = parseInt(s.trim())-1; return acctChoices[idx] ? acctChoices[idx].name : null; }).filter(Boolean);
  }
  if (roomStr !== null) {
    body.rooms = roomStr.split(',').filter(function(s) { return s.trim(); }).map(function(s) { var idx = parseInt(s.trim())-1; return roomChoices[idx] ? roomChoices[idx].room_id : null; }).filter(Boolean);
  }
  await fetch('/api/admin/user?id=' + id, { method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body) });
  loadUsers();
}

async function deleteUser(id, name) {
  if (!confirm(t('confirm_del_user') + ' ' + name + '?')) return;
  await fetch('/api/admin/user?id=' + id, {method: 'DELETE'});
  loadUsers();
}

// --- Bilibili Accounts ---

async function loadBiliAccounts() {
  var res = await fetch('/api/admin/bili-accounts');
  var accounts = await res.json() || [];
  var container = document.getElementById('biliTable');
  container.textContent = '';

  var rows = accounts.map(function(a) {
    var statusEl = document.createElement('span');
    statusEl.style.color = a.valid ? '#4ecca3' : '#e94560';
    statusEl.textContent = a.valid ? t('valid') : t('invalid');

    var maxInput = document.createElement('input');
    maxInput.type = 'number';
    maxInput.value = a.danmaku_max;
    maxInput.style.cssText = 'width:60px;padding:4px;border:1px solid #333;border-radius:4px;background:#0f3460;color:#eee;font-size:13px;';
    maxInput.onchange = function() { updateBiliMax(a.id, this.value); };

    var actions = makeBtn(t('delete'), 'small-btn danger', function() { deleteBiliAccount(a.id, a.name); });

    var timeEl = document.createElement('span');
    timeEl.style.cssText = 'font-size:12px;color:#aaa;';
    timeEl.textContent = a.created_at || '';

    return [a.name, String(a.uid || '-'), maxInput, timeEl, statusEl, actions];
  });
  if (rows.length === 0) {
    var p = document.createElement('p');
    p.style.cssText = 'text-align:center;color:#666;padding:15px;';
    p.textContent = t('no_bili_accounts');
    container.appendChild(p);
    return;
  }
  container.appendChild(buildTable([t('name'), t('uid'), t('danmaku_max'), t('created_at'), t('status'), t('actions')], rows));
}

async function updateBiliMax(id, val) {
  await fetch('/api/admin/bili-account?id=' + id, {
    method: 'PUT', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({danmaku_max: parseInt(val)})
  });
}

async function deleteBiliAccount(id, name) {
  if (!confirm(t('confirm_del_account') + ' ' + name + '?')) return;
  await fetch('/api/admin/bili-account?id=' + id, {method: 'DELETE'});
  loadBiliAccounts();
}

var qrPollTimer = null;

async function startQRLogin() {
  var res = await fetch('/api/admin/bili-qr/generate');
  var data = await res.json();
  if (!data.url) { alert('生成二维码失败'); return; }
  document.getElementById('qrArea').style.display = '';
  document.getElementById('qrBtn').style.display = 'none';
  document.getElementById('qrStatus').textContent = t('qr_scan');
  var img = document.createElement('img');
  img.src = 'https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=' + encodeURIComponent(data.url);
  img.alt = 'QR';
  img.style.cssText = 'width:200px;height:200px;';
  var qrImg = document.getElementById('qrImage');
  qrImg.textContent = '';
  qrImg.appendChild(img);
  qrPollTimer = setInterval(async function() {
    var pollRes = await fetch('/api/admin/bili-qr/poll?key=' + data.qrcode_key);
    var pollData = await pollRes.json();
    if (pollData.status === 'scanned') document.getElementById('qrStatus').textContent = t('qr_scanned');
    else if (pollData.status === 'confirmed') { cancelQR(); alert(t('qr_success') + ' ' + pollData.name); loadBiliAccounts(); }
    else if (pollData.status === 'expired') { cancelQR(); alert(t('qr_expired')); }
  }, 2000);
}

function cancelQR() {
  if (qrPollTimer) { clearInterval(qrPollTimer); qrPollTimer = null; }
  document.getElementById('qrArea').style.display = 'none';
  document.getElementById('qrBtn').style.display = '';
}

// --- Audit Log ---

async function loadAudit() {
  var limit = document.getElementById('auditLimit').value;
  var res = await fetch('/api/admin/audit?limit=' + limit);
  var entries = await res.json() || [];
  var container = document.getElementById('auditTable');
  container.style.display = '';
  container.textContent = '';
  if (entries.length === 0) {
    var p = document.createElement('p');
    p.style.cssText = 'text-align:center;color:#666;padding:15px;';
    p.textContent = t('no_log');
    container.appendChild(p);
    return;
  }
  var rows = entries.map(function(e) {
    return [e.time, e.username, e.action, e.detail||'', e.ip||''];
  });
  container.appendChild(buildTable([t('log_time'), t('log_user'), t('log_action'), t('log_detail'), t('log_ip')], rows));
}

init();
</script>
</body>
</html>`

const settingsHTML = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>LiveSub - ` + "设置" + `</title>
` + faviconTag + `
` + i18nScript + `
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #1a1a2e; color: #eee; min-height: 100vh; padding: 20px; }
  .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 30px; flex-wrap: wrap; gap: 10px; }
  h1 { font-size: 24px; color: #e94560; }
  h2 { font-size: 18px; color: #e94560; margin-bottom: 15px; }
  .link-btn { padding: 8px 16px; border: 1px solid #555; border-radius: 6px; background: transparent; color: #aaa; cursor: pointer; font-size: 13px; text-decoration: none; }
  .link-btn:hover { border-color: #e94560; color: #e94560; }
  .section { background: #16213e; border-radius: 12px; padding: 20px; margin-bottom: 20px; }
  table { width: 100%; border-collapse: collapse; }
  th, td { padding: 10px 12px; text-align: left; border-bottom: 1px solid #0f3460; font-size: 14px; }
  th { color: #aaa; font-weight: normal; font-size: 13px; }
  .small-btn { padding: 5px 12px; border: 1px solid #555; border-radius: 4px; background: transparent; color: #aaa; cursor: pointer; font-size: 12px; }
  .small-btn:hover { border-color: #e94560; color: #e94560; }
  .small-btn.danger:hover { border-color: #ff4444; color: #ff4444; }
  .form-row { display: flex; gap: 10px; margin-bottom: 10px; align-items: center; flex-wrap: wrap; }
  .form-row input, .form-row select { padding: 8px 12px; border: 1px solid #333; border-radius: 6px; background: #0f3460; color: #eee; font-size: 14px; outline: none; }
  .form-row input:focus { border-color: #e94560; }
  .add-btn { padding: 8px 20px; border: none; border-radius: 6px; background: #4ecca3; color: #000; cursor: pointer; font-size: 14px; font-weight: bold; }
  .add-btn:hover { opacity: 0.9; }
  .msg { padding: 10px; border-radius: 6px; margin-bottom: 10px; font-size: 13px; display: none; }
  .msg.ok { background: #1a3a2a; color: #4ecca3; display: block; }
  .msg.err { background: #3a1a1a; color: #e94560; display: block; }
  .streamer-tab { display: inline-block; padding: 8px 16px; border: 1px solid #333; border-radius: 8px 8px 0 0; cursor: pointer; font-size: 14px; color: #aaa; background: transparent; margin-right: 4px; }
  .streamer-tab.active { background: #16213e; color: #e94560; border-color: #e94560; border-bottom-color: #16213e; }
</style>
</head>
<body>
<div class="header">
  <div style="display:flex;align-items:center;gap:15px;">
    <h1 data-i18n="settings">⚙️ 设置</h1>
    <div id="langSwitcherSlot"></div>
  </div>
  <a href="/" class="link-btn" data-i18n="back">← 返回控制面板</a>
</div>

<div class="section">
  <h2 data-i18n="manage_outputs">📤 输出管理</h2>
  <div id="streamerTabs" style="margin-bottom:-1px;"></div>
  <div id="outputsContent" style="border-top:1px solid #0f3460;padding-top:15px;"></div>
  <div style="margin-top:15px;">
    <h3 style="font-size:14px;color:#aaa;margin-bottom:10px;" data-i18n="add_output">➕ 添加/编辑输出</h3>
    <div id="outputMsg" class="msg"></div>
    <div class="form-row">
      <input type="text" id="outName" placeholder="名称" style="width:120px;">
      <select id="outPlatform"><option value="bilibili">bilibili</option></select>
      <select id="outLang">
        <option value="">(原文直传)</option>
        <option value="zh-CN">中文 (zh-CN)</option>
        <option value="en-US">English (en-US)</option>
        <option value="ja-JP">日本語 (ja-JP)</option>
        <option value="ko-KR">한국어 (ko-KR)</option>
        <option value="fr-FR">Français (fr-FR)</option>
        <option value="de-DE">Deutsch (de-DE)</option>
        <option value="es-ES">Español (es-ES)</option>
        <option value="ru-RU">Русский (ru-RU)</option>
      </select>
      <select id="outAccount"></select>
    </div>
    <div class="form-row">
      <input type="number" id="outRoom" placeholder="房间号 (0=默认)" style="width:120px;">
      <input type="text" id="outPrefix" placeholder="前缀" value="【" style="width:100px;">
      <input type="text" id="outSuffix" placeholder="后缀" value="】" style="width:100px;">
      <button class="add-btn" onclick="saveOutput()">保存</button>
    </div>
  </div>
</div>

<script>
document.getElementById('langSwitcherSlot').innerHTML = langSwitcher();

var myStreamers = [];
var myAccounts = [];
var cachedOutputs = [];
var currentStreamer = '';

function escapeHTML(str) {
  if (!str) return '';
  return str.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

async function init() {
  var meRes = await fetch('/api/me');
  if (meRes.status === 401) { window.location.href = '/login'; return; }
  var acctRes = await fetch('/api/my/accounts');
  myAccounts = await acctRes.json() || [];
  var statusRes = await fetch('/api/status');
  var status = await statusRes.json();
  myStreamers = status.streamers || [];
  renderTabs();
  renderAccountDropdown();
  if (myStreamers.length > 0) {
    currentStreamer = myStreamers[0].name;
    selectTab(currentStreamer);
  }
}

function renderTabs() {
  var container = document.getElementById('streamerTabs');
  container.innerHTML = '';
  myStreamers.forEach(function(s) {
    var tab = document.createElement('span');
    tab.className = 'streamer-tab' + (s.name === currentStreamer ? ' active' : '');
    tab.textContent = s.name + ' (#' + s.room_id + ')';
    tab.setAttribute('data-name', s.name);
    tab.onclick = function() { selectTab(this.getAttribute('data-name')); };
    container.appendChild(tab);
  });
}

function selectTab(name) {
  currentStreamer = name;
  document.querySelectorAll('.streamer-tab').forEach(function(t) {
    t.className = 'streamer-tab' + (t.getAttribute('data-name') === name ? ' active' : '');
  });
  loadOutputs();
}

function renderAccountDropdown() {
  var sel = document.getElementById('outAccount');
  sel.innerHTML = '<option value="">(' + t('select_account') + ')</option>';
  myAccounts.forEach(function(a) {
    var opt = document.createElement('option');
    opt.value = a;
    opt.textContent = a;
    sel.appendChild(opt);
  });
}

async function loadOutputs() {
  if (!currentStreamer) return;
  var res = await fetch('/api/my/streamer-outputs?streamer=' + encodeURIComponent(currentStreamer));
  var outputs = await res.json() || [];
  cachedOutputs = outputs;
  var container = document.getElementById('outputsContent');
  container.innerHTML = '';
  if (outputs.length === 0) {
    container.innerHTML = '<p style="text-align:center;color:#666;padding:15px;">' + t('no_streamer_outputs') + '</p>';
    return;
  }
  var table = document.createElement('table');
  var thead = document.createElement('thead');
  var hr = document.createElement('tr');
  [t('name'), t('platform'), t('target_lang'), t('account'), t('room_id'), t('prefix'), t('suffix'), t('actions')].forEach(function(h) {
    var th = document.createElement('th'); th.textContent = h; hr.appendChild(th);
  });
  thead.appendChild(hr); table.appendChild(thead);
  var tbody = document.createElement('tbody');
  outputs.forEach(function(o) {
    var tr = document.createElement('tr');
    [o.name, o.platform||'bilibili', o.target_lang||'(原文)', o.account||'', String(o.room_id||0), o.prefix||'', o.suffix||''].forEach(function(v) {
      var td = document.createElement('td'); td.textContent = v; tr.appendChild(td);
    });
    var actionTd = document.createElement('td');
    var editBtn = document.createElement('button');
    editBtn.className = 'small-btn'; editBtn.textContent = t('edit');
    editBtn.setAttribute('data-name', o.name);
    editBtn.onclick = function() { editOutput(this.getAttribute('data-name')); };
    actionTd.appendChild(editBtn);
    actionTd.appendChild(document.createTextNode(' '));
    var delBtn = document.createElement('button');
    delBtn.className = 'small-btn danger'; delBtn.textContent = t('delete');
    delBtn.setAttribute('data-name', o.name);
    delBtn.onclick = function() { deleteOutput(this.getAttribute('data-name')); };
    actionTd.appendChild(delBtn);
    tr.appendChild(actionTd);
    tbody.appendChild(tr);
  });
  table.appendChild(tbody); container.appendChild(table);
}

async function saveOutput() {
  if (!currentStreamer) { alert(t('select_streamer')); return; }
  var name = document.getElementById('outName').value.trim();
  var msgEl = document.getElementById('outputMsg');
  if (!name) { msgEl.className = 'msg err'; msgEl.textContent = t('name_required'); return; }
  var body = {
    name: name, platform: document.getElementById('outPlatform').value,
    target_lang: document.getElementById('outLang').value,
    account: document.getElementById('outAccount').value,
    room_id: parseInt(document.getElementById('outRoom').value) || 0,
    prefix: document.getElementById('outPrefix').value,
    suffix: document.getElementById('outSuffix').value
  };
  var res = await fetch('/api/my/streamer-outputs?streamer=' + encodeURIComponent(currentStreamer), {
    method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)
  });
  if (res.ok) {
    msgEl.className = 'msg ok'; msgEl.textContent = t('output_saved') + ': ' + name;
    clearForm(); loadOutputs();
  } else {
    var data = await res.json();
    msgEl.className = 'msg err'; msgEl.textContent = data.error || t('create_failed');
  }
}

function editOutput(name) {
  var o = cachedOutputs.find(function(x) { return x.name === name; });
  if (!o) return;
  document.getElementById('outName').value = o.name;
  document.getElementById('outPlatform').value = o.platform || 'bilibili';
  document.getElementById('outLang').value = o.target_lang || '';
  var sel = document.getElementById('outAccount');
  sel.value = o.account || '';
  if (sel.value !== (o.account || '') && o.account) {
    var opt = document.createElement('option'); opt.value = o.account; opt.textContent = o.account;
    sel.appendChild(opt); sel.value = o.account;
  }
  document.getElementById('outRoom').value = o.room_id || 0;
  document.getElementById('outPrefix').value = o.prefix || '';
  document.getElementById('outSuffix').value = o.suffix || '';
  document.getElementById('outName').scrollIntoView({behavior: 'smooth'});
}

async function deleteOutput(name) {
  if (!confirm(t('confirm_del_output') + ' ' + name + '?')) return;
  await fetch('/api/my/streamer-outputs?streamer=' + encodeURIComponent(currentStreamer) + '&name=' + encodeURIComponent(name), {method: 'DELETE'});
  loadOutputs();
}

function clearForm() {
  document.getElementById('outName').value = '';
  document.getElementById('outLang').selectedIndex = 0;
  document.getElementById('outAccount').selectedIndex = 0;
  document.getElementById('outRoom').value = '';
  document.getElementById('outPrefix').value = '【';
  document.getElementById('outSuffix').value = '】';
}

init();
</script>
</body>
</html>`
