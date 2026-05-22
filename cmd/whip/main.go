package main

import (
	"context"
	"fmt"
	"os"

	"github.com/adrian/whip/internal/aliases"
	"github.com/adrian/whip/internal/notify"
	"github.com/adrian/whip/internal/source"
	"github.com/adrian/whip/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func main() {
	var host string
	root := &cobra.Command{
		Use:   "whip",
		Short: "Monitor multiple Claude Code sessions, locally or via SSH.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(host)
		},
	}
	root.Flags().StringVar(&host, "host", "", "ssh_config Host alias to monitor remotely (default: local)")
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(host string) error {
	var src source.Source
	remote := host != ""

	if remote {
		r, err := source.NewRemote(host)
		if err != nil {
			return fmt.Errorf("connect to %s: %w", host, err)
		}
		src = r
	} else {
		l, err := source.NewLocal("")
		if err != nil {
			return err
		}
		src = l
	}
	defer src.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Open the watch stream before constructing the program so we can wire
	// updates straight to Program.Send.
	watchCh, err := src.Watch(ctx)
	if err != nil {
		return fmt.Errorf("watch: %w", err)
	}

	store, err := aliases.Load("")
	if err != nil {
		return fmt.Errorf("load aliases: %w", err)
	}

	model := ui.NewModel(src, notify.New(), store, remote, hostLabel(host))
	prog := tea.NewProgram(model, tea.WithAltScreen())

	go pumpWatch(prog, watchCh)

	if _, err := prog.Run(); err != nil {
		return err
	}
	return nil
}

func hostLabel(host string) string {
	if host == "" {
		return "local"
	}
	return host
}

// pumpWatch forwards Source events into the Bubble Tea event loop.
func pumpWatch(prog *tea.Program, ch <-chan source.Event) {
	for ev := range ch {
		switch ev.Kind {
		case source.EventUpsert:
			prog.Send(ui.UpsertMsg(ev.Session))
		case source.EventDelete:
			prog.Send(ui.DeleteMsg(ev.Session.ID))
		}
	}
}
