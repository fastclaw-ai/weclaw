package messaging

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fastclaw-ai/weclaw/agent"
	"github.com/fastclaw-ai/weclaw/config"
	"github.com/fastclaw-ai/weclaw/ilink"
)

const (
	heartbeatOKToken     = "HEARTBEAT_OK"
	heartbeatDedupWindow = 24 * time.Hour
	defaultHeartbeatFile = "HEARTBEAT.md"
)

// HeartbeatRunner periodically reads HEARTBEAT.md and sends it to the default agent.
type HeartbeatRunner struct {
	cfg         config.HeartbeatConfig
	client      *ilink.Client
	targetUser  string
	getAgent    func() agent.Agent
	interval    time.Duration
	location    *time.Location
	activeStart int // minutes from midnight
	activeEnd   int // minutes from midnight
	mu          sync.Mutex
	recentHash  map[string]time.Time // hash -> last seen
}

// NewHeartbeatRunner creates a heartbeat runner.
func NewHeartbeatRunner(cfg config.HeartbeatConfig, client *ilink.Client, targetUser string, getAgent func() agent.Agent) (*HeartbeatRunner, error) {
	interval := 30 * time.Minute
	if cfg.Interval != "" {
		d, err := time.ParseDuration(cfg.Interval)
		if err != nil {
			return nil, fmt.Errorf("invalid heartbeat interval %q: %w", cfg.Interval, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("heartbeat interval must be positive, got %v", d)
		}
		interval = d
	}

	loc := time.Local
	if cfg.Timezone != "" {
		l, err := time.LoadLocation(cfg.Timezone)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone %q: %w", cfg.Timezone, err)
		}
		loc = l
	}

	r := &HeartbeatRunner{
		cfg:        cfg,
		client:     client,
		targetUser: targetUser,
		getAgent:   getAgent,
		interval:   interval,
		location:   loc,
		recentHash: make(map[string]time.Time),
	}

	if cfg.ActiveHours != "" {
		start, end, err := parseActiveHours(cfg.ActiveHours)
		if err != nil {
			return nil, err
		}
		r.activeStart = start
		r.activeEnd = end
	}

	return r, nil
}

// Start runs the heartbeat loop until context is cancelled.
func (r *HeartbeatRunner) Start(ctx context.Context) {
	log.Printf("[heartbeat] runner started (interval=%s, activeHours=%s)", r.interval, r.cfg.ActiveHours)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[heartbeat] runner stopped")
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *HeartbeatRunner) tick(ctx context.Context) {
	if !r.isActiveTime() {
		return
	}

	content, err := r.readHeartbeatFile()
	if err != nil {
		return
	}
	if isEffectivelyEmpty(content) {
		return
	}

	ag := r.getAgent()
	if ag == nil {
		return
	}

	prompt := fmt.Sprintf("This is a scheduled heartbeat check. Follow the instructions below and report anything that needs attention. If everything is fine, reply with exactly: %s\n\n%s", heartbeatOKToken, content)
	log.Println("[heartbeat] running check")

	reply, err := ag.Chat(ctx, "heartbeat", prompt)
	if err != nil {
		log.Printf("[heartbeat] agent error: %v", err)
		return
	}

	reply = strings.TrimSpace(reply)
	if reply == "" || strings.EqualFold(reply, heartbeatOKToken) || strings.HasPrefix(strings.TrimSpace(strings.ToUpper(reply)), heartbeatOKToken) {
		log.Println("[heartbeat] all clear")
		return
	}

	if r.isDuplicate(reply) {
		log.Println("[heartbeat] duplicate reply suppressed")
		return
	}

	if r.client != nil && r.targetUser != "" {
		if err := SendTextReply(ctx, r.client, r.targetUser, "[Heartbeat] "+reply, "", ""); err != nil {
			log.Printf("[heartbeat] failed to send reply: %v", err)
		}
	}
}

func (r *HeartbeatRunner) isActiveTime() bool {
	if r.cfg.ActiveHours == "" {
		return true
	}
	now := time.Now().In(r.location)
	mins := now.Hour()*60 + now.Minute()
	if r.activeStart <= r.activeEnd {
		return mins >= r.activeStart && mins < r.activeEnd
	}
	// Wraps midnight (e.g. 22:00-06:00)
	return mins >= r.activeStart || mins < r.activeEnd
}

func (r *HeartbeatRunner) readHeartbeatFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".weclaw", defaultHeartbeatFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (r *HeartbeatRunner) isDuplicate(reply string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	h := fmt.Sprintf("%x", sha256.Sum256([]byte(reply)))
	if t, ok := r.recentHash[h]; ok && time.Since(t) < heartbeatDedupWindow {
		return true
	}
	r.recentHash[h] = time.Now()

	// Clean old entries
	cutoff := time.Now().Add(-heartbeatDedupWindow)
	for k, t := range r.recentHash {
		if t.Before(cutoff) {
			delete(r.recentHash, k)
		}
	}
	return false
}

func isEffectivelyEmpty(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<!--") {
			continue
		}
		return false
	}
	return true
}

func parseActiveHours(s string) (start, end int, err error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid active_hours format %q, expected HH:MM-HH:MM", s)
	}
	start, err = parseTimeOfDay(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid active_hours start: %w", err)
	}
	end, err = parseTimeOfDay(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid active_hours end: %w", err)
	}
	return start, end, nil
}

func parseTimeOfDay(s string) (int, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return 0, fmt.Errorf("invalid time %q, expected HH:MM", s)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("time %q out of range", s)
	}
	return h*60 + m, nil
}
