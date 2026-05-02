package messaging

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fastclaw-ai/weclaw/ilink"
)

type pendingNotification struct {
	Time string `json:"time"`
	To   string `json:"to"`
	Text string `json:"text"`
}

func notificationQueuePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "weclaw-pending-notifications.jsonl")
	}
	return filepath.Join(home, ".weclaw", "pending-notifications.jsonl")
}

// EnqueuePendingNotification stores a notification that could not be sent proactively.
func EnqueuePendingNotification(toUserID, text string) error {
	toUserID = strings.TrimSpace(toUserID)
	text = strings.TrimSpace(text)
	if toUserID == "" || text == "" {
		return nil
	}
	path := notificationQueuePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	item := pendingNotification{Time: time.Now().Format(time.RFC3339), To: toUserID, Text: text}
	b, err := json.Marshal(item)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// FlushPendingNotifications sends queued notifications for a user using a fresh context token.
func FlushPendingNotifications(ctx context.Context, client *ilink.Client, toUserID, contextToken string) {
	if toUserID == "" || contextToken == "" {
		return
	}
	path := notificationQueuePath()
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	var keep []pendingNotification
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item pendingNotification
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		if item.To != toUserID {
			keep = append(keep, item)
			continue
		}
		text := item.Text
		if !strings.Contains(text, "补发") {
			text = fmt.Sprintf("补发通知 %s\n%s", time.Now().Format("01-02 15:04:05"), text)
		}
		if err := SendTextReply(ctx, client, toUserID, text, contextToken, ""); err != nil {
			log.Printf("[notify_queue] flush failed for %s: %v", toUserID, err)
			keep = append(keep, item)
		} else {
			log.Printf("[notify_queue] flushed pending notification to %s", toUserID)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[notify_queue] scan failed: %v", err)
		return
	}

	if len(keep) == 0 {
		_ = os.Remove(path)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("[notify_queue] rewrite failed: %v", err)
		return
	}
	defer out.Close()
	for _, item := range keep {
		b, _ := json.Marshal(item)
		_, _ = out.Write(append(b, '\n'))
	}
}
