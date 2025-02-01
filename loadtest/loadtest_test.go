package loadtest

import (
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunWithStats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	log.Println("Starting TestRunWithStats")

	lt, err := New(Config{
		Requests:           10,
		ConcurrentRequests: 10,
		Duration:           time.Second,
		StatsInterval:      100 * time.Millisecond,
	})
	require.NoError(t, err)

	log.Println("LoadTest instance created successfully")

	lt.Run()

	log.Println("Load test completed")

	snapshots := lt.GetSnapshots()
	require.NotEmpty(t, snapshots, "Should have collected snapshots")
	log.Printf("Collected %d snapshots\n", len(snapshots))

	lastSnapshot := snapshots[len(snapshots)-1]
	require.Greater(t, lastSnapshot.Stats.TotalPlayers, 0, "Total players should be greater than 0")
	log.Printf("Final total players: %d\n", lastSnapshot.Stats.TotalPlayers)

	require.Greater(t, lastSnapshot.Stats.TotalMatches, 0, "Total matches should be greater than 0")
	log.Printf("Final total matches: %d\n", lastSnapshot.Stats.TotalMatches)

	t.Log("\n" + lt.GetSummary())
}

func TestLoadScenarios(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	scenarios := []struct {
		name     string
		config   Config
		validate func(*testing.T, []SystemSnapshot, *Metrics)
	}{
		{
			name: "high_concurrency",
			config: Config{
				Requests:           1000,
				ConcurrentRequests: 50,
				Duration:           10 * time.Second,
				StatsInterval:      100 * time.Millisecond,
				RequestsPerSecond:  100,
			},
			validate: validateHighConcurrency,
		},
		{
			name: "host_heavy",
			config: Config{
				Requests:           500,
				ConcurrentRequests: 20,
				Duration:           5 * time.Second,
				StatsInterval:      100 * time.Millisecond,
				Scenario:           "highHostRate",
			},
			validate: validateHostHeavy,
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			log.Printf("Starting scenario: %s\n", sc.name)

			lt, err := New(sc.config)
			require.NoError(t, err)
			log.Println("LoadTest instance created successfully")

			lt.Run()
			log.Println("Load test completed")

			err = lt.SaveReport("./testreports")
			require.NoError(t, err)
			log.Println("Report saved successfully")

			t.Log("\n" + lt.GetDetailedSummary())

			snapshots := lt.GetSnapshots()

			require.NotEmpty(t, snapshots)
			log.Printf("Collected %d snapshots\n", len(snapshots))

			lt.metricsMu.RLock()
			sc.validate(t, snapshots, lt.metrics)
			lt.metricsMu.RUnlock()
			log.Println("Validation completed")
		})
	}
}

func validateHighConcurrency(t *testing.T, snapshots []SystemSnapshot, metrics *Metrics) {
	require.True(t, metrics.AverageLatency < 100*time.Millisecond)
	log.Printf("Average latency: %v\n", metrics.AverageLatency)
	require.Greater(t, metrics.SuccessfulJoins, 0)
	log.Printf("Successful joins: %d\n", metrics.SuccessfulJoins)
}

func validateHostHeavy(t *testing.T, snapshots []SystemSnapshot, metrics *Metrics) {
	lastSnapshot := snapshots[len(snapshots)-1]
	require.Greater(t, lastSnapshot.Stats.TotalMatches, 0)
	log.Printf("Final total matches: %d\n", lastSnapshot.Stats.TotalMatches)
}
