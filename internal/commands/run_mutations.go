package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ar4mirez/samuel/internal/lock"
	"github.com/ar4mirez/samuel/internal/methodology/ralph/prd"
	"github.com/ar4mirez/samuel/internal/ui"
)

// mutate is the shared envelope for every CLI-mutation command. It
// acquires an iteration lock (so a concurrent `samuel run start` can't
// race the agent's `samuel run done` call), reloads prd.toon, applies
// `fn`, and saves atomically.
func mutate(cmd *cobra.Command, action string, fn func(p *prd.AutoPRD) (map[string]any, error)) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	prdPath := prd.PRDPath(cwd)
	release, err := lock.Acquire(cwd)
	if err != nil {
		return fmt.Errorf("acquire iteration lock: %w", err)
	}
	defer release()

	p, _, err := prd.Load(prdPath)
	if err != nil {
		return fmt.Errorf("load prd.toon: %w", err)
	}
	payload, err := fn(p)
	if err != nil {
		return err
	}
	if err := p.Save(prdPath); err != nil {
		return fmt.Errorf("save prd.toon: %w", err)
	}
	if JSONMode(cmd) {
		if payload == nil {
			payload = map[string]any{}
		}
		payload["action"] = action
		ui.PrintJSON(commandPath(cmd), payload)
	}
	return nil
}

func runRunTaskDone(cmd *cobra.Command, args []string) error {
	id := args[0]
	commitSHA, _ := cmd.Flags().GetString("commit-sha")
	iter, _ := cmd.Flags().GetInt("iteration")
	return mutate(cmd, "done", func(p *prd.AutoPRD) (map[string]any, error) {
		if err := p.CompleteTask(id, commitSHA, iter); err != nil {
			return nil, err
		}
		if !JSONMode(cmd) {
			ui.Success("Task %s marked completed (commit=%s, iter=%d)", id, truncSHA(commitSHA), iter)
		}
		return map[string]any{"taskId": id, "commitSha": commitSHA, "iteration": iter}, nil
	})
}

func runRunTaskSkip(cmd *cobra.Command, args []string) error {
	id := args[0]
	reason, _ := cmd.Flags().GetString("reason")
	return mutate(cmd, "skip", func(p *prd.AutoPRD) (map[string]any, error) {
		if err := p.SkipTask(id); err != nil {
			return nil, err
		}
		if !JSONMode(cmd) {
			if reason != "" {
				ui.Success("Task %s skipped — %s", id, reason)
			} else {
				ui.Success("Task %s skipped", id)
			}
		}
		return map[string]any{"taskId": id, "reason": reason}, nil
	})
}

func runRunTaskReset(cmd *cobra.Command, args []string) error {
	id := args[0]
	return mutate(cmd, "reset", func(p *prd.AutoPRD) (map[string]any, error) {
		if err := p.ResetTask(id); err != nil {
			return nil, err
		}
		if !JSONMode(cmd) {
			ui.Success("Task %s reset to pending", id)
		}
		return map[string]any{"taskId": id}, nil
	})
}

func runRunTaskEnqueue(cmd *cobra.Command, args []string) error {
	priority, _ := cmd.Flags().GetString("priority")
	complexity, _ := cmd.Flags().GetString("complexity")
	source, _ := cmd.Flags().GetString("source")
	title := args[0]
	return mutate(cmd, "enqueue", func(p *prd.AutoPRD) (map[string]any, error) {
		id := p.NextAvailableID()
		task := prd.AutoTask{
			ID:         id,
			Title:      title,
			Status:     prd.StatusPending,
			Priority:   priority,
			Complexity: complexity,
			Source:     source,
		}
		if err := p.AddTask(task); err != nil {
			return nil, err
		}
		if !JSONMode(cmd) {
			ui.Success("Task %s enqueued: %s", id, title)
		}
		return map[string]any{"taskId": id, "title": title, "priority": priority, "complexity": complexity, "source": source}, nil
	})
}

func runRunTaskAdd(cmd *cobra.Command, args []string) error {
	id := args[0]
	title := args[1]
	return mutate(cmd, "task add", func(p *prd.AutoPRD) (map[string]any, error) {
		task := prd.AutoTask{
			ID:       id,
			Title:    title,
			Status:   prd.StatusPending,
			Priority: prd.PriorityMedium,
		}
		if err := p.AddTask(task); err != nil {
			return nil, err
		}
		if !JSONMode(cmd) {
			ui.Success("Task %s added: %s", id, title)
		}
		return map[string]any{"taskId": id, "title": title}, nil
	})
}

func truncSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
