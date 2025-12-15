package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"rizznet/internal/config"
	"rizznet/internal/db"
	"rizznet/internal/logger"
	"rizznet/internal/model"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show system status and database statistics",
	Long:  `Displays a dashboard of the current database state, including proxy counts, file sizes, category populations, and protocol breakdowns.`,
	Run: func(cmd *cobra.Command, args []string) {
		// 1. Load Config & DB
		cfg, err :=config.Load(cfgFile)
		if err != nil {
			logger.Log.Fatalf("Error loading config: %v", err)
		}

		database, err := db.Connect(cfg.Database.Path)
		if err != nil {
			logger.Log.Fatalf("Error connecting to DB: %v", err)
		}
		defer db.Close(database)

		// 2. Gather Stats
		var totalProxies int64
		database.Model(&model.Proxy{}).Count(&totalProxies)

		// Disk Usage
		dbSize := getFileSize(cfg.Database.Path)
		walSize := getFileSize(cfg.Database.Path + "-wal")

		// Category Counts
		type CatStat struct {
			Name  string
			Count int
		}
		var catStats []CatStat
		database.Table("categories").
			Select("categories.name, count(proxy_categories.proxy_id) as count").
			Joins("left join proxy_categories on proxy_categories.category_id = categories.id").
			Group("categories.name").
			Scan(&catStats)

		// Country Breakdown (Top 5)
		type CountryStat struct {
			Country string
			Count   int
		}
		var countryStats []CountryStat
		database.Model(&model.Proxy{}).
			Select("country, count(*) as count").
			Where("country != ''").
			Group("country").
			Order("count desc").
			Limit(5).
			Scan(&countryStats)

		// Protocol Breakdown (Heuristic based on Raw string prefix)
		// We fetch all raw strings to do this in Go to avoid complex SQL LIKEs
		var rawLinks []string
		database.Model(&model.Proxy{}).Pluck("raw", &rawLinks)
		
		protoCounts := make(map[string]int)
		for _, link := range rawLinks {
			parts := strings.Split(link, "://")
			if len(parts) > 0 {
				protoCounts[parts[0]]++
			}
		}

		// 3. Print Dashboard
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

		fmt.Println("\n📊 \033[1mRIZZNET STATUS DASHBOARD\033[0m")
		fmt.Println("────────────────────────────────────────")

		// System Section
		fmt.Fprintln(w, "\033[1;36m[ SYSTEM ]\033[0m\t")
		fmt.Fprintf(w, "  Database Path:\t%s\n", cfg.Database.Path)
		fmt.Fprintf(w, "  DB Size:\t%s\n", formatBytes(dbSize))
		if walSize > 0 {
			fmt.Fprintf(w, "  WAL Size:\t%s (pending checkpoint)\n", formatBytes(walSize))
		}
		fmt.Fprintf(w, "  Total Proxies:\t%d\n", totalProxies)
		fmt.Fprintln(w, "\t")

		// Category Section
		fmt.Fprintln(w, "\033[1;36m[ CATEGORIES ]\033[0m\t")
		if len(catStats) == 0 {
			fmt.Fprintln(w, "  (No categories populated)")
		} else {
			for _, c := range catStats {
				fmt.Fprintf(w, "  %s:\t%d\n", c.Name, c.Count)
			}
		}
		fmt.Fprintln(w, "\t")

		// Inventory Section
		fmt.Fprintln(w, "\033[1;36m[ INVENTORY ]\033[0m\t")
		
		// Sort protocols for consistent output
		var protos []string
		for k := range protoCounts {
			protos = append(protos, k)
		}
		sort.Strings(protos)
		
		for _, p := range protos {
			fmt.Fprintf(w, "  %s:\t%d\n", p, protoCounts[p])
		}
		fmt.Fprintln(w, "\t")

		// Geo Section
		fmt.Fprintln(w, "\033[1;36m[ TOP LOCATIONS ]\033[0m\t")
		for _, c := range countryStats {
			flag := getFlagEmoji(c.Country)
			fmt.Fprintf(w, "  %s %s:\t%d\n", flag, c.Country, c.Count)
		}

		w.Flush()
		fmt.Println("")
	},
}

// Helpers

func getFileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// Reusing the flag logic purely for display here
func getFlagEmoji(countryCode string) string {
	if len(countryCode) != 2 {
		return "🌐"
	}
	countryCode = strings.ToUpper(countryCode)
	return string(rune(countryCode[0])+127397) + string(rune(countryCode[1])+127397)
}

func init() {
	rootCmd.AddCommand(statusCmd)
}