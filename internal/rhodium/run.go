package rhodium

import (
	"fmt"
	"os"
	"rhodium/internal/gh"

	tea "github.com/charmbracelet/bubbletea"
)

func Run(args []string) error {
	if len(args) > 0 {
		return runCLI(args)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.GitHubUser == "" {
		if login, err := gh.FetchUser(); err == nil {
			cfg.GitHubUser = login
		} else {
			fmt.Fprintln(os.Stderr, "warning: could not detect GitHub user — set `github_user` in config to split your PRs:", err)
		}
	}
	brain, err := LoadBrain()
	if err != nil {
		return err
	}
	p := tea.NewProgram(newApp(cfg, brain), tea.WithAltScreen())
	program = p
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
