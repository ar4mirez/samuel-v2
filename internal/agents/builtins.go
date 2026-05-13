package agents

// Built-in adapters mirror the five v1 supported agents. The
// declarative fields are the only thing that varies; the wrapping
// behaviour comes from builtinAdapter.
//
// Sources:
//   - claude  : v1 args `-p <content> --dangerously-skip-permissions`
//   - codex   : v1 args `--dangerously-bypass-approvals-and-sandbox exec <content>`
//   - copilot : v1 wrapper (file-arg)
//   - gemini  : prompt via -p flag (file-arg/content-arg)
//   - kiro    : stdin-piped prompt
//
// The exact CLI surfaces are tracked in [[concepts/multi-agent-support]].

func init() {
	Register(claudeAdapter())
	Register(codexAdapter())
	Register(copilotAdapter())
	Register(geminiAdapter())
	Register(kiroAdapter())
}

func claudeAdapter() AgentAdapter {
	return &builtinAdapter{
		manifest: Manifest{
			Name:         "claude",
			DefaultImage: "ghcr.io/anthropics/claude-code:latest",
			EnvAllowlist: []string{"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL"},
			PromptMode:   PromptModeContentArg,
		},
		buildArgs: func(_ Manifest, opts Options) []string {
			args := []string{"-p", opts.PromptContent, "--dangerously-skip-permissions"}
			return append(args, opts.ExtraArgs...)
		},
	}
}

func codexAdapter() AgentAdapter {
	return &builtinAdapter{
		manifest: Manifest{
			Name:         "codex",
			DefaultImage: "ghcr.io/openai/codex:latest",
			EnvAllowlist: []string{"OPENAI_API_KEY", "OPENAI_BASE_URL"},
			PromptMode:   PromptModeContentArg,
		},
		buildArgs: func(_ Manifest, opts Options) []string {
			args := []string{"--dangerously-bypass-approvals-and-sandbox", "exec", opts.PromptContent}
			return append(args, opts.ExtraArgs...)
		},
	}
}

func copilotAdapter() AgentAdapter {
	return &builtinAdapter{
		manifest: Manifest{
			Name:         "copilot",
			DefaultImage: "ghcr.io/github/copilot-cli:latest",
			EnvAllowlist: []string{"GITHUB_TOKEN", "GH_TOKEN"},
			PromptMode:   PromptModeFileArg,
		},
		buildArgs: func(_ Manifest, opts Options) []string {
			args := []string{"--prompt", opts.PromptPath}
			return append(args, opts.ExtraArgs...)
		},
	}
}

func geminiAdapter() AgentAdapter {
	return &builtinAdapter{
		manifest: Manifest{
			Name:         "gemini",
			DefaultImage: "ghcr.io/google/gemini-cli:latest",
			EnvAllowlist: []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
			PromptMode:   PromptModeContentArg,
		},
		buildArgs: func(_ Manifest, opts Options) []string {
			args := []string{"-p", opts.PromptContent}
			return append(args, opts.ExtraArgs...)
		},
	}
}

func kiroAdapter() AgentAdapter {
	return &builtinAdapter{
		manifest: Manifest{
			Name:         "kiro",
			DefaultImage: "ghcr.io/kiro-ai/kiro:latest",
			EnvAllowlist: []string{"KIRO_API_KEY"},
			PromptMode:   PromptModeStdin,
		},
	}
}
