// internal/notification/email_test.go
package notification

import (
	"bufio"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---------- FormatBackupEmail ----------

func TestFormatBackupEmail_Success(t *testing.T) {
	subject, body := FormatBackupEmail("web-01", "daily-db", "success", "")

	if !strings.Contains(subject, "Success") {
		t.Errorf("expected subject to contain 'Success', got: %s", subject)
	}
	if !strings.Contains(subject, "web-01") {
		t.Errorf("expected subject to contain server name, got: %s", subject)
	}
	if !strings.Contains(subject, "daily-db") {
		t.Errorf("expected subject to contain job name, got: %s", subject)
	}

	// Green indicator
	if !strings.Contains(body, "#28a745") {
		t.Error("expected body to contain green color indicator for success")
	}
	if !strings.Contains(body, "web-01") {
		t.Error("expected body to contain server name")
	}
	if !strings.Contains(body, "daily-db") {
		t.Error("expected body to contain job name")
	}
	if !strings.Contains(body, "BackupManager") {
		t.Error("expected body to contain BackupManager branding")
	}
}

func TestFormatBackupEmail_Failure(t *testing.T) {
	subject, body := FormatBackupEmail("web-01", "daily-db", "failed", "connection refused")

	if !strings.Contains(subject, "Failed") {
		t.Errorf("expected subject to contain 'Failed', got: %s", subject)
	}

	// Red indicator
	if !strings.Contains(body, "#dc3545") {
		t.Error("expected body to contain red color indicator for failure")
	}
	if !strings.Contains(body, "connection refused") {
		t.Error("expected body to contain error message")
	}
}

func TestFormatBackupEmail_NoErrorSection(t *testing.T) {
	_, body := FormatBackupEmail("srv", "job", "success", "")
	// No error row should appear when errorMsg is empty.
	if strings.Contains(body, "Error") {
		t.Error("expected no Error row when errorMsg is empty")
	}
}

// ---------- FormatHealthEmail ----------

func TestFormatHealthEmail(t *testing.T) {
	subject, body := FormatHealthEmail("app-server", "disk", "critical", "disk at 95%")

	if !strings.Contains(subject, "app-server") {
		t.Errorf("expected subject to contain server name, got: %s", subject)
	}
	if !strings.Contains(subject, "disk") {
		t.Errorf("expected subject to contain check type, got: %s", subject)
	}

	if !strings.Contains(body, "app-server") {
		t.Error("expected body to contain server name")
	}
	if !strings.Contains(body, "disk") {
		t.Error("expected body to contain check type")
	}
	if !strings.Contains(body, "disk at 95%") {
		t.Error("expected body to contain detail message")
	}
	// Red indicator for critical
	if !strings.Contains(body, "#dc3545") {
		t.Error("expected red color indicator for critical status")
	}
	if !strings.Contains(body, "BackupManager") {
		t.Error("expected body to contain BackupManager branding")
	}
}

func TestFormatHealthEmail_OK(t *testing.T) {
	_, body := FormatHealthEmail("srv", "cpu", "ok", "")
	// Green indicator for ok status.
	if !strings.Contains(body, "#28a745") {
		t.Error("expected green color indicator for ok status")
	}
}

func TestFormatHealthEmail_Warning(t *testing.T) {
	_, body := FormatHealthEmail("srv", "mem", "warning", "memory at 80%")
	// Yellow indicator for warning status.
	if !strings.Contains(body, "#ffc107") {
		t.Error("expected yellow color indicator for warning status")
	}
}

// ---------- Anti-flood ----------

func TestAntiFlood_Email(t *testing.T) {
	// Use a no-op Send that always succeeds by pointing at a real local SMTP stub.
	// We test the anti-flood logic in isolation by back-dating timestamps directly.
	notifier := NewEmailNotifier(SMTPConfig{
		Host:     "localhost",
		Port:     0, // won't be used – we don't actually dial
		Username: "",
		Password: "",
		From:     "test@example.com",
	})

	key := "server1:backup"

	// Seed the lastAlert map to simulate a recent send.
	notifier.mu.Lock()
	notifier.lastAlert[key] = time.Now()
	notifier.mu.Unlock()

	// Should be suppressed within cooldown.
	sent, err := notifier.SendWithAntiFlood(nil, key, "subj", "body", 30*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sent {
		t.Error("expected message to be suppressed within cooldown")
	}

	// Back-date past cooldown – should NOT be suppressed.
	notifier.mu.Lock()
	notifier.lastAlert[key] = time.Now().Add(-31 * time.Minute)
	notifier.mu.Unlock()

	// The actual Send will fail (no real SMTP), but we only care that
	// the anti-flood gate let it through (sent=false due to error, but
	// the gate itself passed). We distinguish by checking that the error
	// is a dial/connect error, not a suppression (err must be non-nil and sent==false).
	sent2, err2 := notifier.SendWithAntiFlood(nil, key, "subj", "body", 30*time.Minute)
	// We expect an error because there's no SMTP server, but the gate passed.
	if err2 == nil {
		t.Error("expected connection error when no SMTP server is available")
	}
	// sent2 must be false because Send itself errored.
	if sent2 {
		t.Error("sent must be false when Send returns an error")
	}
	// After a failed send the timestamp should be rolled back.
	notifier.mu.Lock()
	ts := notifier.lastAlert[key]
	notifier.mu.Unlock()
	if time.Since(ts) < 30*time.Minute {
		t.Error("expected timestamp to be rolled back after failed Send")
	}
}

func TestAntiFlood_Email_DifferentKeyAllowed(t *testing.T) {
	notifier := NewEmailNotifier(SMTPConfig{From: "test@example.com"})

	key1 := "server1:backup"
	key2 := "server1:disk"

	// Seed key1 as recently sent.
	notifier.mu.Lock()
	notifier.lastAlert[key1] = time.Now()
	notifier.mu.Unlock()

	// key2 has never been sent – gate should open (Send will fail on dial, but gate passed).
	_, err := notifier.SendWithAntiFlood(nil, key2, "subj", "body", 30*time.Minute)
	// An error from the dial is expected; what matters is it's not a suppression.
	// Suppression returns (false, nil); a dial error returns (false, non-nil error).
	if err == nil {
		t.Error("expected dial error for key2, not a silent suppression")
	}
}

// ---------- BuildMIMEMessage ----------

func TestBuildMIMEMessage(t *testing.T) {
	from := "sender@example.com"
	to := []string{"alice@example.com", "bob@example.com"}
	subject := "Test Subject"
	htmlBody := "<p>Hello World</p>"

	msg := buildMIMEMessage(from, to, subject, htmlBody)
	raw := string(msg)

	checks := []struct {
		desc     string
		contains string
	}{
		{"From header", "From: sender@example.com"},
		{"To header with both recipients", "alice@example.com"},
		{"To header with both recipients", "bob@example.com"},
		{"Subject header", "Subject: Test Subject"},
		{"MIME-Version header", "MIME-Version: 1.0"},
		{"Content-Type header", "Content-Type: text/html; charset=UTF-8"},
		{"HTML body", "<p>Hello World</p>"},
	}

	for _, c := range checks {
		if !strings.Contains(raw, c.contains) {
			t.Errorf("%s: expected message to contain %q", c.desc, c.contains)
		}
	}

	// Headers and body must be separated by a blank line (\r\n\r\n).
	if !strings.Contains(raw, "\r\n\r\n") {
		t.Error("expected blank line separating headers from body")
	}

	// Headers must use CRLF line endings.
	headerSection := raw[:strings.Index(raw, "\r\n\r\n")]
	if strings.Contains(headerSection, "\n") && !strings.Contains(headerSection, "\r\n") {
		t.Error("expected CRLF line endings in headers")
	}
}

// ---------- SMTPConfig / NewEmailNotifier ----------

func TestNewEmailNotifier_Defaults(t *testing.T) {
	cfg := SMTPConfig{
		Host:     "smtp.example.com",
		Port:     587,
		Username: "user@example.com",
		Password: "secret",
		From:     "noreply@example.com",
		UseTLS:   false,
	}
	n := NewEmailNotifier(cfg)
	if n == nil {
		t.Fatal("expected non-nil notifier")
	}
	if n.lastAlert == nil {
		t.Error("expected lastAlert map to be initialised")
	}
	if n.config.Host != cfg.Host {
		t.Errorf("expected host %s, got %s", cfg.Host, n.config.Host)
	}
}

// ---------- Integration-style: verify SMTP conversation ----------

// smtpStubServer starts a minimal SMTP server stub on a random local port.
// It speaks just enough of the SMTP protocol to let net/smtp complete a delivery
// without STARTTLS or AUTH. The done channel is closed once the server has
// finished handling one connection.
func smtpStubServer(t *testing.T) (host string, port int, done <-chan struct{}) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("stub SMTP listen: %v", err)
	}

	ch := make(chan struct{})

	go func() {
		defer close(ch)
		defer ln.Close()

		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		sc := bufio.NewScanner(conn)

		write := func(s string) {
			conn.Write([]byte(s + "\r\n")) //nolint:errcheck
		}

		readLine := func() string {
			if !sc.Scan() {
				return ""
			}
			return strings.TrimRight(sc.Text(), "\r\n")
		}

		// RFC 5321 greeting
		write("220 stub.local ESMTP ready")
		readLine() // EHLO / HELO
		// Advertise no extensions so client skips STARTTLS and AUTH.
		write("250 stub.local")
		cmd := readLine() // MAIL FROM or AUTH
		if strings.HasPrefix(strings.ToUpper(cmd), "AUTH") {
			write("235 2.7.0 Authentication successful")
			cmd = readLine() // MAIL FROM
		}
		_ = cmd
		write("250 2.1.0 OK")    // MAIL FROM accepted
		readLine()                // RCPT TO
		write("250 2.1.5 OK")    // RCPT accepted
		readLine()                // DATA
		write("354 Start input") // ready for body
		// Read until dot-stuffed terminator.
		for {
			line := readLine()
			if line == "." {
				break
			}
		}
		write("250 2.0.0 Message accepted")
		readLine() // QUIT
		write("221 2.0.0 Bye")
	}()

	addrStr := ln.Addr().String()
	h, portStr, _ := net.SplitHostPort(addrStr)
	p, _ := strconv.Atoi(portStr)
	return h, p, ch
}

// TestSend_PlainSMTP verifies that Send correctly connects to a plain SMTP stub
// and delivers a properly formatted message.
func TestSend_PlainSMTP(t *testing.T) {
	host, port, done := smtpStubServer(t)

	cfg := SMTPConfig{
		Host:     host,
		Port:     port,
		Username: "",
		Password: "",
		From:     "sender@example.com",
		UseTLS:   false,
	}
	n := NewEmailNotifier(cfg)

	subject, body := FormatBackupEmail("web-01", "daily-db", "success", "")
	err := n.Send([]string{"admin@example.com"}, subject, body)
	if err != nil {
		t.Errorf("Send returned unexpected error: %v", err)
	}

	// Wait for the stub to finish (ensures the goroutine exits cleanly).
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("SMTP stub did not finish in time")
	}
}

// Ensure smtp package is used (import check).
var _ = smtp.PlainAuth
