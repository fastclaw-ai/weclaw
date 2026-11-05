package messaging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// CronJob represents a scheduled job.
type CronJob struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Enabled  bool     `json:"enabled"`
	Schedule string   `json:"schedule"` // cron expr "*/30 * * * *", interval "every:5m", or one-shot "at:2026-04-01T09:00:00"
	Message  string   `json:"message"`
	UserID   string   `json:"user_id,omitempty"` // target user (default: first account owner)
	Agent    string   `json:"agent,omitempty"`    // target agent (default: default agent)
	State    JobState `json:"state"`
}

// JobState holds runtime state for a job.
type JobState struct {
	NextRunAt   time.Time `json:"next_run_at"`
	LastRunAt   time.Time `json:"last_run_at,omitempty"`
	LastStatus  string    `json:"last_status,omitempty"` // "ok", "error", "skipped"
	LastError   string    `json:"last_error,omitempty"`
	RunCount    int       `json:"run_count"`
	ErrorCount  int       `json:"error_count"`
	DeleteAfter bool      `json:"delete_after,omitempty"` // auto-delete after one-shot runs
}

type cronStoreFile struct {
	Version int       `json:"version"`
	Jobs    []CronJob `json:"jobs"`
}

// CronStore manages persistent cron job storage.
type CronStore struct {
	mu   sync.Mutex
	path string
	jobs []CronJob
}

// NewCronStore creates a store backed by a JSON file.
func NewCronStore(path string) *CronStore {
	return &CronStore{path: path}
}

// DefaultCronStorePath returns the default cron store path.
func DefaultCronStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".weclaw", "cron", "jobs.json"), nil
}

// Load reads jobs from disk.
func (s *CronStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.jobs = nil
			return nil
		}
		return fmt.Errorf("read cron store: %w", err)
	}

	var file cronStoreFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse cron store: %w", err)
	}
	s.jobs = file.Jobs
	return nil
}

// Save writes jobs to disk.
func (s *CronStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *CronStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create cron dir: %w", err)
	}

	file := cronStoreFile{Version: 1, Jobs: s.jobs}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cron store: %w", err)
	}
	return os.WriteFile(s.path, data, 0o600)
}

// List returns all jobs.
func (s *CronStore) List() []CronJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CronJob, len(s.jobs))
	copy(out, s.jobs)
	return out
}

// Add adds a new job and persists.
func (s *CronStore) Add(job CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job.ID == "" {
		job.ID = uuid.New().String()[:8]
	}
	s.jobs = append(s.jobs, job)
	return s.saveLocked()
}

// Delete removes a job by ID and persists.
func (s *CronStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, j := range s.jobs {
		if j.ID == id {
			s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
			return s.saveLocked()
		}
	}
	return fmt.Errorf("job %q not found", id)
}

// SetEnabled enables or disables a job by ID.
func (s *CronStore) SetEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.jobs {
		if s.jobs[i].ID == id {
			s.jobs[i].Enabled = enabled
			return s.saveLocked()
		}
	}
	return fmt.Errorf("job %q not found", id)
}

// UpdateState updates the runtime state for a job.
func (s *CronStore) UpdateState(id string, state JobState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.jobs {
		if s.jobs[i].ID == id {
			s.jobs[i].State = state
			_ = s.saveLocked()
			return
		}
	}
}

// Get returns a job by ID.
func (s *CronStore) Get(id string) (CronJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, j := range s.jobs {
		if j.ID == id {
			return j, true
		}
	}
	return CronJob{}, false
}
