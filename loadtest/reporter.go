package loadtest

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alesr/tango"
)

// TestReport contains all the data from a load test run
type TestReport struct {
	Scenario  string
	StartTime time.Time
	Duration  time.Duration
	Snapshots []SystemSnapshot
	Metrics   *Metrics
	Config    *Config
}

// SaveReport saves test results to files for later analysis
func (lt *LoadTest) SaveReport(outputDir string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	timestamp := time.Now().Format("2006-01-02-15-04-05")
	scenario := strings.ToLower(lt.cfg.Scenario)
	if scenario == "" {
		scenario = "default"
	}

	// Save raw data as CSV
	if err := lt.saveSnapshotsCSV(filepath.Join(outputDir,
		fmt.Sprintf("%s-%s-snapshots.csv", scenario, timestamp))); err != nil {
		return err
	}

	// Save summary
	summaryPath := filepath.Join(outputDir,
		fmt.Sprintf("%s-%s-summary.txt", scenario, timestamp))
	return os.WriteFile(summaryPath, []byte(lt.GetDetailedSummary()), 0644)
}

func (lt *LoadTest) saveSnapshotsCSV(filepath string) error {
	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write headers
	headers := []string{
		"Timestamp",
		"ElapsedSeconds",
		"TotalPlayers",
		"TotalMatches",
		"RequestsPerSecond",
		"AverageLatencyMS",
		"P95LatencyMS",
		"P99LatencyMS",
		"SuccessRate",
	}

	for mode := range tango.AllGameModes {
		headers = append(headers,
			fmt.Sprintf("%s_Matches", mode),
			fmt.Sprintf("%s_Players", mode),
			fmt.Sprintf("%s_OpenSlots", mode),
		)
	}

	if err := writer.Write(headers); err != nil {
		return err
	}

	// Write data rows
	for _, snap := range lt.GetSnapshots() {
		row := []string{
			snap.Timestamp.Format(time.RFC3339),
			fmt.Sprintf("%.2f", snap.ElapsedTime.Seconds()),
			fmt.Sprintf("%d", snap.Stats.TotalPlayers),
			fmt.Sprintf("%d", snap.Stats.TotalMatches),
			fmt.Sprintf("%.2f", snap.RequestsPerSec),
			fmt.Sprintf("%.2f", float64(lt.metrics.AverageLatency)/float64(time.Millisecond)),
			fmt.Sprintf("%.2f", float64(lt.metrics.P95Latency)/float64(time.Millisecond)),
			fmt.Sprintf("%.2f", float64(lt.metrics.P99Latency)/float64(time.Millisecond)),
			fmt.Sprintf("%.2f", float64(lt.metrics.SuccessfulJoins)/float64(lt.metrics.TotalRequests)),
		}

		for mode := range tango.AllGameModes {
			stats := snap.Stats.MatchesByGameMode[mode]
			row = append(row,
				fmt.Sprintf("%d", stats.ActiveMatches),
				fmt.Sprintf("%d", stats.PlayersJoined),
				fmt.Sprintf("%d", stats.OpenSlots),
			)
		}

		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}

// GetDetailedSummary returns a comprehensive analysis of the test results
func (lt *LoadTest) GetDetailedSummary() string {
	lt.snapshotsMu.RLock()
	defer lt.snapshotsMu.RUnlock()

	if len(lt.snapshots) == 0 {
		return "No data collected"
	}

	var sb strings.Builder
	first := lt.snapshots[0]
	last := lt.snapshots[len(lt.snapshots)-1]

	// Test Configuration
	fmt.Fprintf(&sb, "Load Test Configuration:\n"+
		"------------------------\n"+
		"Scenario: %s\n"+
		"Concurrent Requests: %d\n"+
		"Rate Limit: %.2f/sec\n"+
		"Duration: %v\n\n",
		lt.cfg.Scenario,
		lt.cfg.ConcurrentRequests,
		lt.cfg.RequestsPerSecond,
		lt.cfg.Duration,
	)

	// Performance Metrics
	fmt.Fprintf(&sb, "Performance Metrics:\n"+
		"-------------------\n"+
		"Total Requests: %d\n"+
		"Success Rate: %.2f%%\n"+
		"Average RPS: %.2f\n"+
		"Peak RPS: %.2f\n"+
		"Average Latency: %v\n"+
		"P95 Latency: %v\n"+
		"P99 Latency: %v\n\n",
		lt.metrics.TotalRequests,
		float64(lt.metrics.SuccessfulJoins)/float64(lt.metrics.TotalRequests)*100,
		float64(lt.metrics.TotalRequests)/last.ElapsedTime.Seconds(),
		lt.getPeakRPS(),
		lt.metrics.AverageLatency,
		lt.metrics.P95Latency,
		lt.metrics.P99Latency,
	)

	// System State Changes
	fmt.Fprintf(&sb, "System State Changes:\n"+
		"--------------------\n"+
		"Initial Players: %d\n"+
		"Final Players: %d\n"+
		"Peak Players: %d\n"+
		"Final Matches: %d\n\n",
		first.Stats.TotalPlayers,
		last.Stats.TotalPlayers,
		lt.getPeakPlayers(),
		last.Stats.TotalMatches,
	)

	// Game Mode Distribution
	fmt.Fprintf(&sb, "Game Mode Distribution:\n"+
		"---------------------\n")
	for mode, stats := range last.Stats.MatchesByGameMode {
		if stats.ActiveMatches > 0 {
			fmt.Fprintf(&sb, "%v:\n"+
				"  Active Matches: %d\n"+
				"  Players Joined: %d\n"+
				"  Average Players/Match: %.2f\n"+
				"  Open Slots: %d\n",
				mode,
				stats.ActiveMatches,
				stats.PlayersJoined,
				float64(stats.PlayersJoined)/float64(stats.ActiveMatches),
				stats.OpenSlots,
			)
		}
	}

	return sb.String()
}

// Helper methods for analytics
func (lt *LoadTest) getPeakRPS() float64 {
	var peak float64
	for _, snap := range lt.snapshots {
		if snap.RequestsPerSec > peak {
			peak = snap.RequestsPerSec
		}
	}
	return peak
}

func (lt *LoadTest) getPeakPlayers() int {
	var peak int
	for _, snap := range lt.snapshots {
		if snap.Stats.TotalPlayers > peak {
			peak = snap.Stats.TotalPlayers
		}
	}
	return peak
}
