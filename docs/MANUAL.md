# LiveSub User Manual

<div id="manual-root">

<!-- Language Switcher -->
<div style="text-align:center;margin:20px 0 30px;">
  <strong style="margin-right:8px;">Language / 语言 / 言語:</strong>
  <a href="#" onclick="switchLang('zh');return false;" id="tab-zh" style="padding:6px 16px;border:2px solid #e94560;border-radius:6px;margin:0 4px;text-decoration:none;font-weight:bold;background:#e94560;color:#fff;">中文</a>
  <a href="#" onclick="switchLang('en');return false;" id="tab-en" style="padding:6px 16px;border:2px solid #888;border-radius:6px;margin:0 4px;text-decoration:none;color:#888;">English</a>
  <a href="#" onclick="switchLang('ja');return false;" id="tab-ja" style="padding:6px 16px;border:2px solid #888;border-radius:6px;margin:0 4px;text-decoration:none;color:#888;">日本語</a>
</div>

<script>
function switchLang(lang) {
  ['zh','en','ja'].forEach(function(l) {
    var section = document.getElementById('lang-' + l);
    var tab = document.getElementById('tab-' + l);
    if (l === lang) {
      section.style.display = 'block';
      tab.style.background = '#e94560';
      tab.style.color = '#fff';
      tab.style.borderColor = '#e94560';
    } else {
      section.style.display = 'none';
      tab.style.background = 'transparent';
      tab.style.color = '#888';
      tab.style.borderColor = '#888';
    }
  });
}
</script>

<!-- ==================== CHINESE ==================== -->
<div id="lang-zh" style="display:block;">

## 目录

1. [快速上手](#zh-getting-started)
2. [了解主页面](#zh-main-page)
3. [暂停与恢复输出](#zh-pause-resume)
4. [3秒延迟队列](#zh-delay-queue)
5. [序号表情开关](#zh-show-seq)
6. [下载字幕记录](#zh-transcripts)
7. [设置页面（管理输出）](#zh-settings)
8. [管理员功能](#zh-admin)
9. [弹幕指令](#zh-commands)
10. [常见问题](#zh-troubleshooting)

---

<h3 id="zh-getting-started">1. 快速上手</h3>

#### 登录

打开浏览器，输入管理员告诉你的地址（通常是 `http://服务器IP:8899`）。你会看到登录页面。

[Screenshot: 登录页面 — 深色背景，中间有用户名和密码输入框，右上角有中文/EN/日本語语言切换]

1. 在右上角可以切换界面语言（**中文** / **EN** / **日本語**）
2. 输入你的 **用户名** 和 **密码**
3. 点击 **登录** 按钮

> 登录后 7 天内无需重新登录，即使服务重启也不受影响。

#### 初次进入主页面

登录成功后，你会自动跳转到 **控制面板**。根据你的角色，你会看到不同的内容：

- **管理员**：可以看到所有直播间、所有账号，并且右上角有「⚙️ 管理」按钮
- **普通用户**：只能看到管理员分配给你的直播间

---

<h3 id="zh-main-page">2. 了解主页面</h3>

控制面板是你最常用的页面。它展示了所有直播间的翻译状态。

[Screenshot: 控制面板主页 — 顶部标题栏，下方是多个直播间卡片]

#### 页面结构

- **顶部栏**：标题、语言切换、用户名、管理入口、退出登录
- **直播间卡片**：每个配置的主播对应一张卡片
- **字幕记录区**：页面底部可下载历史字幕文件

#### 直播间卡片

每张卡片包含：

| 元素 | 说明 |
|------|------|
| **主播名称** | 卡片左上角，如「VTuber A」 |
| **房间号** | 卡片右上角的 `#12345` |
| **直播状态** | 🔴 **直播中**（红色标签）或 ⚫ **未开播**（灰色标签） |
| **输出卡片** | 每个翻译输出对应一张小卡片（见下文） |

#### 输出卡片

每个输出卡片展示一个翻译通道的状态：

[Screenshot: 输出卡片 — 显示名称、平台信息、翻译状态标签、待发送/已发送消息、暂停按钮]

| 元素 | 说明 |
|------|------|
| **输出名称** | 如「中文翻译」「English」 |
| **信息行** | `bilibili | zh-CN | 🔑 账号: bot1` |
| **状态标签** | ▶️ **翻译中**（蓝色）或 ⏸ **已暂停**（橙色） |
| **待发送** | 延迟队列中等待发送的消息（详见第4节） |
| **已发送** | 最近成功发送的5条弹幕 |
| **操作按钮** | ⏸ 暂停翻译 / ▶️ 恢复翻译 |
| **序号开关** | 显示序号 0️⃣~🔟 复选框 |

> 如果输出配置了多个账号轮发，信息行会显示如 `bot1, bot2 (2个轮发)`。

---

<h3 id="zh-pause-resume">3. 暂停与恢复输出</h3>

你可以随时暂停或恢复任何一个翻译输出。

#### 从网页暂停

1. 找到你想控制的输出卡片
2. 点击底部的 **⏸ 暂停翻译** 按钮
3. 状态标签会变为橙色的「⏸ 已暂停」
4. 按钮变为 **▶️ 恢复翻译**

[Screenshot: 暂停状态的输出卡片 — 橙色「已暂停」标签，绿色「恢复翻译」按钮]

#### 注意事项

- 暂停后，翻译仍在后台进行，**字幕记录会继续写入 CSV 文件**，只是不发送弹幕
- 暂停/恢复操作会被记录在审计日志中
- 即使主播下播再开播，暂停状态会保持（不会自动恢复）

---

<h3 id="zh-delay-queue">4. 3秒延迟队列</h3>

为了让你有机会在弹幕发出前审核内容，每条翻译会先在 **待发送** 队列中停留约 3 秒。

#### 待发送区域

当有消息在队列中时，输出卡片会显示一个红色的 **⏳ 待发送** 区域：

[Screenshot: 待发送队列 — 红色标题，每条消息显示倒计时秒数和跳过按钮]

- 每条消息显示剩余等待时间，如 `2s | 今天天气真好呢`
- 右侧有 **跳过** 按钮

#### 跳过消息

如果你看到一条不合适的翻译，点击 **跳过** 按钮即可取消发送。消息将被丢弃，不会以弹幕形式发出。

#### 已发送区域

消息成功发送后，会出现在绿色的 **✅ 已发送** 区域，最多显示最近 5 条，方便你确认弹幕内容。

---

<h3 id="zh-show-seq">5. 序号表情开关</h3>

开启后，每条弹幕前面会加上数字表情（0️⃣ 1️⃣ 2️⃣ ... 🔟），帮助观众区分不同句子的先后顺序。

#### 使用方法

1. 在输出卡片底部找到 **显示序号 0️⃣~🔟** 复选框
2. 勾选即开启，取消勾选即关闭
3. 设置会自动保存

#### 效果示例

开启前：
```
【翻译】大家好
【翻译】今天天气真好呢
```

开启后：
```
【翻译】1️⃣ 大家好
【翻译】2️⃣ 今天天气真好呢
```

> 序号到 🔟 后会循环回 0️⃣。序号出现在用户前缀之后。

---

<h3 id="zh-transcripts">6. 下载字幕记录</h3>

每次直播会自动生成字幕记录文件（CSV 格式），记录每句翻译的原文和译文。

#### 下载方法

1. 在控制面板底部找到 **📄 字幕记录** 区域
2. 点击 **刷新** 按钮加载文件列表
3. 找到你需要的文件，点击 **⬇ 下载**

[Screenshot: 字幕记录区域 — 文件名、大小、时间、下载按钮的表格]

#### 文件格式

文件名格式：`房间号_主播名_日期_时间.csv`

例如：`12345_VTuberA_20260219_143000.csv`

CSV 包含以下列：

| 列 | 说明 |
|----|------|
| 时间 | 翻译发生的实际时间 |
| 时间轴 | 从开播算起的时间偏移 |
| 原文语言 | 如 `ja-JP` |
| 原文 | 识别到的原始语音内容 |
| 目标语言 | 如 `zh-CN` |
| 翻译 | 翻译后的文本 |

> 文件使用 UTF-8 编码（带 BOM），可以直接用 Excel 打开不乱码。

> 普通用户只能下载自己被分配的直播间的字幕记录。

---

<h3 id="zh-settings">7. 设置页面（管理输出）</h3>

输出管理功能已合并到管理面板中。普通用户和管理员都可以在管理面板的 **📤 输出管理** 区域管理自己有权限的直播间的输出。

#### 进入管理面板

点击页面右上角的 **⚙️ 管理** 按钮进入管理面板。

> 普通用户进入后，只会看到「📤 输出管理」区域。管理员才能看到所有管理功能。

#### 管理输出

[Screenshot: 输出管理区域 — 顶部有主播选择下拉框，下方是输出列表和添加表单]

1. **选择主播**：从下拉框选择你要管理的主播
2. **查看现有输出**：表格显示所有输出的名称、平台、目标语言、账号、房间号、前缀、后缀
3. **添加/编辑输出**：

| 字段 | 说明 |
|------|------|
| 名称 | 输出的显示名称，如「中文翻译」 |
| 平台 | 目前只有 `bilibili` |
| 目标语言 | 翻译目标语言（留空则直传原文） |
| 账号 | 选择一个或多个B站账号（多选即为轮发模式） |
| 房间号 | 发送弹幕的目标房间（0 = 主播所在房间） |
| 前缀 | 弹幕前面加的文字，如 `【翻译】` |
| 后缀 | 弹幕后面加的文字 |

4. 填写完成后点击 **保存**
5. 要删除某个输出，点击对应行的 **删除** 按钮

> 新添加的输出默认处于暂停状态，需要在控制面板手动恢复。

---

<h3 id="zh-admin">8. 管理员功能</h3>

> 以下功能仅限管理员角色使用。

点击控制面板右上角的 **⚙️ 管理** 进入管理面板。

[Screenshot: 管理面板全景 — 多个区域：主播管理、输出管理、用户列表、B站账号、操作记录]

#### 📺 主播管理

| 操作 | 说明 |
|------|------|
| 查看 | 表格显示所有主播的名称、房间号、识别语言、输出列表、指令白名单 |
| 添加 | 填写主播名称、房间号（支持 URL 或数字）、识别语言，点击保存 |
| 编辑 | 点击某个主播的 **编辑** 按钮，表单会自动填入现有配置 |
| 删除 | 点击 **删除** 并确认 |
| 指令白名单 | 填写允许使用弹幕指令的 B站 UID（逗号分隔） |

#### 👥 用户管理

| 操作 | 说明 |
|------|------|
| 查看 | 表格显示用户名、角色、已分配的账号和直播间 |
| 添加用户 | 填写用户名、密码，勾选是否为管理员，分配B站账号和直播间 |
| 编辑用户 | 点击 **编辑**，弹窗修改密码、重新分配账号/直播间 |
| 删除用户 | 点击 **删除** 并确认（管理员账号不可删除） |

#### 权限说明

| 角色 | 直播间 | 账号 | 字幕记录 | 管理面板 |
|------|--------|------|----------|----------|
| 管理员 | 全部 | 全部 | 全部 | 完整访问 |
| 普通用户 | 仅分配的 | 仅分配的 | 仅分配的直播间 | 仅输出管理 |

#### 🎮 B站弹幕账号

用于管理发送弹幕的 B站账号。

**添加账号（扫码登录）：**

1. 点击 **📱 扫码添加账号** 按钮
2. 页面会显示一个二维码
3. 用 **B站手机 APP** 扫描这个二维码
4. 在手机上确认登录
5. 登录成功后，账号会自动添加到列表

[Screenshot: QR 扫码登录 — 二维码图片居中，下方有「已扫码，请在手机上确认」提示]

**管理已有账号：**

| 列 | 说明 |
|----|------|
| 名称 | 账号昵称 |
| UID | B站用户 ID |
| 弹幕上限 | 每条弹幕的最大字符数（默认 20，UL20+ 可设为 30） |
| 添加时间 | 账号添加的时间 |
| 状态 | **有效** 或 **已失效** |

> 弹幕上限可以直接在表格中修改，改完后自动保存。

#### 📋 操作记录（审计日志）

记录所有用户的操作，包括登录、暂停/恢复翻译、添加/删除账号等。

1. 选择显示条数（最近 50 / 100 / 500 条）
2. 点击 **加载记录**
3. 查看操作时间、用户、操作类型、详情和 IP 地址

---

<h3 id="zh-commands">9. 弹幕指令</h3>

除了网页操作，你还可以直接在B站直播间的弹幕中发送指令来控制翻译。

> 只有在配置中加入了你的 B站 UID 的白名单用户才能使用指令。

#### 指令列表

| 指令 | 别名 | 功能 |
|------|------|------|
| `/off` | `/pause` `/暂停` | 暂停所有翻译输出 |
| `/on` | `/resume` `/恢复` | 恢复所有翻译输出 |
| `/off 名称` | `/pause 名称` `/暂停 名称` | 暂停指定输出（如 `/off 中文翻译`） |
| `/on 名称` | `/resume 名称` `/恢复 名称` | 恢复指定输出 |
| `/list` | `/列表` | 查看所有输出的 ▶/⏸ 状态 |
| `/help` | `/帮助` | 显示指令帮助信息 |

#### 使用示例

在B站直播间的弹幕框中输入：

- 暂停所有翻译：发送 `/off`
- 只暂停中文翻译：发送 `/off 中文翻译`
- 恢复所有翻译：发送 `/on`
- 查看当前状态：发送 `/list`，机器人会回复如 `▶中文翻译 | ⏸English`

> 指令回复由账号池轮发，以提高速度和避免频率限制。

---

<h3 id="zh-troubleshooting">10. 常见问题</h3>

#### 登录不上去

- 检查用户名和密码是否正确（区分大小写）
- 如果忘记密码，联系管理员重置
- 检查浏览器是否允许 Cookie

#### 看不到直播间

- 普通用户只能看到管理员分配给你的直播间
- 联系管理员检查你的权限配置

#### 翻译没有在弹幕中出现

- 检查输出是否处于 **暂停** 状态（橙色标签）
- 检查主播是否在 **直播中**（红色标签）
- 翻译需要主播说话并被语音识别成功
- 检查B站账号状态是否 **有效**
- 新添加的输出默认暂停，需要手动恢复

#### 弹幕显示不完整

- 弹幕有字数限制（默认 20 字，UL20+ 账号可设为 30 字）
- 超长翻译会自动拆分为多条弹幕发送
- 前缀和后缀也占用字数

#### 弹幕指令没有反应

- 确认你的 B站 UID 在该主播的指令白名单中
- 指令需要以 `/` 开头
- 主播当前必须在直播中

#### 字幕记录看不到

- 点击字幕记录区域的 **刷新** 按钮
- 字幕记录只在主播直播时生成
- 普通用户只能看到自己被分配的直播间的记录

#### 页面数据没有更新

- LiveSub 使用 WebSocket 实时推送状态更新
- 如果长时间未更新，尝试刷新页面
- 后台还有 5 秒一次的轮询作为备用

</div>

<!-- ==================== ENGLISH ==================== -->
<div id="lang-en" style="display:none;">

## Table of Contents

1. [Getting Started](#en-getting-started)
2. [Understanding the Main Page](#en-main-page)
3. [Pause and Resume Outputs](#en-pause-resume)
4. [3-Second Delay Queue](#en-delay-queue)
5. [Sequence Emoji Toggle](#en-show-seq)
6. [Downloading Transcripts](#en-transcripts)
7. [Settings (Managing Outputs)](#en-settings)
8. [Admin Features](#en-admin)
9. [Danmaku Commands](#en-commands)
10. [Troubleshooting](#en-troubleshooting)

---

<h3 id="en-getting-started">1. Getting Started</h3>

#### Logging In

Open your browser and go to the address your administrator gave you (usually `http://server-ip:8899`). You'll see the login page.

[Screenshot: Login page — dark background, username and password fields centered, language switcher in top-right corner]

1. You can switch the interface language in the top-right corner (**中文** / **EN** / **日本語**)
2. Enter your **username** and **password**
3. Click the **Login** button

> Once logged in, you'll stay signed in for 7 days — even if the service restarts.

#### First Look at the Dashboard

After logging in, you'll land on the **Control Panel**. What you see depends on your role:

- **Admin**: All rooms are visible, plus an "⚙️ Admin" button in the top-right
- **Regular user**: Only the rooms your administrator has assigned to you

---

<h3 id="en-main-page">2. Understanding the Main Page</h3>

The Control Panel is where you'll spend most of your time. It shows the live translation status for every room.

[Screenshot: Control Panel — title bar at top, multiple streamer cards below]

#### Page Layout

- **Top bar**: Title, language switcher, your username, Admin link, Logout
- **Streamer cards**: One card per configured streamer
- **Transcript section**: Download history at the bottom of the page

#### Streamer Cards

Each card contains:

| Element | Description |
|---------|-------------|
| **Streamer name** | Top-left of the card, e.g. "VTuber A" |
| **Room ID** | Top-right, shown as `#12345` |
| **Live status** | 🔴 **Live** (red badge) or ⚫ **Offline** (gray badge) |
| **Output cards** | One mini-card per translation output (see below) |

#### Output Cards

Each output card shows the status of a single translation channel:

[Screenshot: Output card — name, platform info, translation status badge, pending/sent messages, pause button]

| Element | Description |
|---------|-------------|
| **Output name** | e.g. "中文翻译", "English" |
| **Info line** | `bilibili | zh-CN | 🔑 Account: bot1` |
| **Status badge** | ▶️ **Translating** (blue) or ⏸ **Paused** (orange) |
| **Pending** | Messages waiting in the delay queue (see Section 4) |
| **Sent** | Last 5 successfully sent danmaku messages |
| **Action button** | ⏸ Pause / ▶️ Resume |
| **Seq toggle** | "Show seq 0️⃣~🔟" checkbox |

> If an output uses multiple accounts for rotation, the info line shows something like `bot1, bot2 (2 rotating)`.

---

<h3 id="en-pause-resume">3. Pause and Resume Outputs</h3>

You can pause or resume any translation output at any time.

#### Pausing from the Web UI

1. Find the output card you want to control
2. Click the **⏸ Pause** button at the bottom
3. The status badge turns orange: "⏸ Paused"
4. The button changes to **▶️ Resume**

[Screenshot: Paused output card — orange "Paused" badge, green "Resume" button]

#### Important Notes

- While paused, translation still runs in the background. **Transcripts keep recording to CSV** — only danmaku sending is stopped
- Pause/resume actions are logged in the audit trail
- The pause state persists across stream sessions (if the streamer goes offline and comes back, the output stays paused)

---

<h3 id="en-delay-queue">4. 3-Second Delay Queue</h3>

To give you a chance to review translations before they're sent as danmaku, each message sits in a **pending** queue for about 3 seconds.

#### Pending Area

When messages are queued, the output card shows a red **⏳ Pending** section:

[Screenshot: Pending queue — red header, each message showing countdown seconds and a Skip button]

- Each message shows its remaining wait time, e.g. `2s | The weather is nice today`
- There's a **Skip** button on the right side

#### Skipping Messages

If you spot an inappropriate or incorrect translation, click **Skip** to cancel it. The message will be discarded and won't be sent as danmaku.

#### Sent Area

After a message is successfully sent, it appears in the green **✅ Sent** area. Up to 5 recent messages are shown so you can confirm what was delivered.

---

<h3 id="en-show-seq">5. Sequence Emoji Toggle</h3>

When enabled, each danmaku is prefixed with a number emoji (0️⃣ 1️⃣ 2️⃣ ... 🔟) to help viewers track the order of messages.

#### How to Use

1. Find the **Show seq 0️⃣~🔟** checkbox at the bottom of an output card
2. Check to enable, uncheck to disable
3. The setting is saved automatically

#### Example

Before enabling:
```
【翻译】Hello everyone
【翻译】The weather is nice today
```

After enabling:
```
【翻译】1️⃣ Hello everyone
【翻译】2️⃣ The weather is nice today
```

> The numbers cycle back to 0️⃣ after reaching 🔟. The number appears after your configured prefix.

---

<h3 id="en-transcripts">6. Downloading Transcripts</h3>

Every live session automatically generates a transcript file (CSV format) recording every original and translated line.

#### How to Download

1. Scroll to the **📄 Transcripts** section at the bottom of the Control Panel
2. Click the **Refresh** button to load the file list
3. Find the file you need, then click **⬇ Download**

[Screenshot: Transcripts section — table with filename, size, time, and download button]

#### File Format

Filename pattern: `RoomID_StreamerName_Date_Time.csv`

For example: `12345_VTuberA_20260219_143000.csv`

The CSV contains these columns:

| Column | Description |
|--------|-------------|
| Time | Actual clock time when the translation occurred |
| Timeline | Offset from stream start |
| Source Language | e.g. `ja-JP` |
| Source Text | The original speech recognized by STT |
| Target Language | e.g. `zh-CN` |
| Translation | The translated text |

> The file uses UTF-8 encoding with BOM, so it opens correctly in Excel without encoding issues.

> Regular users can only download transcripts for rooms they've been assigned to.

---

<h3 id="en-settings">7. Settings (Managing Outputs)</h3>

Output management has been merged into the Admin Panel. Both regular users and admins can manage outputs for their permitted rooms in the **📤 Output Management** section.

#### Accessing the Settings

Click the **⚙️ Admin** button in the top-right corner of the Control Panel.

> Regular users will only see the "📤 Output Management" section. Admins see all management features.

#### Managing Outputs

[Screenshot: Output Management — streamer dropdown at top, output list table, add/edit form below]

1. **Select a streamer** from the dropdown
2. **View existing outputs**: The table shows each output's name, platform, target language, account, room ID, prefix, and suffix
3. **Add or edit an output**:

| Field | Description |
|-------|-------------|
| Name | Display name for this output, e.g. "中文翻译" |
| Platform | Currently only `bilibili` |
| Target Language | The language to translate into (leave empty for source passthrough) |
| Account | Select one or more Bilibili accounts (multiple = rotation mode) |
| Room ID | Where to send danmaku (0 = same room as the streamer) |
| Prefix | Text prepended to each danmaku, e.g. `【翻译】` |
| Suffix | Text appended to each danmaku |

4. Click **Save** when done
5. To delete an output, click the **Delete** button in its row

> Newly added outputs start in paused state — you need to resume them on the Control Panel.

---

<h3 id="en-admin">8. Admin Features</h3>

> The following features are available to admin users only.

Click **⚙️ Admin** in the top-right corner of the Control Panel.

[Screenshot: Admin Panel overview — sections for Streamer Management, Output Management, Users, Bilibili Accounts, Audit Log]

#### 📺 Streamer Management

| Action | Description |
|--------|-------------|
| View | Table showing all streamers with name, room ID, source language, outputs, command whitelist |
| Add | Fill in the streamer name, room ID (URL or number), source language, then click Save |
| Edit | Click a streamer's **Edit** button — the form auto-fills with its current settings |
| Delete | Click **Delete** and confirm |
| Command Whitelist | Enter Bilibili UIDs allowed to use danmaku commands (comma-separated) |

#### 👥 User Management

| Action | Description |
|--------|-------------|
| View | Table showing username, role, assigned accounts, and assigned rooms |
| Add User | Enter username, password, check Admin if needed, assign Bilibili accounts and rooms |
| Edit User | Click **Edit** to update password, reassign accounts/rooms via popup |
| Delete User | Click **Delete** and confirm (admin accounts cannot be deleted) |

#### Permission Model

| Role | Rooms | Accounts | Transcripts | Admin Panel |
|------|-------|----------|-------------|-------------|
| Admin | All | All | All | Full access |
| User | Assigned only | Assigned only | Assigned rooms only | Output management only |

#### 🎮 Bilibili Accounts

Manage the Bilibili accounts used for sending danmaku.

**Adding an Account (QR Login):**

1. Click the **📱 QR Code Login** button
2. A QR code appears on screen
3. Scan it with the **Bilibili mobile app**
4. Confirm the login on your phone
5. Once confirmed, the account is automatically added

[Screenshot: QR Login — QR code image centered, with "Scanned, please confirm on phone" status text]

**Managing Existing Accounts:**

| Column | Description |
|--------|-------------|
| Name | Account nickname |
| UID | Bilibili user ID |
| Max Length | Maximum characters per danmaku (default 20, UL20+ can use 30) |
| Created | When the account was added |
| Status | **Valid** or **Expired** |

> You can edit the max length directly in the table — changes save automatically.

#### 📋 Audit Log

Tracks all user actions: logins, pause/resume toggles, account additions/deletions, and more.

1. Choose how many entries to show (Last 50 / 100 / 500)
2. Click **Load Log**
3. Review the timestamp, user, action, details, and IP address

---

<h3 id="en-commands">9. Danmaku Commands</h3>

Besides the web UI, you can also control translations by typing commands directly in the Bilibili live chat.

> Only users whose Bilibili UID is on the streamer's command whitelist can use these commands.

#### Command Reference

| Command | Aliases | Effect |
|---------|---------|--------|
| `/off` | `/pause` `/暂停` | Pause all translation outputs |
| `/on` | `/resume` `/恢复` | Resume all translation outputs |
| `/off name` | `/pause name` `/暂停 name` | Pause a specific output (e.g. `/off 中文翻译`) |
| `/on name` | `/resume name` `/恢复 name` | Resume a specific output |
| `/list` | `/列表` | Show all outputs with ▶/⏸ status |
| `/help` | `/帮助` | Display command help |

#### Usage Examples

Type these into the Bilibili live chat:

- Pause everything: send `/off`
- Pause only Chinese output: send `/off 中文翻译`
- Resume everything: send `/on`
- Check current status: send `/list` — the bot replies with something like `▶中文翻译 | ⏸English`

> Command replies are sent via account pool rotation for speed and to avoid rate limits.

---

<h3 id="en-troubleshooting">10. Troubleshooting</h3>

#### Can't log in

- Double-check your username and password (case-sensitive)
- If you forgot your password, ask your administrator to reset it
- Make sure your browser accepts cookies

#### Can't see any rooms

- Regular users only see rooms their administrator has assigned to them
- Contact your admin to check your permission settings

#### Translation isn't appearing as danmaku

- Check if the output is **Paused** (orange badge)
- Check if the streamer is **Live** (red badge)
- Translation requires the streamer to be speaking and STT to detect voice
- Check that the Bilibili account status is **Valid**
- Newly added outputs start paused — resume them manually

#### Danmaku text is cut off

- Danmaku has a character limit (default 20, UL20+ accounts can use 30)
- Long translations are automatically split into multiple danmaku messages
- Prefix and suffix also count toward the character limit

#### Danmaku commands not working

- Confirm your Bilibili UID is on the streamer's command whitelist
- Commands must start with `/`
- The streamer must currently be live

#### Can't see transcripts

- Click the **Refresh** button in the Transcripts section
- Transcripts are only generated during live sessions
- Regular users can only see transcripts for their assigned rooms

#### Page data isn't updating

- LiveSub uses WebSocket for real-time status pushes
- If nothing has updated for a while, try refreshing the page
- There's also a 5-second polling fallback running in the background

</div>

<!-- ==================== JAPANESE ==================== -->
<div id="lang-ja" style="display:none;">

## 目次

1. [はじめに](#ja-getting-started)
2. [メインページの見方](#ja-main-page)
3. [出力の一時停止と再開](#ja-pause-resume)
4. [3秒遅延キュー](#ja-delay-queue)
5. [番号絵文字の切り替え](#ja-show-seq)
6. [字幕記録のダウンロード](#ja-transcripts)
7. [設定ページ（出力管理）](#ja-settings)
8. [管理者機能](#ja-admin)
9. [弾幕コマンド](#ja-commands)
10. [よくある質問](#ja-troubleshooting)

---

<h3 id="ja-getting-started">1. はじめに</h3>

#### ログイン

ブラウザを開いて、管理者から教えてもらったアドレス（通常は `http://サーバーIP:8899`）にアクセスします。ログインページが表示されます。

[Screenshot: ログインページ — ダークテーマの背景、中央にユーザー名とパスワードの入力欄、右上に中文/EN/日本語の言語切替]

1. 右上で表示言語を切り替えられます（**中文** / **EN** / **日本語**）
2. **ユーザー名** と **パスワード** を入力します
3. **ログイン** ボタンをクリックします

> 一度ログインすると、サービスが再起動しても 7日間 はログイン状態が維持されます。

#### ダッシュボードの初見

ログインすると **コントロールパネル** に移動します。あなたの権限によって表示内容が異なります：

- **管理者**：すべての配信ルームが表示され、右上に「⚙️ 管理」ボタンがあります
- **一般ユーザー**：管理者から割り当てられた配信ルームのみ表示されます

---

<h3 id="ja-main-page">2. メインページの見方</h3>

コントロールパネルは最もよく使うページです。各配信ルームのリアルタイム翻訳状況を確認できます。

[Screenshot: コントロールパネル — 上部にタイトルバー、下部に配信者カードが並ぶ]

#### ページの構成

- **上部バー**：タイトル、言語切替、ユーザー名、管理画面リンク、ログアウト
- **配信者カード**：設定された配信者ごとに1枚のカード
- **字幕記録エリア**：ページ下部で過去の字幕ファイルをダウンロード

#### 配信者カード

各カードの要素：

| 要素 | 説明 |
|------|------|
| **配信者名** | カード左上（例：「VTuber A」） |
| **ルームID** | カード右上の `#12345` |
| **配信状態** | 🔴 **配信中**（赤いバッジ）または ⚫ **オフライン**（グレーバッジ） |
| **出力カード** | 各翻訳出力に対応するミニカード（後述） |

#### 出力カード

各出力カードは、翻訳チャンネルのステータスを表示します：

[Screenshot: 出力カード — 名前、プラットフォーム情報、翻訳状態バッジ、送信待ち/送信済みメッセージ、一時停止ボタン]

| 要素 | 説明 |
|------|------|
| **出力名** | 例：「中文翻訳」「English」 |
| **情報行** | `bilibili | zh-CN | 🔑 アカウント: bot1` |
| **状態バッジ** | ▶️ **翻訳中**（青）または ⏸ **一時停止**（オレンジ） |
| **送信待ち** | 遅延キューで待機中のメッセージ（第4節参照） |
| **送信済み** | 最近送信された弾幕5件 |
| **操作ボタン** | ⏸ 一時停止 / ▶️ 再開 |
| **番号切替** | 「番号表示 0️⃣~🔟」チェックボックス |

> 出力に複数のアカウントが設定されている場合、情報行に `bot1, bot2 (2アカウント輪番)` のように表示されます。

---

<h3 id="ja-pause-resume">3. 出力の一時停止と再開</h3>

いつでも翻訳出力を一時停止・再開できます。

#### Webページから一時停止

1. 操作したい出力カードを見つけます
2. カード下部の **⏸ 一時停止** ボタンをクリックします
3. 状態バッジがオレンジの「⏸ 一時停止」に変わります
4. ボタンが **▶️ 再開** に変わります

[Screenshot: 一時停止中の出力カード — オレンジの「一時停止」バッジ、緑の「再開」ボタン]

#### 注意点

- 一時停止中も翻訳はバックグラウンドで動作し、**字幕記録のCSVファイルへの書き込みは継続されます**。弾幕の送信のみが停止します
- 一時停止/再開の操作は監査ログに記録されます
- 配信者がオフラインになって再開しても、一時停止状態は維持されます（自動的に再開されません）

---

<h3 id="ja-delay-queue">4. 3秒遅延キュー</h3>

弾幕として送信される前に翻訳内容を確認できるよう、各メッセージは **送信待ち** キューで約3秒間待機します。

#### 送信待ちエリア

キューにメッセージがあると、出力カードに赤い **⏳ 送信待ち** エリアが表示されます：

[Screenshot: 送信待ちキュー — 赤いヘッダー、各メッセージにカウントダウン秒数とスキップボタン]

- 各メッセージに残り待機時間が表示されます（例：`2s | 今日は天気がいいですね`）
- 右側に **スキップ** ボタンがあります

#### メッセージのスキップ

不適切な翻訳を見つけた場合、**スキップ** ボタンをクリックして送信をキャンセルできます。メッセージは破棄され、弾幕として送信されません。

#### 送信済みエリア

メッセージが正常に送信されると、緑の **✅ 送信済み** エリアに表示されます。最近5件まで表示されるので、送信された内容を確認できます。

---

<h3 id="ja-show-seq">5. 番号絵文字の切り替え</h3>

有効にすると、各弾幕の先頭に番号絵文字（0️⃣ 1️⃣ 2️⃣ ... 🔟）が付加され、視聴者がメッセージの順序を把握しやすくなります。

#### 使い方

1. 出力カード下部の **番号表示 0️⃣~🔟** チェックボックスを見つけます
2. チェックを入れると有効、外すと無効になります
3. 設定は自動的に保存されます

#### 表示例

有効化前：
```
【翻訳】みなさんこんにちは
【翻訳】今日は天気がいいですね
```

有効化後：
```
【翻訳】1️⃣ みなさんこんにちは
【翻訳】2️⃣ 今日は天気がいいですね
```

> 番号は 🔟 に達すると 0️⃣ に戻ります。番号はプレフィックスの後に表示されます。

---

<h3 id="ja-transcripts">6. 字幕記録のダウンロード</h3>

配信セッションごとに字幕記録ファイル（CSV形式）が自動生成され、原文と翻訳文がすべて記録されます。

#### ダウンロード方法

1. コントロールパネル下部の **📄 字幕記録** エリアまでスクロールします
2. **更新** ボタンをクリックしてファイル一覧を読み込みます
3. 必要なファイルを見つけ、**⬇ DL** をクリックします

[Screenshot: 字幕記録エリア — ファイル名、サイズ、日時、ダウンロードボタンのテーブル]

#### ファイル形式

ファイル名の形式：`ルームID_配信者名_日付_時刻.csv`

例：`12345_VTuberA_20260219_143000.csv`

CSVの列構成：

| 列 | 説明 |
|----|------|
| 時間 | 翻訳が行われた実際の時刻 |
| タイムライン | 配信開始からの経過時間 |
| 原文言語 | 例：`ja-JP` |
| 原文 | 音声認識された元の音声内容 |
| 翻訳先言語 | 例：`zh-CN` |
| 翻訳 | 翻訳されたテキスト |

> ファイルはUTF-8（BOM付き）で保存されるため、Excelで文字化けなく開けます。

> 一般ユーザーは割り当てられた配信ルームの字幕記録のみダウンロードできます。

---

<h3 id="ja-settings">7. 設定ページ（出力管理）</h3>

出力管理は管理パネルに統合されています。一般ユーザーと管理者の両方が、**📤 出力管理** セクションで権限のある配信ルームの出力を管理できます。

#### 管理パネルへのアクセス

コントロールパネル右上の **⚙️ 管理** ボタンをクリックします。

> 一般ユーザーは「📤 出力管理」セクションのみ表示されます。管理者はすべての管理機能にアクセスできます。

#### 出力の管理

[Screenshot: 出力管理 — 上部に配信者選択ドロップダウン、出力リストテーブル、下部に追加/編集フォーム]

1. **配信者を選択**：ドロップダウンから管理したい配信者を選びます
2. **既存の出力を確認**：テーブルに各出力の名前、プラットフォーム、翻訳先言語、アカウント、ルームID、プレフィックス、サフィックスが表示されます
3. **出力の追加/編集**：

| フィールド | 説明 |
|-----------|------|
| 名前 | 出力の表示名（例：「中文翻訳」） |
| プラットフォーム | 現在は `bilibili` のみ |
| ターゲット言語 | 翻訳先言語（空欄の場合は原文をそのまま転送） |
| アカウント | 1つまたは複数のBilibiliアカウントを選択（複数選択 = 輪番モード） |
| ルームID | 弾幕の送信先ルーム（0 = 配信者と同じルーム） |
| プレフィックス | 弾幕の先頭に付加するテキスト（例：`【翻訳】`） |
| サフィックス | 弾幕の末尾に付加するテキスト |

4. 入力が完了したら **保存** をクリックします
5. 出力を削除するには、該当行の **削除** ボタンをクリックします

> 新しく追加された出力はデフォルトで一時停止状態です。コントロールパネルで手動で再開してください。

---

<h3 id="ja-admin">8. 管理者機能</h3>

> 以下の機能は管理者のみ使用できます。

コントロールパネル右上の **⚙️ 管理** をクリックして管理パネルに移動します。

[Screenshot: 管理パネル概観 — 配信者管理、出力管理、ユーザー一覧、Bilibiliアカウント、操作ログの各セクション]

#### 📺 配信者管理

| 操作 | 説明 |
|------|------|
| 一覧 | 配信者名、ルームID、認識言語、出力一覧、コマンド許可リストのテーブル |
| 追加 | 配信者名、ルームID（URLまたは数字）、認識言語を入力して保存 |
| 編集 | **編集** ボタンをクリックすると、フォームに現在の設定が自動入力されます |
| 削除 | **削除** をクリックして確認 |
| コマンド許可リスト | 弾幕コマンドの使用を許可するBilibili UID（カンマ区切り） |

#### 👥 ユーザー管理

| 操作 | 説明 |
|------|------|
| 一覧 | ユーザー名、権限、割当済みアカウント、割当済みルームのテーブル |
| ユーザー追加 | ユーザー名、パスワード入力、管理者フラグ設定、Bilibiliアカウントとルームの割り当て |
| ユーザー編集 | **編集** をクリックしてパスワード変更、アカウント/ルームの再割り当てをポップアップで行います |
| ユーザー削除 | **削除** をクリックして確認（管理者アカウントは削除できません） |

#### 権限モデル

| 権限 | 配信ルーム | アカウント | 字幕記録 | 管理パネル |
|------|-----------|-----------|----------|-----------|
| 管理者 | すべて | すべて | すべて | フルアクセス |
| 一般ユーザー | 割当のみ | 割当のみ | 割当ルームのみ | 出力管理のみ |

#### 🎮 Bilibiliアカウント

弾幕送信に使用するBilibiliアカウントを管理します。

**アカウントの追加（QRコードログイン）：**

1. **📱 QRコードログイン** ボタンをクリックします
2. 画面にQRコードが表示されます
3. **Bilibiliスマホアプリ** でスキャンします
4. スマホでログインを確認します
5. 確認後、アカウントが自動的にリストに追加されます

[Screenshot: QRコードログイン — QRコード画像が中央に表示、「スキャン済み、スマホで確認してください」のステータステキスト]

**既存アカウントの管理：**

| 列 | 説明 |
|----|------|
| 名前 | アカウントのニックネーム |
| UID | BilibiliユーザーID |
| 最大文字数 | 弾幕1件あたりの最大文字数（デフォルト20、UL20+は30に設定可能） |
| 作成日 | アカウントが追加された日時 |
| ステータス | **有効** または **期限切れ** |

> 最大文字数はテーブル内で直接編集できます。変更は自動保存されます。

#### 📋 操作ログ

すべてのユーザーの操作を記録します：ログイン、一時停止/再開の切替、アカウントの追加/削除など。

1. 表示件数を選択します（最新50件 / 100件 / 500件）
2. **ログ読込** をクリックします
3. 日時、ユーザー、操作、詳細、IPアドレスを確認できます

---

<h3 id="ja-commands">9. 弾幕コマンド</h3>

Webページの操作に加えて、Bilibiliのライブチャットに直接コマンドを入力して翻訳を制御できます。

> 配信者のコマンド許可リストにBilibili UIDが登録されているユーザーのみ使用できます。

#### コマンド一覧

| コマンド | エイリアス | 機能 |
|---------|-----------|------|
| `/off` | `/pause` `/暂停` | すべての翻訳出力を一時停止 |
| `/on` | `/resume` `/恢复` | すべての翻訳出力を再開 |
| `/off 名前` | `/pause 名前` `/暂停 名前` | 特定の出力を一時停止（例：`/off 中文翻訳`） |
| `/on 名前` | `/resume 名前` `/恢复 名前` | 特定の出力を再開 |
| `/list` | `/列表` | すべての出力の ▶/⏸ 状態を表示 |
| `/help` | `/帮助` | コマンドのヘルプを表示 |

#### 使用例

Bilibiliのライブチャットに以下のように入力します：

- すべて停止：`/off` と送信
- 中国語出力のみ停止：`/off 中文翻訳` と送信
- すべて再開：`/on` と送信
- 現在の状態を確認：`/list` と送信 → ボットが `▶中文翻訳 | ⏸English` のように返信します

> コマンドへの返信はアカウントプールの輪番で送信されるため、高速かつレートリミット回避に効果的です。

---

<h3 id="ja-troubleshooting">10. よくある質問</h3>

#### ログインできない

- ユーザー名とパスワードが正しいか確認してください（大文字小文字を区別します）
- パスワードを忘れた場合は、管理者にリセットを依頼してください
- ブラウザがCookieを許可しているか確認してください

#### 配信ルームが表示されない

- 一般ユーザーには、管理者が割り当てたルームのみ表示されます
- 管理者に権限設定を確認してもらってください

#### 翻訳が弾幕として表示されない

- 出力が **一時停止** 状態（オレンジバッジ）になっていないか確認してください
- 配信者が **配信中**（赤いバッジ）であるか確認してください
- 翻訳には配信者の発話と音声認識の成功が必要です
- Bilibiliアカウントのステータスが **有効** であるか確認してください
- 新しく追加した出力はデフォルトで一時停止されているため、手動で再開してください

#### 弾幕のテキストが途切れる

- 弾幕には文字数制限があります（デフォルト20文字、UL20+アカウントは30文字に設定可能）
- 長い翻訳は自動的に複数の弾幕に分割して送信されます
- プレフィックスとサフィックスも文字数にカウントされます

#### 弾幕コマンドが反応しない

- あなたのBilibili UIDが配信者のコマンド許可リストに登録されているか確認してください
- コマンドは `/` で始める必要があります
- 配信者が現在配信中である必要があります

#### 字幕記録が見つからない

- 字幕記録エリアの **更新** ボタンをクリックしてください
- 字幕記録は配信中にのみ生成されます
- 一般ユーザーは割り当てられたルームの記録のみ閲覧できます

#### ページのデータが更新されない

- LiveSubはWebSocketによるリアルタイムのステータス配信を使用しています
- しばらく更新がない場合は、ページを再読み込みしてください
- バックグラウンドで5秒ごとのポーリングもフォールバックとして動作しています

</div>

</div>
