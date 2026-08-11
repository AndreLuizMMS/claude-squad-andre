package session

import (
	"claude-squad/config"
	"claude-squad/log"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// InstanceData represents the serializable data of an Instance. Fields written
// by older versions that no longer exist (branch, mode, worktree) are simply
// ignored on read.
type InstanceData struct {
	Title     string    `json:"title"`
	Path      string    `json:"path"`
	Status    Status    `json:"status"`
	Height    int       `json:"height"`
	Width     int       `json:"width"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	AutoYes   bool      `json:"auto_yes"`

	// SessionID is the stable identity of the session. Absent in older records,
	// which fall back to the title.
	SessionID string `json:"session_id"`

	Program   string        `json:"program"`
	DiffStats DiffStatsData `json:"diff_stats"`
}

// DiffStatsData represents the serializable data of a DiffStats
type DiffStatsData struct {
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Content string `json:"content"`
}

// Storage handles saving and loading instances using the state interface
type Storage struct {
	state config.InstanceStorage
}

// NewStorage creates a new storage instance
func NewStorage(state config.InstanceStorage) (*Storage, error) {
	return &Storage{
		state: state,
	}, nil
}

// SaveInstances saves the list of instances to disk
func (s *Storage) SaveInstances(instances []*Instance) error {
	// Convert instances to InstanceData
	data := make([]InstanceData, 0)
	for _, instance := range instances {
		if instance.Started() {
			data = append(data, instance.ToInstanceData())
		}
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal instances: %w", err)
	}

	return s.state.SaveInstances(jsonData)
}

// LoadInstances loads the list of instances from disk
func (s *Storage) LoadInstances() ([]*Instance, error) {
	jsonData := s.state.GetInstances()

	var instancesData []InstanceData
	if err := json.Unmarshal(jsonData, &instancesData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal instances: %w", err)
	}

	// A record we cannot revive must not take the others with it: the session
	// whose terminal refused to come back is dropped, the rest of the list opens
	// normally, and the failure is reported alongside it.
	instances := make([]*Instance, 0, len(instancesData))
	var failures []string
	for _, data := range instancesData {
		instance, err := FromInstanceData(data)
		if err != nil {
			log.ErrorLog.Printf("failed to restore session %s: %v", data.Title, err)
			failures = append(failures, data.Title)
			continue
		}
		instances = append(instances, instance)
	}

	if len(failures) > 0 {
		return instances, fmt.Errorf("could not restore %d session(s): %s",
			len(failures), strings.Join(failures, ", "))
	}
	return instances, nil
}

// DeleteInstance removes an instance from storage, keyed by session identity
// (SessionID, falling back to Title for records written by older versions).
//
// It works on the stored records directly instead of loading them: loading
// revives every session it reads, which would restart the terminals of all the
// other sessions just to drop one row.
func (s *Storage) DeleteInstance(id string) error {
	var stored []InstanceData
	if err := json.Unmarshal(s.state.GetInstances(), &stored); err != nil {
		return fmt.Errorf("failed to unmarshal instances: %w", err)
	}

	kept := make([]InstanceData, 0, len(stored))
	found := false
	for _, data := range stored {
		if storedID(data) == id {
			found = true
			continue
		}
		kept = append(kept, data)
	}

	if !found {
		return fmt.Errorf("instance not found: %s", id)
	}

	jsonData, err := json.Marshal(kept)
	if err != nil {
		return fmt.Errorf("failed to marshal instances: %w", err)
	}
	return s.state.SaveInstances(jsonData)
}

// storedID is the identity of a stored record, mirroring Instance.ID().
func storedID(data InstanceData) string {
	if data.SessionID != "" {
		return data.SessionID
	}
	return data.Title
}

// DeleteAllInstances removes all stored instances
func (s *Storage) DeleteAllInstances() error {
	return s.state.DeleteAllInstances()
}
