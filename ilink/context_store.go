package ilink

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var contextStoreMu sync.Mutex

type contextTokenData struct {
	Tokens map[string]string `json:"tokens"`
}

func contextTokenPath(botID string) (string, error) {
	dir, err := AccountsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NormalizeAccountID(botID)+".contexts.json"), nil
}

// SaveContextToken stores the latest iLink context token for a user.
func SaveContextToken(botID, userID, token string) error {
	if botID == "" || userID == "" || token == "" {
		return nil
	}

	contextStoreMu.Lock()
	defer contextStoreMu.Unlock()

	path, err := contextTokenPath(botID)
	if err != nil {
		return err
	}

	data := contextTokenData{Tokens: map[string]string{}}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &data)
	}
	if data.Tokens == nil {
		data.Tokens = map[string]string{}
	}
	data.Tokens[userID] = token

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create context token dir: %w", err)
	}

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal context tokens: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write context tokens: %w", err)
	}
	return nil
}

// LoadContextToken returns the latest cached iLink context token for a user.
func LoadContextToken(botID, userID string) (string, error) {
	if botID == "" || userID == "" {
		return "", nil
	}

	contextStoreMu.Lock()
	defer contextStoreMu.Unlock()

	path, err := contextTokenPath(botID)
	if err != nil {
		return "", err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read context tokens: %w", err)
	}

	var data contextTokenData
	if err := json.Unmarshal(raw, &data); err != nil {
		return "", fmt.Errorf("parse context tokens: %w", err)
	}
	return data.Tokens[userID], nil
}

// ClearContextTokens removes cached iLink context tokens for a bot account.
func ClearContextTokens(botID string) error {
	if botID == "" {
		return nil
	}

	contextStoreMu.Lock()
	defer contextStoreMu.Unlock()

	path, err := contextTokenPath(botID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove context tokens: %w", err)
	}
	return nil
}
