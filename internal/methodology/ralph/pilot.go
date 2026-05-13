package ralph

import (
	"path/filepath"
	"time"

	"github.com/ar4mirez/samuel/internal/methodology/ralph/prd"
)

// NewPilotConfig returns the defaults that match v1.
func NewPilotConfig() *prd.PilotConfig {
	return &prd.PilotConfig{
		DiscoverInterval:  prd.DefaultDiscoverInterval,
		MaxDiscoveryTasks: prd.DefaultMaxDiscoveryTasks,
	}
}

// ShouldRunDiscovery decides whether the next iteration should be a
// discovery iteration. It triggers on:
//
//   - empty queue (no pending tasks)
//   - first iteration (lastDiscoveryIter == 0)
//   - configured interval elapsed since the last discovery
//   - fewer than MinPendingTasksForDiscovery pending tasks
//
// Matches v1's ShouldRunDiscovery semantics exactly so converted PRDs
// behave the same.
func ShouldRunDiscovery(p *prd.AutoPRD, currentIter, lastDiscoveryIter, discoverInterval int) bool {
	pending := p.CountPendingTasks()
	if pending == 0 {
		return true
	}
	if lastDiscoveryIter == 0 {
		return true
	}
	if currentIter-lastDiscoveryIter >= discoverInterval {
		return true
	}
	if pending < prd.MinPendingTasksForDiscovery {
		return true
	}
	return false
}

// InitPilotPRD constructs a fresh AutoPRD configured for pilot mode.
func InitPilotPRD(projectDir string, base prd.AutoConfig, pilot *prd.PilotConfig) *prd.AutoPRD {
	dirName := filepath.Base(projectDir)
	now := time.Now().UTC().Format(time.RFC3339)
	cfg := base
	cfg.PilotMode = true
	cfg.PilotConfig = pilot
	cfg.DiscoveryPromptFile = filepath.Join(prd.RunDir, prd.DiscoveryPromptFile)
	if cfg.PromptFile == "" {
		cfg.PromptFile = filepath.Join(prd.RunDir, prd.PromptFile)
	}
	if cfg.Methodology == "" {
		cfg.Methodology = "ralph"
	}
	return &prd.AutoPRD{
		Version: prd.SchemaVersion,
		Project: prd.AutoProject{
			Name:        dirName,
			Description: "Autonomous pilot mode - AI-discovered tasks",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Config: cfg,
		Tasks:  []prd.AutoTask{},
		Progress: prd.AutoProgress{Status: prd.LoopStatusNotStarted},
	}
}
