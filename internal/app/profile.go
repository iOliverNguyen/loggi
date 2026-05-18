package app

import (
	"fmt"
	"sort"

	"github.com/iOliverNguyen/loggi/internal/config"
	"github.com/spf13/cobra"
)

// NewProfileCmd implements `loggi profile [list|show|use|save|rm]`.
//
// `use`, `save`, and `rm` mutate the user-level config at
// ~/.zz/loggi/config.toml. `list` and `show` use the merged effective config.
func NewProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage profiles",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List configured profiles (merged across user/repo/local)",
		RunE: func(_ *cobra.Command, _ []string) error {
			loaded, err := config.Load(mustGetwd())
			if err != nil {
				return err
			}
			if len(loaded.Config.Profiles) == 0 {
				fmt.Println("(no profiles configured)")
				return nil
			}
			sorted := append([]config.Profile(nil), loaded.Config.Profiles...)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
			marker := func(name string) string {
				if name == loaded.Config.UI.DefaultProfile {
					return "*"
				}
				return " "
			}
			for _, p := range sorted {
				fmt.Printf("%s %-16s  %s\n", marker(p.Name), p.Name, p.Filter)
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Print the filter expression for a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			loaded, err := config.Load(mustGetwd())
			if err != nil {
				return err
			}
			for _, p := range loaded.Config.Profiles {
				if p.Name == args[0] {
					fmt.Println(p.Filter)
					return nil
				}
			}
			return fmt.Errorf("no such profile: %s", args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "use <name>",
		Short: "Set the default profile in the user config",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			// Verify the profile exists in the merged view (it might be
			// defined at the repo level — that's fine).
			loaded, err := config.Load(mustGetwd())
			if err != nil {
				return err
			}
			found := false
			for _, p := range loaded.Config.Profiles {
				if p.Name == name {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("no such profile: %s", name)
			}
			user, err := config.LoadUser()
			if err != nil {
				return err
			}
			user.UI.DefaultProfile = name
			if err := config.SaveUser(user); err != nil {
				return err
			}
			fmt.Printf("default profile is now %q\n", name)
			return nil
		},
	})
	{
		var filterFlag string
		var columnsFlag []string
		save := &cobra.Command{
			Use:   "save <name>",
			Short: "Add or update a profile in the user config",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				name := args[0]
				if filterFlag == "" {
					return fmt.Errorf("--filter is required")
				}
				user, err := config.LoadUser()
				if err != nil {
					return err
				}
				updated := false
				for i := range user.Profiles {
					if user.Profiles[i].Name == name {
						user.Profiles[i].Filter = filterFlag
						if columnsFlag != nil {
							user.Profiles[i].Columns = columnsFlag
						}
						updated = true
						break
					}
				}
				if !updated {
					user.Profiles = append(user.Profiles, config.Profile{
						Name:    name,
						Filter:  filterFlag,
						Columns: columnsFlag,
					})
				}
				if err := config.SaveUser(user); err != nil {
					return err
				}
				verb := "saved"
				if updated {
					verb = "updated"
				}
				fmt.Printf("%s profile %q\n", verb, name)
				return nil
			},
		}
		save.Flags().StringVar(&filterFlag, "filter", "", "filter expression (required)")
		save.Flags().StringSliceVar(&columnsFlag, "columns", nil, "columns to display, comma-separated")
		cmd.AddCommand(save)
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a profile from the user config",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			user, err := config.LoadUser()
			if err != nil {
				return err
			}
			out := user.Profiles[:0]
			removed := false
			for _, p := range user.Profiles {
				if p.Name == name {
					removed = true
					continue
				}
				out = append(out, p)
			}
			if !removed {
				return fmt.Errorf("no such profile in user config: %s", name)
			}
			user.Profiles = out
			if err := config.SaveUser(user); err != nil {
				return err
			}
			fmt.Printf("removed profile %q\n", name)
			return nil
		},
	})
	return cmd
}

// NewConfigCmd implements `loggi config print`.
func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect loggi configuration",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "print",
		Short: "Print the effective merged configuration",
		RunE: func(_ *cobra.Command, _ []string) error {
			loaded, err := config.Load(mustGetwd())
			if err != nil {
				return err
			}
			fmt.Println("# files merged:")
			for _, p := range loaded.Found {
				fmt.Println("#  ", p)
			}
			if len(loaded.Found) == 0 {
				fmt.Println("#   (none — using defaults)")
			}
			fmt.Println()
			cfg := loaded.Config
			fmt.Printf("[server]\nidle_timeout = %q\nring_buffer  = %d\nhttp_bind    = %q\n\n",
				cfg.Server.IdleTimeout, cfg.Server.RingBuffer, cfg.Server.HTTPBind)
			fmt.Printf("[ui]\ntheme            = %q\ndefault_profile  = %q\ntimestamp_format = %q\n\n",
				cfg.UI.Theme, cfg.UI.DefaultProfile, cfg.UI.TimestampFormat)
			for _, p := range cfg.Profiles {
				fmt.Printf("[[profiles]]\nname    = %q\nfilter  = %q\ncolumns = %v\n\n",
					p.Name, p.Filter, p.Columns)
			}
			return nil
		},
	})
	return cmd
}
