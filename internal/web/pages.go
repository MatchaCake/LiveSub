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
document.getElementById('langSwitcherSlot').innerHTML = langSwitcher();
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
    <a href="/admin" class="link-btn" id="adminLink" style="display:none" data-i18n="admin">⚙️ 管理</a>
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
document.getElementById('langSwitcherSlot').innerHTML = langSwitcher();

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
  var outputsHTML = (data.outputs || []).map(function(o) {
    return '<div class="output-card">' +
      '<div class="output-name">' + escapeHTML(o.name) + '</div>' +
      '<div class="output-info">' +
        escapeHTML(o.platform) + ' | ' + escapeHTML(o.target_lang || t('source_text')) + ' | ' + t('bot_label') + ' ' + escapeHTML(o.bot_name) +
      '</div>' +
      '<div class="status">' +
        '<span class="badge ' + (o.paused ? 'badge-paused' : 'badge-translating') + '">' + (o.paused ? t('paused') : t('translating')) + '</span>' +
      '</div>' +
      '<div class="output-text">' + escapeHTML(o.last_text || t('waiting_voice')) + '</div>' +
      '<button class="btn ' + (o.paused ? 'btn-resume' : 'btn-pause') + '" onclick="toggle(\'' + escapeHTML(o.name).replace(/'/g,"\\'") + '\')">' +
        (o.paused ? t('resume_btn') : t('pause_btn')) +
      '</button>' +
    '</div>';
  }).join('');

  el.innerHTML = '<div class="streamer-card">' +
    '<div class="streamer-header">' +
      '<span class="streamer-name">' + escapeHTML(data.name || t('room_default')) + '</span>' +
      '<span class="room-id">#' + data.room_id + '</span>' +
    '</div>' +
    '<div class="status">' +
      '<span class="badge ' + (data.live ? 'badge-live' : 'badge-offline') + '">' + (data.live ? t('live') : t('offline')) + '</span>' +
    '</div>' +
    '<div class="outputs">' +
      (outputsHTML || '<div style="color:#666;">' + t('no_outputs') + '</div>') +
    '</div>' +
  '</div>';
}

function escapeHTML(str) {
  if (!str) return '';
  return str.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

async function toggle(outputName) {
  await fetch('/api/toggle?output=' + encodeURIComponent(outputName));
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
  var rows = files.map(function(f) {
    var size = f.size < 1024 ? f.size + ' B' : (f.size/1024).toFixed(1) + ' KB';
    return '<tr style="border-top:1px solid #0f3460;">' +
      '<td style="padding:6px;font-size:13px;">' + escapeHTML(f.name) + '</td>' +
      '<td style="padding:6px;text-align:right;color:#666;font-size:12px;">' + size + '</td>' +
      '<td style="padding:6px;text-align:right;color:#666;font-size:12px;">' + escapeHTML(f.mod_time) + '</td>' +
      '<td style="padding:6px;text-align:right;"><a href="/api/transcripts/download?file=' + encodeURIComponent(f.name) + '" style="color:#4ecca3;text-decoration:none;font-size:13px;">⬇ 下载</a></td>' +
    '</tr>';
  }).join('');
  el.innerHTML = '<table style="width:100%;border-collapse:collapse;">' +
    '<tr style="color:#aaa;font-size:12px;"><th style="text-align:left;padding:6px;">' + t('filename') + '</th><th style="text-align:right;padding:6px;">' + t('size') + '</th><th style="text-align:right;padding:6px;">' + t('time') + '</th><th></th></tr>' +
    rows + '</table>';
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
  .small-btn { padding: 5px 12px; border: 1px solid #555; border-radius: 4px; background: transparent; color: #aaa; cursor: pointer; font-size: 12px; }
  .small-btn:hover { border-color: #e94560; color: #e94560; }
  .small-btn.danger:hover { border-color: #ff4444; color: #ff4444; }
  .form-row { display: flex; gap: 10px; margin-bottom: 10px; align-items: center; flex-wrap: wrap; }
  .form-row input { padding: 8px 12px; border: 1px solid #333; border-radius: 6px; background: #0f3460; color: #eee; font-size: 14px; outline: none; }
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

<div class="section">
  <h2 data-i18n="user_list">👥 用户列表</h2>
  <table>
    <thead><tr><th>用户名</th><th>角色</th><th>B站账号</th><th>操作</th></tr></thead>
    <tbody id="usersBody"></tbody>
  </table>
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
  <button class="add-btn" onclick="addUser()">添加</button>
</div>

<div class="section">
  <h2 data-i18n="bili_accounts">🎮 B站弹幕账号</h2>
  <table>
    <thead><tr><th>名称</th><th>UID</th><th>弹幕上限</th><th>添加时间</th><th>状态</th><th>操作</th></tr></thead>
    <tbody id="biliBody"></tbody>
  </table>
  <div style="margin-top:15px;">
    <button class="add-btn" onclick="startQRLogin()" id="qrBtn">📱 扫码添加账号</button>
  </div>
  <div id="qrArea" style="display:none;margin-top:15px;text-align:center;">
    <div style="font-size:14px;color:#aaa;margin-bottom:10px;" id="qrStatus">请用B站手机APP扫描二维码</div>
    <div id="qrImage" style="background:#fff;display:inline-block;padding:10px;border-radius:8px;"></div>
    <div style="margin-top:10px;"><button class="small-btn" onclick="cancelQR()">取消</button></div>
  </div>
</div>

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
  <table id="auditTable" style="display:none;">
    <thead><tr><th>时间</th><th>用户</th><th>操作</th><th>详情</th><th>IP</th></tr></thead>
    <tbody id="auditBody"></tbody>
  </table>
</div>

<script>
document.getElementById('langSwitcherSlot').innerHTML = langSwitcher();

var allAccounts = [];

function escapeHTML(str) {
  if (!str) return '';
  return str.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

async function init() {
  var acctsRes = await fetch('/api/admin/all-accounts');
  allAccounts = await acctsRes.json() || [];
  renderCheckboxes();
  loadUsers();
  loadBiliAccounts();
}

function renderCheckboxes() {
  document.getElementById('accountCheckboxes').innerHTML = allAccounts.map(function(a) {
    return '<label><input type="checkbox" value="' + escapeHTML(a) + '"> ' + escapeHTML(a) + '</label>';
  }).join('');
}

async function loadUsers() {
  var res = await fetch('/api/admin/users');
  var users = await res.json() || [];
  document.getElementById('usersBody').innerHTML = users.map(function(u) {
    var accts = (u.accounts||[]).map(function(a) {
      return '<span class="tag tag-account">' + escapeHTML(a) + '</span>';
    }).join('');
    var role = u.is_admin ? '<span class="tag tag-admin">管理员</span>' : '普通用户';
    var actions = u.is_admin ? '' :
      '<button class="small-btn" onclick="editUser(' + u.id + ')">编辑</button> ' +
      '<button class="small-btn danger" onclick="deleteUser(' + u.id + ',\'' + escapeHTML(u.username) + '\')">删除</button>';
    return '<tr><td>' + escapeHTML(u.username) + '</td><td>' + role + '</td><td>' + (accts||'无') + '</td><td>' + actions + '</td></tr>';
  }).join('');
}

async function addUser() {
  var username = document.getElementById('newUsername').value.trim();
  var password = document.getElementById('newPassword').value;
  var isAdmin = document.getElementById('newIsAdmin').checked;
  var accounts = Array.from(document.querySelectorAll('#accountCheckboxes input:checked')).map(function(c) { return c.value; });
  var msgEl = document.getElementById('addMsg');
  if (!username || !password) { msgEl.className = 'msg err'; msgEl.textContent = t('fill_required'); return; }
  var res = await fetch('/api/admin/users', {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({username: username, password: password, is_admin: isAdmin, accounts: accounts})
  });
  if (res.ok) {
    msgEl.className = 'msg ok'; msgEl.textContent = t('user_created') + ': ' + username;
    document.getElementById('newUsername').value = '';
    document.getElementById('newPassword').value = '';
    document.getElementById('newIsAdmin').checked = false;
    document.querySelectorAll('#accountCheckboxes input').forEach(function(c) { c.checked = false; });
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
  if (acctStr === null && (newPw === null || newPw === '')) return;
  var body = {};
  if (newPw) body.password = newPw;
  if (acctStr !== null) {
    body.accounts = acctStr.split(',').filter(function(s) { return s.trim(); }).map(function(s) { var idx = parseInt(s.trim())-1; return acctChoices[idx] ? acctChoices[idx].name : null; }).filter(Boolean);
  }
  await fetch('/api/admin/user?id=' + id, { method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body) });
  loadUsers();
}

async function deleteUser(id, name) {
  if (!confirm(t('confirm_del_user') + ' ' + name + '?')) return;
  await fetch('/api/admin/user?id=' + id, {method: 'DELETE'});
  loadUsers();
}

async function loadBiliAccounts() {
  var res = await fetch('/api/admin/bili-accounts');
  var accounts = await res.json() || [];
  document.getElementById('biliBody').innerHTML = accounts.map(function(a) {
    var status = a.valid ? '<span style="color:#4ecca3;">✅ ' + t('valid') + '</span>' : '<span style="color:#e94560;">❌ ' + t('invalid') + '</span>';
    return '<tr>' +
      '<td>' + escapeHTML(a.name) + '</td>' +
      '<td>' + (a.uid || '-') + '</td>' +
      '<td><input type="number" value="' + a.danmaku_max + '" style="width:60px;padding:4px;border:1px solid #333;border-radius:4px;background:#0f3460;color:#eee;font-size:13px;" onchange="updateBiliMax(' + a.id + ',this.value)"></td>' +
      '<td style="font-size:12px;color:#aaa;">' + escapeHTML(a.created_at||'') + '</td>' +
      '<td>' + status + '</td>' +
      '<td><button class="small-btn danger" onclick="deleteBiliAccount(' + a.id + ',\'' + escapeHTML(a.name).replace(/'/g,"\\'") + '\')">删除</button></td>' +
    '</tr>';
  }).join('') || '<tr><td colspan="6" style="text-align:center;color:#666;">' + t('no_bili_accounts') + '</td></tr>';
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
  document.getElementById('qrImage').innerHTML = '<img src="https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=' + encodeURIComponent(data.url) + '" alt="QR" style="width:200px;height:200px;">';
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

async function loadAudit() {
  var limit = document.getElementById('auditLimit').value;
  var res = await fetch('/api/admin/audit?limit=' + limit);
  var entries = await res.json() || [];
  document.getElementById('auditTable').style.display = '';
  document.getElementById('auditBody').innerHTML = entries.map(function(e) {
    return '<tr><td style="white-space:nowrap;font-size:12px;">' + escapeHTML(e.time) + '</td><td>' + escapeHTML(e.username) + '</td><td>' + escapeHTML(e.action) + '</td><td style="font-size:12px;color:#aaa;">' + escapeHTML(e.detail||'') + '</td><td style="font-size:12px;color:#666;">' + escapeHTML(e.ip||'') + '</td></tr>';
  }).join('') || '<tr><td colspan="5" style="text-align:center;color:#666;">' + t('no_log') + '</td></tr>';
}

init();
</script>
</body>
</html>`
