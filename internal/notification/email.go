// internal/notification/email.go
package notification

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"
)

// SMTPConfig holds the configuration for the SMTP email notifier.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // sender email address
	UseTLS   bool
}

// EmailNotifier sends HTML emails via SMTP with optional anti-flood protection.
type EmailNotifier struct {
	config SMTPConfig

	// Anti-flood tracking (same as Telegram)
	mu        sync.Mutex
	lastAlert map[string]time.Time // key: alertKey → last alert time
}

// NewEmailNotifier creates a new EmailNotifier with the given SMTP config.
func NewEmailNotifier(config SMTPConfig) *EmailNotifier {
	return &EmailNotifier{
		config:    config,
		lastAlert: make(map[string]time.Time),
	}
}

// buildMIMEMessage constructs a MIME-formatted email message ready for SMTP delivery.
func buildMIMEMessage(from string, to []string, subject, htmlBody string) []byte {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("From: %s\r\n", from))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(htmlBody)
	return []byte(sb.String())
}

// Send sends an HTML email to the specified recipients.
func (e *EmailNotifier) Send(to []string, subject, htmlBody string) error {
	addr := fmt.Sprintf("%s:%d", e.config.Host, e.config.Port)
	msg := buildMIMEMessage(e.config.From, to, subject, htmlBody)

	var auth smtp.Auth
	if e.config.Username != "" {
		auth = smtp.PlainAuth("", e.config.Username, e.config.Password, e.config.Host)
	}

	if e.config.UseTLS {
		// Implicit TLS (e.g. port 465): dial a TLS connection directly.
		tlsCfg := &tls.Config{
			ServerName: e.config.Host,
		}
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return fmt.Errorf("email: TLS dial %s: %w", addr, err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, e.config.Host)
		if err != nil {
			return fmt.Errorf("email: SMTP client over TLS: %w", err)
		}
		defer client.Quit() //nolint:errcheck

		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("email: SMTP auth: %w", err)
			}
		}
		if err := client.Mail(e.config.From); err != nil {
			return fmt.Errorf("email: MAIL FROM: %w", err)
		}
		for _, recipient := range to {
			if err := client.Rcpt(recipient); err != nil {
				return fmt.Errorf("email: RCPT TO %s: %w", recipient, err)
			}
		}
		w, err := client.Data()
		if err != nil {
			return fmt.Errorf("email: DATA command: %w", err)
		}
		if _, err := w.Write(msg); err != nil {
			return fmt.Errorf("email: write message: %w", err)
		}
		if err := w.Close(); err != nil {
			return fmt.Errorf("email: close data writer: %w", err)
		}
		return nil
	}

	// Plain SMTP with optional STARTTLS (e.g. port 587).
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("email: dial %s: %w", addr, err)
	}

	client, err := smtp.NewClient(conn, e.config.Host)
	if err != nil {
		return fmt.Errorf("email: SMTP client: %w", err)
	}
	defer client.Quit() //nolint:errcheck

	// Attempt STARTTLS upgrade if supported.
	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsCfg := &tls.Config{ServerName: e.config.Host}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("email: STARTTLS: %w", err)
		}
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("email: SMTP auth: %w", err)
		}
	}
	if err := client.Mail(e.config.From); err != nil {
		return fmt.Errorf("email: MAIL FROM: %w", err)
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("email: RCPT TO %s: %w", recipient, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: DATA command: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("email: write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: close data writer: %w", err)
	}
	return nil
}

// SendWithAntiFlood sends only if the same alertKey hasn't been sent within cooldown.
// Returns true if the message was sent, false if it was suppressed.
func (e *EmailNotifier) SendWithAntiFlood(to []string, alertKey, subject, htmlBody string, cooldown time.Duration) (bool, error) {
	e.mu.Lock()
	last, exists := e.lastAlert[alertKey]
	now := time.Now()
	if exists && now.Sub(last) < cooldown {
		e.mu.Unlock()
		return false, nil
	}
	e.lastAlert[alertKey] = now
	e.mu.Unlock()

	if err := e.Send(to, subject, htmlBody); err != nil {
		// Roll back the timestamp so a retry can pass through.
		e.mu.Lock()
		if exists {
			e.lastAlert[alertKey] = last
		} else {
			delete(e.lastAlert, alertKey)
		}
		e.mu.Unlock()
		return false, err
	}

	return true, nil
}

// emailStatusStyle returns CSS color and label for a given status string.
func emailStatusStyle(status string) (color, label string) {
	switch strings.ToLower(status) {
	case "success", "ok":
		return "#28a745", strings.ToUpper(status)
	case "warning":
		return "#ffc107", "WARNING"
	default:
		return "#dc3545", strings.ToUpper(status)
	}
}

// emailHTML wraps content in the standard BackupManager HTML email shell.
func emailHTML(title, headerTitle, content string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>%s</title>
</head>
<body style="margin:0;padding:0;background:#f4f4f4;font-family:Arial,sans-serif;">
  <table width="100%%" cellpadding="0" cellspacing="0" style="background:#f4f4f4;padding:20px 0;">
    <tr><td align="center">
      <table width="600" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:6px;overflow:hidden;box-shadow:0 2px 8px rgba(0,0,0,.1);">
        <!-- Header -->
        <tr><td style="background:#1a1a2e;padding:20px 30px;">
          <h1 style="margin:0;color:#ffffff;font-size:22px;">BackupManager</h1>
          <p style="margin:4px 0 0;color:#a0a0c0;font-size:13px;">%s</p>
        </td></tr>
        <!-- Body -->
        <tr><td style="padding:30px;">
          %s
        </td></tr>
        <!-- Footer -->
        <tr><td style="background:#f8f8f8;padding:14px 30px;text-align:center;border-top:1px solid #e0e0e0;">
          <p style="margin:0;color:#888;font-size:12px;">Generated at %s UTC</p>
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`, title, headerTitle, content, time.Now().UTC().Format("2006-01-02 15:04:05"))
}

// FormatBackupEmail returns subject and HTML body for a backup notification.
func FormatBackupEmail(serverName, jobName, status, errorMsg string) (subject, body string) {
	color, label := emailStatusStyle(status)

	var errorSection string
	if errorMsg != "" {
		errorSection = fmt.Sprintf(`
          <tr>
            <td style="padding:8px 0;color:#555;font-weight:bold;">Error</td>
            <td style="padding:8px 0;color:#dc3545;">%s</td>
          </tr>`, errorMsg)
	}

	var subjectVerb string
	if strings.ToLower(status) == "success" {
		subjectVerb = "Success"
	} else {
		subjectVerb = "Failed"
	}
	subject = fmt.Sprintf("[BackupManager] Backup %s – %s / %s", subjectVerb, serverName, jobName)

	content := fmt.Sprintf(`
        <div style="margin-bottom:20px;">
          <span style="display:inline-block;background:%s;color:#fff;padding:6px 16px;border-radius:4px;font-size:15px;font-weight:bold;">%s</span>
        </div>
        <table width="100%%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;">
          <tr>
            <td style="padding:8px 0;color:#555;font-weight:bold;width:120px;">Server</td>
            <td style="padding:8px 0;color:#222;">%s</td>
          </tr>
          <tr>
            <td style="padding:8px 0;color:#555;font-weight:bold;">Job</td>
            <td style="padding:8px 0;color:#222;">%s</td>
          </tr>
          <tr>
            <td style="padding:8px 0;color:#555;font-weight:bold;">Status</td>
            <td style="padding:8px 0;color:#222;">%s</td>
          </tr>%s
        </table>`, color, label, serverName, jobName, status, errorSection)

	body = emailHTML(subject, "Backup Notification", content)
	return subject, body
}

// FormatHealthEmail returns subject and HTML body for a health alert.
func FormatHealthEmail(serverName, checkType, status, message string) (subject, body string) {
	color, label := emailStatusStyle(status)

	var detailSection string
	if message != "" {
		detailSection = fmt.Sprintf(`
          <tr>
            <td style="padding:8px 0;color:#555;font-weight:bold;">Detail</td>
            <td style="padding:8px 0;color:#222;">%s</td>
          </tr>`, message)
	}

	subject = fmt.Sprintf("[BackupManager] Health Alert – %s / %s", serverName, checkType)

	content := fmt.Sprintf(`
        <div style="margin-bottom:20px;">
          <span style="display:inline-block;background:%s;color:#fff;padding:6px 16px;border-radius:4px;font-size:15px;font-weight:bold;">%s</span>
        </div>
        <table width="100%%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;">
          <tr>
            <td style="padding:8px 0;color:#555;font-weight:bold;width:120px;">Server</td>
            <td style="padding:8px 0;color:#222;">%s</td>
          </tr>
          <tr>
            <td style="padding:8px 0;color:#555;font-weight:bold;">Check</td>
            <td style="padding:8px 0;color:#222;">%s</td>
          </tr>
          <tr>
            <td style="padding:8px 0;color:#555;font-weight:bold;">Status</td>
            <td style="padding:8px 0;color:#222;">%s</td>
          </tr>%s
        </table>`, color, label, serverName, checkType, status, detailSection)

	body = emailHTML(subject, "Health Alert", content)
	return subject, body
}
