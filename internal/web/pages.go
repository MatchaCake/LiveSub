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
    <script>document.write(langSwitcher())</script>
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
<script>setLang(currentLang);</script>
<script>
document.getElementById('loginForm').onsubmit = async (e) => {
  e.preventDefault();
  const form = new FormData(e.target);
  const res = await fetch('/api/login', { method: 'POST', body: new URLSearchParams(form) });
  if (res.ok) {
    window.location.href = '/';
  } else {
    const el = document.getElementById('error');
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
  .rooms { display: flex; flex-wrap: wrap; gap: 20px; justify-content: center; }
  .room { background: #16213e; border-radius: 12px; padding: 20px; min-width: 300px; max-width: 400px; flex: 1; }
  .room-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
  .room-name { font-size: 18px; font-weight: bold; }
  .room-id { font-size: 12px; color: #888; }
  .status { display: flex; gap: 8px; align-items: center; margin-bottom: 12px; }
  .badge { padding: 3px 10px; border-radius: 12px; font-size: 12px; font-weight: bold; }
  .badge-live { background: #e94560; }
  .badge-offline { background: #444; }
  .badge-translating { background: #0f3460; }
  .badge-paused { background: #e9a045; color: #000; }
  .last-text { font-size: 13px; color: #aaa; min-height: 40px; margin-bottom: 15px; word-break: break-all; }
  .account-row { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
  .account-row label { font-size: 13px; color: #aaa; white-space: nowrap; }
  .account-select { flex: 1; padding: 6px 10px; border: 1px solid #333; border-radius: 6px; background: #0f3460; color: #eee; font-size: 13px; outline: none; cursor: pointer; }
  .account-select:focus { border-color: #e94560; }
  .btn { width: 100%; padding: 12px; border: none; border-radius: 8px; font-size: 16px; cursor: pointer; font-weight: bold; transition: all 0.2s; }
  .btn-pause { background: #e94560; color: #fff; }
  .btn-resume { background: #4ecca3; color: #000; }
  .btn:hover { opacity: 0.85; transform: scale(1.02); }
  .btn:active { transform: scale(0.98); }
  .empty { text-align: center; color: #666; margin-top: 60px; font-size: 16px; }
</style>
</head>
<body>
<div class="header">
  <h1 data-i18n="control_panel">🎙️ LiveSub 控制面板</h1>
  <div class="header-right">
    <script>document.write(langSwitcher())</script>
    <span id="userInfo"></span>
    <a href="/admin" class="link-btn" id="adminLink" style="display:none" data-i18n="admin">⚙️ 管理</a>
    <a href="/api/logout" class="link-btn" data-i18n="logout">退出登录</a>
  </div>
</div>
<div class="rooms" id="rooms"><div class="empty">加载中...</div></div>

<div style="margin-top:30px;background:#16213e;border-radius:12px;padding:20px;">
  <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px;">
    <h2 style="font-size:18px;color:#e94560;margin:0;" data-i18n="transcripts">📄 字幕记录</h2>
    <button class="link-btn" onclick="loadTranscripts()" data-i18n="refresh">刷新</button>
  </div>
  <div id="transcripts" style="font-size:13px;color:#aaa;" data-i18n="refresh">点击刷新加载</div>
</div>
<script>
let currentUser = null;

async function init() {
  const res = await fetch('/api/me');
  if (res.status === 401) { window.location.href = '/login'; return; }
  currentUser = await res.json();
  document.getElementById('userInfo').textContent = currentUser.username;
  if (currentUser.is_admin) {
    document.getElementById('adminLink').style.display = '';
  }
  fetchRooms();
  setInterval(fetchRooms, 2000);
}

async function fetchRooms() {
  const res = await fetch('/api/rooms');
  if (res.status === 401) { window.location.href = '/login'; return; }
  const rooms = await res.json();
  const el = document.getElementById('rooms');
  if (!rooms || rooms.length === 0) {
    el.innerHTML = '<div class="empty">' + t('no_rooms') + '</div>';
    return;
  }
  el.innerHTML = rooms.map(r => ` + "`" + `
    <div class="room">
      <div class="room-header">
        <span class="room-name">${r.name || t('room_default')}</span>
        <span class="room-id">#${r.room_id}</span>
      </div>
      <div class="status">
        <span class="badge ${r.live ? 'badge-live' : 'badge-offline'}">${r.live ? t('live') : t('offline')}</span>
        <span class="badge ${r.paused ? 'badge-paused' : 'badge-translating'}">${r.paused ? t('paused') : t('translating')}</span>
      </div>
      <div class="last-text">${r.stt_text || t('waiting_voice')}</div>
      ${r.accounts && r.accounts.length > 0 ? ` + "`" + `
      <div class="account-row">
        <label>${t('account_label')}</label>
        <select class="account-select" onchange="switchAccount(${r.room_id}, this.value)">
          ${r.accounts.map((a, i) => ` + "`" + `<option value="${i}" ${i === r.current_account ? 'selected' : ''}>${a}</option>` + "`" + `).join('')}
        </select>
      </div>
      ` + "`" + ` : ''}
      <button class="btn ${r.paused ? 'btn-resume' : 'btn-pause'}" onclick="toggle(${r.room_id})">
        ${r.paused ? t('resume_btn') : t('pause_btn')}
      </button>
    </div>
  ` + "`" + `).join('');
}

async function toggle(roomId) {
  await fetch('/api/toggle?room=' + roomId);
  fetchRooms();
}

async function switchAccount(roomId, index) {
  await fetch('/api/account?room=' + roomId + '&index=' + index);
  fetchRooms();
}

function onLangChange() { fetchRooms(); }

async function loadTranscripts() {
  const res = await fetch('/api/transcripts');
  const files = await res.json() || [];
  const el = document.getElementById('transcripts');
  if (files.length === 0) {
    el.innerHTML = '<span style="color:#666;">' + t('no_transcripts') + '</span>';
    return;
  }
  el.innerHTML = '<table style="width:100%;border-collapse:collapse;">' +
    '<tr style="color:#aaa;font-size:12px;"><th style="text-align:left;padding:6px;">' + t('filename') + '</th><th style="text-align:right;padding:6px;">' + t('size') + '</th><th style="text-align:right;padding:6px;">' + t('time') + '</th><th></th></tr>' +
    files.map(f => {
      const size = f.size < 1024 ? f.size + ' B' : (f.size/1024).toFixed(1) + ' KB';
      return '<tr style="border-top:1px solid #0f3460;">' +
        '<td style="padding:6px;font-size:13px;">' + f.name + '</td>' +
        '<td style="padding:6px;text-align:right;color:#666;font-size:12px;">' + size + '</td>' +
        '<td style="padding:6px;text-align:right;color:#666;font-size:12px;">' + f.mod_time + '</td>' +
        '<td style="padding:6px;text-align:right;"><a href="/api/transcripts/download?file=' + encodeURIComponent(f.name) + '" style="color:#4ecca3;text-decoration:none;font-size:13px;">⬇ 下载</a></td>' +
      '</tr>';
    }).join('') + '</table>';
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
  .tag-room { background: #0f3460; }
  .tag-account { background: #3d1e5c; }
  .tag-admin { background: #e94560; }
  .small-btn { padding: 5px 12px; border: 1px solid #555; border-radius: 4px; background: transparent; color: #aaa; cursor: pointer; font-size: 12px; }
  .small-btn:hover { border-color: #e94560; color: #e94560; }
  .small-btn.danger:hover { border-color: #ff4444; color: #ff4444; }
  .form-row { display: flex; gap: 10px; margin-bottom: 10px; align-items: center; flex-wrap: wrap; }
  .form-row input, .form-row select { padding: 8px 12px; border: 1px solid #333; border-radius: 6px; background: #0f3460; color: #eee; font-size: 14px; outline: none; }
  .form-row input:focus, .form-row select:focus { border-color: #e94560; }
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
    <h1 data-i18n="user_mgmt">⚙️ 用户管理</h1>
    <script>document.write(langSwitcher())</script>
  </div>
  <a href="/" class="link-btn" data-i18n="back">← 返回控制面板</a>
</div>

<div class="section">
  <h2 data-i18n="stream_mgmt">📺 直播间管理</h2>
  <table>
    <thead><tr><th>名称</th><th>房间号</th><th>语言</th><th>来源</th><th>操作</th></tr></thead>
    <tbody id="streamsBody"></tbody>
  </table>
  <div style="margin-top:15px;">
    <div id="streamMsg" class="msg"></div>
    <div class="form-row">
      <input type="text" id="newStreamName" placeholder="名称 (如: 向日葵)">
      <input type="text" id="newStreamURL" placeholder="直播间URL或房间号">
      <select id="newStreamLang" style="padding:8px 12px;border:1px solid #333;border-radius:6px;background:#0f3460;color:#eee;font-size:14px;">
        <option value="ja-JP">日语 (ja-JP)</option>
        <option value="en-US">英语 (en-US)</option>
        <option value="ko-KR">韩语 (ko-KR)</option>
        <option value="zh-CN">中文 (zh-CN)</option>
      </select>
      <button class="add-btn" onclick="addStream()">添加直播间</button>
    </div>
  </div>
</div>

<div class="section">
  <h2 data-i18n="user_list">👥 用户列表</h2>
  <table id="usersTable">
    <thead><tr><th>用户名</th><th>角色</th><th>直播间</th><th>B站账号</th><th>操作</th></tr></thead>
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
    <div style="font-size:13px;color:#aaa;margin-bottom:6px;">分配直播间:</div>
    <div class="checkbox-group" id="roomCheckboxes"></div>
  </div>
  <div style="margin-bottom:10px;">
    <div style="font-size:13px;color:#aaa;margin-bottom:6px;">分配B站账号:</div>
    <div class="checkbox-group" id="accountCheckboxes"></div>
  </div>
  <button class="add-btn" onclick="addUser()">添加</button>
</div>

<div class="section">
  <h2 data-i18n="bili_accounts">🎮 B站弹幕账号</h2>
  <table id="biliTable">
    <thead><tr><th>名称</th><th>UID</th><th>弹幕上限</th><th>添加时间</th><th>状态</th><th>操作</th></tr></thead>
    <tbody id="biliBody"></tbody>
  </table>
  <div style="margin-top:15px;">
    <button class="add-btn" onclick="startQRLogin()" id="qrBtn">📱 扫码添加账号</button>
  </div>
  <div id="qrArea" style="display:none;margin-top:15px;text-align:center;">
    <div style="font-size:14px;color:#aaa;margin-bottom:10px;" id="qrStatus">请用B站手机APP扫描二维码</div>
    <div id="qrImage" style="background:#fff;display:inline-block;padding:10px;border-radius:8px;"></div>
    <div style="margin-top:10px;">
      <button class="small-btn" onclick="cancelQR()">取消</button>
    </div>
  </div>
</div>

<div class="section">
  <h2 data-i18n="audit_log">📋 操作记录</h2>
  <div style="margin-bottom:10px;">
    <button class="small-btn" onclick="loadAudit()" id="auditBtn">加载记录</button>
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
let allRooms = [];
let allAccounts = [];

async function init() {
  const [roomsRes, acctsRes] = await Promise.all([
    fetch('/api/admin/all-rooms'),
    fetch('/api/admin/all-accounts')
  ]);
  allRooms = await roomsRes.json() || [];
  allAccounts = await acctsRes.json() || [];
  renderCheckboxes();
  loadUsers();
  loadBiliAccounts();
  loadStreams();
}

function renderCheckboxes() {
  document.getElementById('roomCheckboxes').innerHTML = allRooms.map(r =>
    '<label><input type="checkbox" value="' + r.room_id + '"> ' + (r.name || r.room_id) + '</label>'
  ).join('');
  document.getElementById('accountCheckboxes').innerHTML = allAccounts.map(a =>
    '<label><input type="checkbox" value="' + a + '"> ' + a + '</label>'
  ).join('');
}

async function loadUsers() {
  const res = await fetch('/api/admin/users');
  const users = await res.json() || [];
  const body = document.getElementById('usersBody');
  body.innerHTML = users.map(u => {
    const rooms = (u.rooms||[]).map(r => {
      const info = allRooms.find(x => x.room_id === r);
      return '<span class="tag tag-room">' + (info ? info.name : r) + '</span>';
    }).join('');
    const accts = (u.accounts||[]).map(a =>
      '<span class="tag tag-account">' + a + '</span>'
    ).join('');
    const role = u.is_admin ? '<span class="tag tag-admin">管理员</span>' : '普通用户';
    const actions = u.is_admin ? '' :
      '<button class="small-btn" onclick="editUser(' + u.id + ')">编辑</button> ' +
      '<button class="small-btn danger" onclick="deleteUser(' + u.id + ',\'' + u.username + '\')">删除</button>';
    return '<tr><td>' + u.username + '</td><td>' + role + '</td><td>' + (rooms||'无') + '</td><td>' + (accts||'无') + '</td><td>' + actions + '</td></tr>';
  }).join('');
}

async function addUser() {
  const username = document.getElementById('newUsername').value.trim();
  const password = document.getElementById('newPassword').value;
  const isAdmin = document.getElementById('newIsAdmin').checked;
  const rooms = [...document.querySelectorAll('#roomCheckboxes input:checked')].map(c => parseInt(c.value));
  const accounts = [...document.querySelectorAll('#accountCheckboxes input:checked')].map(c => c.value);

  const msgEl = document.getElementById('addMsg');
  if (!username || !password) {
    msgEl.className = 'msg err'; msgEl.textContent = '请填写用户名和密码'; return;
  }

  const res = await fetch('/api/admin/users', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({username, password, is_admin: isAdmin, rooms, accounts})
  });

  if (res.ok) {
    msgEl.className = 'msg ok'; msgEl.textContent = '用户 ' + username + ' 已创建';
    document.getElementById('newUsername').value = '';
    document.getElementById('newPassword').value = '';
    document.getElementById('newIsAdmin').checked = false;
    document.querySelectorAll('#roomCheckboxes input, #accountCheckboxes input').forEach(c => c.checked = false);
    loadUsers();
  } else {
    const data = await res.json();
    msgEl.className = 'msg err'; msgEl.textContent = data.error || '创建失败';
  }
}

async function editUser(id) {
  const res = await fetch('/api/admin/users');
  const users = await res.json();
  const u = users.find(x => x.id === id);
  if (!u) return;

  const newPw = prompt('新密码 (留空不改):');

  // Room selection
  const roomChoices = allRooms.map(r => ({id: r.room_id, name: r.name || String(r.room_id), checked: (u.rooms||[]).includes(r.room_id)}));
  const acctChoices = allAccounts.map(a => ({name: a, checked: (u.accounts||[]).includes(a)}));

  // Simple prompt-based editing (checkbox dialogs not practical in prompt)
  const roomStr = prompt(
    '分配直播间 (输入序号，逗号分隔):\\n' + roomChoices.map((r,i) => (i+1) + '. ' + r.name + (r.checked?' ✓':'')).join('\\n'),
    roomChoices.filter(r=>r.checked).map((_,i)=>i+1).join(',')
  );
  const acctStr = prompt(
    '分配B站账号 (输入序号，逗号分隔):\\n' + acctChoices.map((a,i) => (i+1) + '. ' + a.name + (a.checked?' ✓':'')).join('\\n'),
    acctChoices.filter(a=>a.checked).map((_,i)=>i+1).join(',')
  );

  if (roomStr === null && acctStr === null && (newPw === null || newPw === '')) return;

  const body = {};
  if (newPw) body.password = newPw;
  if (roomStr !== null) {
    body.rooms = roomStr.split(',').filter(s=>s.trim()).map(s => roomChoices[parseInt(s.trim())-1]?.id).filter(Boolean);
  }
  if (acctStr !== null) {
    body.accounts = acctStr.split(',').filter(s=>s.trim()).map(s => acctChoices[parseInt(s.trim())-1]?.name).filter(Boolean);
  }

  await fetch('/api/admin/user?id=' + id, {
    method: 'PUT',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(body)
  });
  loadUsers();
}

async function deleteUser(id, name) {
  if (!confirm('确定删除用户 ' + name + '?')) return;
  await fetch('/api/admin/user?id=' + id, {method: 'DELETE'});
  loadUsers();
}

// --- Streams ---

async function loadStreams() {
  const res = await fetch('/api/admin/streams');
  const streams = await res.json() || [];
  const body = document.getElementById('streamsBody');
  body.innerHTML = streams.map(s => {
    const src = s.source === 'config' ? '<span class="tag" style="background:#444;">配置文件</span>' : '<span class="tag" style="background:#0f3460;">WebUI</span>';
    const delParam = s.id ? 'id=' + s.id : 'room=' + s.room_id;
    return '<tr>' +
    '<td>' + s.name + '</td>' +
    '<td><a href="https://live.bilibili.com/' + s.room_id + '" target="_blank" style="color:#4ecca3;">' + s.room_id + '</a></td>' +
    '<td style="font-size:12px;color:#aaa;">' + (s.source_lang||'-') + '</td>' +
    '<td>' + src + '</td>' +
    '<td><button class="small-btn danger" onclick="deleteStream(\'' + delParam + '\',\'' + s.name.replace(/'/g,"\\'") + '\')">删除</button></td>' +
    '</tr>';
  }).join('') || '<tr><td colspan="5" style="text-align:center;color:#666;">暂无直播间</td></tr>';
}

function extractRoomID(input) {
  input = input.trim();
  // Pure number
  if (/^\d+$/.test(input)) return parseInt(input);
  // URL like https://live.bilibili.com/22959964?...
  const m = input.match(/live\.bilibili\.com\/(\d+)/);
  return m ? parseInt(m[1]) : 0;
}

async function addStream() {
  const name = document.getElementById('newStreamName').value.trim();
  const urlInput = document.getElementById('newStreamURL').value.trim();
  const lang = document.getElementById('newStreamLang').value;
  const msgEl = document.getElementById('streamMsg');

  if (!name) { msgEl.className='msg err'; msgEl.textContent='请填写名称'; return; }

  const roomID = extractRoomID(urlInput);
  if (!roomID) { msgEl.className='msg err'; msgEl.textContent='无法识别房间号，请输入URL或数字'; return; }

  const res = await fetch('/api/admin/stream', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({name, room_id: roomID, source_lang: lang})
  });

  if (res.ok) {
    msgEl.className='msg ok'; msgEl.textContent='已添加: ' + name + ' (#' + roomID + ')';
    document.getElementById('newStreamName').value = '';
    document.getElementById('newStreamURL').value = '';
    loadStreams();
    // Also refresh room checkboxes for user assignment
    const [roomsRes] = await Promise.all([fetch('/api/admin/all-rooms')]);
    allRooms = await roomsRes.json() || [];
    renderCheckboxes();
  } else {
    const data = await res.json();
    msgEl.className='msg err'; msgEl.textContent=data.error||'添加失败';
  }
}

async function deleteStream(param, name) {
  if (!confirm('确定删除直播间 ' + name + '?\\n正在进行的翻译会停止')) return;
  await fetch('/api/admin/stream?' + param, {method: 'DELETE'});
  loadStreams();
}

// --- Bilibili accounts ---

async function loadBiliAccounts() {
  const res = await fetch('/api/admin/bili-accounts');
  const accounts = await res.json() || [];
  const body = document.getElementById('biliBody');
  body.innerHTML = accounts.map(a => {
    const status = a.valid ? '<span style="color:#4ecca3;">✅ 有效</span>' : '<span style="color:#e94560;">❌ 已失效</span>';
    return '<tr>' +
      '<td>' + a.name + '</td>' +
      '<td>' + (a.uid || '-') + '</td>' +
      '<td><input type="number" value="' + a.danmaku_max + '" style="width:60px;padding:4px;border:1px solid #333;border-radius:4px;background:#0f3460;color:#eee;font-size:13px;" onchange="updateBiliMax(' + a.id + ',this.value)"></td>' +
      '<td style="font-size:12px;color:#aaa;">' + (a.created_at||'') + '</td>' +
      '<td>' + status + '</td>' +
      '<td><button class="small-btn danger" onclick="deleteBiliAccount(' + a.id + ',\'' + a.name.replace(/'/g,"\\'") + '\')">删除</button></td>' +
    '</tr>';
  }).join('') || '<tr><td colspan="6" style="text-align:center;color:#666;">暂无账号，点击下方扫码添加</td></tr>';
}

async function updateBiliMax(id, val) {
  await fetch('/api/admin/bili-account?id=' + id, {
    method: 'PUT',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({danmaku_max: parseInt(val)})
  });
}

async function deleteBiliAccount(id, name) {
  if (!confirm('确定删除B站账号 ' + name + '?')) return;
  await fetch('/api/admin/bili-account?id=' + id, {method: 'DELETE'});
  loadBiliAccounts();
}

let qrPollTimer = null;

async function startQRLogin() {
  const res = await fetch('/api/admin/bili-qr/generate');
  const data = await res.json();
  if (!data.url) { alert('生成二维码失败'); return; }

  document.getElementById('qrArea').style.display = '';
  document.getElementById('qrBtn').style.display = 'none';
  document.getElementById('qrStatus').textContent = '请用B站手机APP扫描二维码';
  // Use a QR code image API
  document.getElementById('qrImage').innerHTML = '<img src="https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=' + encodeURIComponent(data.url) + '" alt="QR Code" style="width:200px;height:200px;">';

  // Start polling
  qrPollTimer = setInterval(async () => {
    const pollRes = await fetch('/api/admin/bili-qr/poll?key=' + data.qrcode_key);
    const pollData = await pollRes.json();

    switch (pollData.status) {
      case 'waiting':
        break;
      case 'scanned':
        document.getElementById('qrStatus').textContent = '✅ 已扫码，请在手机上确认';
        break;
      case 'confirmed':
        cancelQR();
        alert('登录成功！账号: ' + pollData.name + ' (UID: ' + pollData.uid + ')');
        loadBiliAccounts();
        break;
      case 'expired':
        cancelQR();
        alert('二维码已过期，请重新生成');
        break;
      case 'error':
        cancelQR();
        alert('登录失败: ' + (pollData.error || '未知错误'));
        break;
    }
  }, 2000);
}

function cancelQR() {
  if (qrPollTimer) { clearInterval(qrPollTimer); qrPollTimer = null; }
  document.getElementById('qrArea').style.display = 'none';
  document.getElementById('qrBtn').style.display = '';
}

// --- Audit ---

async function loadAudit() {
  const limit = document.getElementById('auditLimit').value;
  const res = await fetch('/api/admin/audit?limit=' + limit);
  const entries = await res.json() || [];
  const table = document.getElementById('auditTable');
  const body = document.getElementById('auditBody');
  table.style.display = '';
  body.innerHTML = entries.map(e =>
    '<tr><td style="white-space:nowrap;font-size:12px;">' + e.time + '</td><td>' + e.username + '</td><td>' + e.action + '</td><td style="font-size:12px;color:#aaa;">' + (e.detail||'') + '</td><td style="font-size:12px;color:#666;">' + (e.ip||'') + '</td></tr>'
  ).join('') || '<tr><td colspan="5" style="text-align:center;color:#666;">暂无记录</td></tr>';
}

init();
</script>
</body>
</html>`
