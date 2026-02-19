# LiveSub User Manual

> 📖 [中文](MANUAL_ZH.md) | [日本語](MANUAL_JA.md)

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


#### Important Notes

- While paused, translation still runs in the background. **Transcripts keep recording to CSV** — only danmaku sending is stopped
- Pause/resume actions are logged in the audit trail
- The pause state persists across stream sessions (if the streamer goes offline and comes back, the output stays paused)

---

<h3 id="en-delay-queue">4. 3-Second Delay Queue</h3>

To give you a chance to review translations before they're sent as danmaku, each message sits in a **pending** queue for about 3 seconds.

#### Pending Area

When messages are queued, the output card shows a red **⏳ Pending** section:


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
