package incidents

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/zanetworker/code-heatmap/internal/types"
)

// Store manages incident data
type Store struct {
	path      string
	incidents []types.Incident
}

// NewStore creates a store backed by a JSON file
func NewStore(path string) (*Store, error) {
	s := &Store{path: path}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.incidents = []types.Incident{}
			return s, nil
		}
		return nil, fmt.Errorf("read incidents: %w", err)
	}

	if err := json.Unmarshal(data, &s.incidents); err != nil {
		return nil, fmt.Errorf("parse incidents: %w", err)
	}

	return s, nil
}

// Add records a new incident
func (s *Store) Add(file, severity, description string, date time.Time) (*types.Incident, error) {
	validSeverities := map[string]bool{
		"critical": true, "high": true, "medium": true, "low": true,
	}
	if !validSeverities[severity] {
		return nil, fmt.Errorf("invalid severity %q: must be critical, high, medium, or low", severity)
	}

	id := fmt.Sprintf("INC-%s-%03d", date.Format("2006"), len(s.incidents)+1)

	incident := types.Incident{
		ID:          id,
		File:        file,
		Date:        date,
		Severity:    severity,
		Description: description,
	}

	s.incidents = append(s.incidents, incident)

	if err := s.save(); err != nil {
		return nil, err
	}

	return &incident, nil
}

// ForFile returns incidents for a specific file
func (s *Store) ForFile(file string) []types.Incident {
	var result []types.Incident
	for _, inc := range s.incidents {
		if inc.File == file {
			result = append(result, inc)
		}
	}
	return result
}

// All returns all incidents
func (s *Store) All() []types.Incident {
	return s.incidents
}

// Count returns total incident count
func (s *Store) Count() int {
	return len(s.incidents)
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.incidents, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal incidents: %w", err)
	}

	return os.WriteFile(s.path, data, 0644)
}
