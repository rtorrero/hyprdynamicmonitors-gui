package cmd

import (
	"context"
	"fmt"

	"github.com/fiffeek/hyprdynamicmonitors/internal/gui"
	"github.com/spf13/cobra"
)

var guiCmd = &cobra.Command{
	Use:   "gui",
	Short: "Launch the graphical UI for monitor and profile configuration",
	Long: `Launch a GTK graphical interface (inspired by wdisplays) to visually arrange
monitors via drag-and-drop, tweak per-monitor settings, apply them live to Hyprland,
and create/save hyprdynamicmonitors profiles from the current setup.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		application, err := gui.NewApp(ctx, configPath, Version)
		if err != nil {
			return fmt.Errorf("cant init gui: %w", err)
		}

		return application.Run()
	},
}

func init() {
	rootCmd.AddCommand(guiCmd)
}
