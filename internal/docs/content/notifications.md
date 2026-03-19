<!-- title: Notifications -->
<!-- category: Setup -->

# Notifications

BackupManager can notify you when backups succeed, fail, or detect integrity issues.
Supported channels are **Telegram** and **Email (SMTP)**.

## Configuring Telegram

### 1. Create a Bot

1. Open Telegram and search for `@BotFather`
2. Send `/newbot` and follow the prompts
3. Copy the **bot token** (format: `123456789:AAAA-...`)

### 2. Get Your Chat ID

Send any message to your new bot, then call:

```
https://api.telegram.org/bot<TOKEN>/getUpdates
```

Look for `"chat": {"id": 12345678}` in the response.
That number is your **chat ID**.

For group notifications: add the bot to a group and use the negative group chat ID.

### 3. Configure in BackupManager

1. Go to **Settings → Notifications**
2. Enable **Telegram**
3. Enter your **Bot Token** and **Chat ID**
4. Click **Test Notification** to verify
5. Click **Save**

## Configuring Email (SMTP)

BackupManager supports any SMTP server (Gmail, SendGrid, Mailgun, etc.).

### Settings

| Field | Example |
| ----- | ------- |
| SMTP Host | `smtp.gmail.com` |
| SMTP Port | `587` |
| Username | `you@gmail.com` |
| Password | App password or API key |
| From address | `backups@yourcompany.com` |
| To address | `ops@yourcompany.com` |
| TLS | STARTTLS (recommended) |

### Gmail Setup

1. Enable 2-factor authentication on your Google account
2. Go to **Google Account → Security → App Passwords**
3. Generate an app password for "Mail"
4. Use your Gmail address as the username and the app password

### Testing

After saving, click **Test Notification** to send a test message.
Check your Telegram or email inbox to confirm delivery.

## Notification Events

You can control which events trigger notifications:

| Event | Default |
| ----- | ------- |
| Backup completed | Enabled |
| Backup failed | Enabled |
| Backup partially failed | Enabled |
| Integrity check failed | Enabled |
| Server unreachable | Enabled |
| Snapshot expired | Disabled |

Configure events under **Settings → Notifications → Events**.

## Notification Log

All sent notifications are recorded in the **Notification Log**
(Settings → Notifications → Log).
Each entry shows the channel, event, timestamp, and delivery status.
