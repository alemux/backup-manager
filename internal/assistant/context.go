// internal/assistant/context.go
package assistant

import (
	"fmt"
	"strings"

	"github.com/backupmanager/backupmanager/internal/database"
)

// BuildContext creates the system context for the LLM.
// Budget: ~4000 tokens total (4 chars ≈ 1 token)
// - Server config summary: ~500 tokens
// - Relevant logs: ~2000 tokens (keyword-matched from user question)
// - Health/backup status: ~500 tokens
// - Conversation history: ~1000 tokens (oldest dropped first)
func BuildContext(db *database.Database, userMessage string, conversationHistory []Message) string {
	var sb strings.Builder

	sb.WriteString("You are an intelligent backup system assistant. You help users understand their server backup status, troubleshoot issues, and optimize their backup strategies. Be concise and practical.\n\n")

	// Server config summary (~500 tokens)
	sb.WriteString("## Server & Backup Configuration\n")
	sb.WriteString(buildServerSummary(db))
	sb.WriteString("\n")

	// Health/backup status (~500 tokens)
	sb.WriteString("## Current Status\n")
	sb.WriteString(buildStatusSummary(db))
	sb.WriteString("\n")

	// Relevant logs (~2000 tokens)
	keywords := extractKeywords(userMessage)
	if len(keywords) > 0 {
		sb.WriteString("## Relevant Logs\n")
		sb.WriteString(findRelevantLogs(db, keywords, 20))
		sb.WriteString("\n")
	}

	result := sb.String()
	// Keep within ~16000 chars (4000 tokens)
	if len(result) > 16000 {
		result = result[:16000]
	}
	return result
}

// extractKeywords extracts meaningful keywords from the user message.
func extractKeywords(message string) []string {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "it": true,
		"in": true, "on": true, "at": true, "to": true, "for": true,
		"of": true, "and": true, "or": true, "but": true, "with": true,
		"my": true, "me": true, "i": true, "why": true, "how": true,
		"what": true, "when": true, "did": true, "do": true, "does": true,
		"can": true, "could": true, "would": true, "should": true, "will": true,
		"was": true, "were": true, "are": true, "has": true, "have": true,
		"had": true, "be": true, "been": true, "not": true, "no": true,
		"any": true, "all": true, "last": true, "get": true, "show": true,
		"tell": true, "give": true, "list": true, "check": true,
	}

	lower := strings.ToLower(message)
	// Remove punctuation
	for _, ch := range "?!.,;:\"'()[]{}/" {
		lower = strings.ReplaceAll(lower, string(ch), " ")
	}

	words := strings.Fields(lower)
	seen := map[string]bool{}
	var keywords []string
	for _, w := range words {
		if len(w) < 3 {
			continue
		}
		if stopWords[w] {
			continue
		}
		if !seen[w] {
			seen[w] = true
			keywords = append(keywords, w)
		}
	}
	return keywords
}

// findRelevantLogs returns backup run entries matching any of the keywords.
func findRelevantLogs(db *database.Database, keywords []string, maxEntries int) string {
	if len(keywords) == 0 {
		return ""
	}

	// Build LIKE conditions for keywords against job name and error_message
	conditions := make([]string, 0, len(keywords)*2)
	args := make([]interface{}, 0, len(keywords)*2)
	for _, kw := range keywords {
		like := "%" + strings.ToLower(kw) + "%"
		conditions = append(conditions, "LOWER(bj.name) LIKE ?", "LOWER(COALESCE(br.error_message,'')) LIKE ?")
		args = append(args, like, like)
	}

	query := fmt.Sprintf(`
		SELECT br.id, br.status, COALESCE(bj.name,''), COALESCE(br.error_message,''), br.created_at
		FROM backup_runs br
		LEFT JOIN backup_jobs bj ON br.job_id = bj.id
		WHERE %s
		ORDER BY br.created_at DESC
		LIMIT ?`, strings.Join(conditions, " OR "))

	args = append(args, maxEntries)

	rows, err := db.DB().Query(query, args...)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var sb strings.Builder
	count := 0
	for rows.Next() {
		var runID, status, jobName, errMsg, createdAt string
		if err := rows.Scan(&runID, &status, &jobName, &errMsg, &createdAt); err != nil {
			continue
		}
		line := fmt.Sprintf("[%s] run=%s job=%s status=%s", createdAt, runID, jobName, status)
		if errMsg != "" {
			line += ": " + errMsg
		}
		sb.WriteString(line + "\n")
		count++
	}
	if count == 0 {
		return "No relevant log entries found.\n"
	}
	return sb.String()
}

// buildServerSummary returns a concise summary of configured servers and jobs.
func buildServerSummary(db *database.Database) string {
	var sb strings.Builder

	rows, err := db.DB().Query("SELECT name, host, type, connection_type FROM servers ORDER BY name LIMIT 20")
	if err != nil {
		return "Unable to retrieve server information.\n"
	}
	defer rows.Close()

	serverCount := 0
	for rows.Next() {
		var name, host, typ, connType string
		if err := rows.Scan(&name, &host, &typ, &connType); err != nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("- Server: %s (%s) type=%s connection=%s\n", name, host, typ, connType))
		serverCount++
	}
	if serverCount == 0 {
		sb.WriteString("No servers configured.\n")
	}

	// Jobs summary
	jobRows, err := db.DB().Query(`
		SELECT bj.name, bj.schedule, bj.enabled, s.name
		FROM backup_jobs bj
		LEFT JOIN servers s ON bj.server_id = s.id
		ORDER BY bj.name LIMIT 20`)
	if err == nil {
		defer jobRows.Close()
		for jobRows.Next() {
			var jobName, schedule, serverName string
			var enabled int
			if err := jobRows.Scan(&jobName, &schedule, &enabled, &serverName); err != nil {
				continue
			}
			status := "enabled"
			if enabled == 0 {
				status = "disabled"
			}
			sb.WriteString(fmt.Sprintf("- Job: %s (server=%s, schedule=%s, %s)\n", jobName, serverName, schedule, status))
		}
	}

	return sb.String()
}

// buildStatusSummary returns recent backup run status and health checks.
func buildStatusSummary(db *database.Database) string {
	var sb strings.Builder

	// Recent backup runs
	rows, err := db.DB().Query(`
		SELECT br.id, br.status, br.started_at, br.finished_at, bj.name
		FROM backup_runs br
		LEFT JOIN backup_jobs bj ON br.job_id = bj.id
		ORDER BY br.created_at DESC
		LIMIT 10`)
	if err == nil {
		defer rows.Close()
		sb.WriteString("Recent backup runs:\n")
		for rows.Next() {
			var id, status, jobName string
			var startedAt, finishedAt *string
			if err := rows.Scan(&id, &status, &startedAt, &finishedAt, &jobName); err != nil {
				continue
			}
			start := "N/A"
			if startedAt != nil {
				start = *startedAt
			}
			sb.WriteString(fmt.Sprintf("  run #%s: job=%s status=%s started=%s\n", id, jobName, status, start))
		}
	}

	// Health checks summary
	hRows, err := db.DB().Query(`
		SELECT s.name, hc.check_type, hc.status, hc.message, hc.created_at
		FROM health_checks hc
		JOIN servers s ON hc.server_id = s.id
		WHERE hc.created_at >= datetime('now', '-1 day')
		ORDER BY hc.created_at DESC
		LIMIT 20`)
	if err == nil {
		defer hRows.Close()
		sb.WriteString("Recent health checks (last 24h):\n")
		count := 0
		for hRows.Next() {
			var serverName, checkType, status, message, createdAt string
			if err := hRows.Scan(&serverName, &checkType, &status, &message, &createdAt); err != nil {
				continue
			}
			sb.WriteString(fmt.Sprintf("  %s - %s: %s (%s)\n", serverName, checkType, status, message))
			count++
		}
		if count == 0 {
			sb.WriteString("  No recent health checks.\n")
		}
	}

	return sb.String()
}
