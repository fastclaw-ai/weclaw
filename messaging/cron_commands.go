package messaging

import (
	"fmt"
	"strings"
	"time"
)

const cronUsage = `Usage:
  /cron list
  /cron add "<name>" <schedule> "<message>"
  /cron delete <id>
  /cron enable <id>
  /cron disable <id>

Schedule formats:
  every:5m                    — run every 5 minutes
  every:24h                   — run every 24 hours
  at:2026-04-01T09:00:00      — one-shot at specific time
  */30 * * * *                — cron expression (every 30 min)
  0 9 * * 1-5                 — cron expression (weekdays 9am)`

// HandleCronCommand processes /cron subcommands.
// userID is recorded on newly created jobs so results go back to the originating user.
func HandleCronCommand(store *CronStore, text, userID string) string {
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return cronUsage
	}
	sub := strings.ToLower(parts[1])

	switch sub {
	case "list":
		return cronList(store)
	case "add":
		return cronAdd(store, text, userID)
	case "delete", "del", "rm":
		if len(parts) < 3 {
			return "Usage: /cron delete <id>"
		}
		return cronDelete(store, parts[2])
	case "enable":
		if len(parts) < 3 {
			return "Usage: /cron enable <id>"
		}
		return cronSetEnabled(store, parts[2], true)
	case "disable":
		if len(parts) < 3 {
			return "Usage: /cron disable <id>"
		}
		return cronSetEnabled(store, parts[2], false)
	default:
		return cronUsage
	}
}

func cronList(store *CronStore) string {
	jobs := store.List()
	if len(jobs) == 0 {
		return "No cron jobs configured."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Cron Jobs (%d)\n\n", len(jobs)))
	for _, j := range jobs {
		status := "enabled"
		if !j.Enabled {
			status = "disabled"
		}
		next := "—"
		if !j.State.NextRunAt.IsZero() && j.Enabled {
			next = j.State.NextRunAt.Format("01/02 15:04")
		}
		lastRun := "never"
		if !j.State.LastRunAt.IsZero() {
			lastRun = j.State.LastRunAt.Format("01/02 15:04")
		}
		sb.WriteString(fmt.Sprintf("[%s] %s (%s)\n", j.ID, j.Name, status))
		sb.WriteString(fmt.Sprintf("  Schedule: %s\n", j.Schedule))
		sb.WriteString(fmt.Sprintf("  Message: %s\n", truncateStr(j.Message, 60)))
		sb.WriteString(fmt.Sprintf("  Next: %s | Last: %s | Runs: %d\n\n", next, lastRun, j.State.RunCount))
	}
	return sb.String()
}

// cronAdd parses: /cron add "name" schedule "message"
func cronAdd(store *CronStore, text, userID string) string {
	name, schedule, message, err := parseCronAddArgs(text)
	if err != nil {
		return fmt.Sprintf("Error: %v\n\n%s", err, cronUsage)
	}

	job := CronJob{
		Name:     name,
		Enabled:  true,
		Schedule: schedule,
		Message:  message,
		UserID:   userID,
	}

	if strings.HasPrefix(schedule, "at:") {
		job.State.DeleteAfter = true
	}

	parser := NewCronScheduler(nil, nil, "", nil)
	next, err := parser.ComputeNextRun(schedule, time.Now())
	if err != nil {
		return fmt.Sprintf("Invalid schedule %q: %v", schedule, err)
	}
	job.State.NextRunAt = next

	if err := store.Add(job); err != nil {
		return fmt.Sprintf("Failed to add job: %v", err)
	}
	return fmt.Sprintf("Job %s added (next run: %s)", name, next.Format("01/02 15:04"))
}

func cronDelete(store *CronStore, id string) string {
	if err := store.Delete(id); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Job %s deleted.", id)
}

func cronSetEnabled(store *CronStore, id string, enabled bool) string {
	if err := store.SetEnabled(id, enabled); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	action := "enabled"
	if !enabled {
		action = "disabled"
	}
	return fmt.Sprintf("Job %s %s.", id, action)
}

// parseCronAddArgs parses: /cron add "name" schedule "message"
func parseCronAddArgs(text string) (name, schedule, message string, err error) {
	after := strings.TrimSpace(text[len("/cron add"):])

	name, after, err = extractQuoted(after)
	if err != nil {
		return "", "", "", fmt.Errorf("name must be quoted: %w", err)
	}

	after = strings.TrimSpace(after)
	if after == "" {
		return "", "", "", fmt.Errorf("missing schedule and message")
	}

	lastQuote := strings.LastIndex(after, `"`)
	if lastQuote <= 0 {
		return "", "", "", fmt.Errorf("message must be quoted")
	}
	secondLastQuote := strings.LastIndex(after[:lastQuote], `"`)
	if secondLastQuote < 0 {
		return "", "", "", fmt.Errorf("message must be quoted")
	}

	schedule = strings.TrimSpace(after[:secondLastQuote])
	message = after[secondLastQuote+1 : lastQuote]

	if schedule == "" {
		return "", "", "", fmt.Errorf("missing schedule")
	}
	if message == "" {
		return "", "", "", fmt.Errorf("missing message")
	}

	return name, schedule, message, nil
}

func extractQuoted(s string) (quoted, rest string, err error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 || s[0] != '"' {
		return "", "", fmt.Errorf("expected opening quote")
	}
	end := strings.Index(s[1:], `"`)
	if end < 0 {
		return "", "", fmt.Errorf("missing closing quote")
	}
	return s[1 : end+1], s[end+2:], nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
