package messaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCronStore_AddListDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")
	store := NewCronStore(path)

	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 0 {
		t.Fatal("expected empty store")
	}

	job := CronJob{Name: "test", Enabled: true, Schedule: "every:5m", Message: "hello"}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}

	jobs := store.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Name != "test" {
		t.Fatalf("expected name 'test', got %q", jobs[0].Name)
	}
	if jobs[0].ID == "" {
		t.Fatal("expected auto-generated ID")
	}

	// Verify persistence
	store2 := NewCronStore(path)
	if err := store2.Load(); err != nil {
		t.Fatal(err)
	}
	if len(store2.List()) != 1 {
		t.Fatal("expected 1 job after reload")
	}

	// Delete
	if err := store.Delete(jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 0 {
		t.Fatal("expected 0 jobs after delete")
	}
}

func TestCronStore_EnableDisable(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	job := CronJob{Name: "toggle", Enabled: true, Schedule: "every:1h", Message: "test"}
	_ = store.Add(job)
	id := store.List()[0].ID

	_ = store.SetEnabled(id, false)
	j, ok := store.Get(id)
	if !ok {
		t.Fatal("job not found")
	}
	if j.Enabled {
		t.Fatal("expected disabled")
	}

	_ = store.SetEnabled(id, true)
	j, _ = store.Get(id)
	if !j.Enabled {
		t.Fatal("expected enabled")
	}
}

func TestCronStore_DeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	if err := store.Delete("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestCronStore_FileNotExist(t *testing.T) {
	store := NewCronStore("/tmp/weclaw-test-nonexistent/jobs.json")
	if err := store.Load(); err != nil {
		t.Fatalf("Load should succeed for missing file, got: %v", err)
	}
	if len(store.List()) != 0 {
		t.Fatal("expected empty list")
	}
}

func TestComputeNextRun_Every(t *testing.T) {
	s := NewCronScheduler(nil, nil, "", nil)
	now := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)

	next, err := s.ComputeNextRun("every:5m", now)
	if err != nil {
		t.Fatal(err)
	}
	expected := now.Add(5 * time.Minute)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestComputeNextRun_At(t *testing.T) {
	s := NewCronScheduler(nil, nil, "", nil)
	now := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)

	next, err := s.ComputeNextRun("at:2026-04-01T10:00:00Z", now)
	if err != nil {
		t.Fatal(err)
	}
	expected := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}

	// Past time should error
	_, err = s.ComputeNextRun("at:2020-01-01T00:00:00Z", now)
	if err == nil {
		t.Fatal("expected error for past time")
	}
}

func TestComputeNextRun_CronExpr(t *testing.T) {
	s := NewCronScheduler(nil, nil, "", nil)
	now := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)

	next, err := s.ComputeNextRun("*/30 * * * *", now)
	if err != nil {
		t.Fatal(err)
	}
	expected := time.Date(2026, 4, 1, 9, 30, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestComputeNextRun_Invalid(t *testing.T) {
	s := NewCronScheduler(nil, nil, "", nil)
	now := time.Now()

	tests := []string{"bad", "every:-1m", "at:not-a-date", "every:0s"}
	for _, sched := range tests {
		_, err := s.ComputeNextRun(sched, now)
		if err == nil {
			t.Errorf("expected error for schedule %q", sched)
		}
	}
}

func TestParseCronAddArgs(t *testing.T) {
	tests := []struct {
		input   string
		name    string
		sched   string
		msg     string
		wantErr bool
	}{
		{
			input: `/cron add "standup" every:24h "summarize yesterday"`,
			name:  "standup", sched: "every:24h", msg: "summarize yesterday",
		},
		{
			input: `/cron add "check" */30 * * * * "check emails"`,
			name:  "check", sched: "*/30 * * * *", msg: "check emails",
		},
		{
			input:   `/cron add missing quotes`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		name, sched, msg, err := parseCronAddArgs(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseCronAddArgs(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseCronAddArgs(%q): %v", tt.input, err)
			continue
		}
		if name != tt.name || sched != tt.sched || msg != tt.msg {
			t.Errorf("parseCronAddArgs(%q) = (%q,%q,%q), want (%q,%q,%q)", tt.input, name, sched, msg, tt.name, tt.sched, tt.msg)
		}
	}
}

func TestHandleCronCommand_List_Empty(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	reply := HandleCronCommand(store, "/cron list", "user1")
	if reply != "No cron jobs configured." {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestHandleCronCommand_AddAndList(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	reply := HandleCronCommand(store, `/cron add "test" every:1h "hello world"`, "user1")
	if !strings.Contains(reply, "added") {
		t.Fatalf("expected 'added' in reply: %q", reply)
	}

	// Verify userID was recorded on the job
	jobs := store.List()
	if len(jobs) != 1 || jobs[0].UserID != "user1" {
		t.Fatalf("expected UserID 'user1', got %q", jobs[0].UserID)
	}

	reply = HandleCronCommand(store, "/cron list", "user1")
	if !strings.Contains(reply, "test") {
		t.Fatalf("expected 'test' in list: %q", reply)
	}
}

func TestHandleCronCommand_DeleteEnableDisable(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	HandleCronCommand(store, `/cron add "test" every:1h "hello"`, "user1")
	id := store.List()[0].ID

	reply := HandleCronCommand(store, "/cron disable "+id, "user1")
	if !strings.Contains(reply, "disabled") {
		t.Fatalf("unexpected: %q", reply)
	}

	reply = HandleCronCommand(store, "/cron enable "+id, "user1")
	if !strings.Contains(reply, "enabled") {
		t.Fatalf("unexpected: %q", reply)
	}

	reply = HandleCronCommand(store, "/cron delete "+id, "user1")
	if !strings.Contains(reply, "deleted") {
		t.Fatalf("unexpected: %q", reply)
	}
}

func TestHandleCronCommand_Usage(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	reply := HandleCronCommand(store, "/cron", "user1")
	if !strings.Contains(reply, "Usage") {
		t.Fatalf("expected usage: %q", reply)
	}
}

func TestCronStore_FileFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")
	store := NewCronStore(path)
	_ = store.Load()

	_ = store.Add(CronJob{Name: "a", Enabled: true, Schedule: "every:1h", Message: "test"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version": 1`) {
		t.Fatalf("file should contain version: %s", string(data))
	}
}

func TestCronStore_UpdateState(t *testing.T) {
	dir := t.TempDir()
	store := NewCronStore(filepath.Join(dir, "jobs.json"))
	_ = store.Load()

	_ = store.Add(CronJob{Name: "stateful", Enabled: true, Schedule: "every:1h", Message: "test"})
	id := store.List()[0].ID

	now := time.Now()
	store.UpdateState(id, JobState{
		LastRunAt:  now,
		LastStatus: "ok",
		RunCount:   1,
	})

	j, ok := store.Get(id)
	if !ok {
		t.Fatal("job not found")
	}
	if j.State.RunCount != 1 {
		t.Fatalf("expected RunCount=1, got %d", j.State.RunCount)
	}
	if j.State.LastStatus != "ok" {
		t.Fatalf("expected LastStatus=ok, got %q", j.State.LastStatus)
	}
}
