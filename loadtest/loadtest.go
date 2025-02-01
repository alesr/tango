package loadtest

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/alesr/tango"
	"github.com/alesr/tango/pkg/loggerutil"
)

var (
	currentID int64
	nextID    = func() int64 {
		return atomic.AddInt64(&currentID, 1)
	}
)

type Config struct {
	Requests           int
	ConcurrentRequests int
	Duration           time.Duration
	StatsInterval      time.Duration
	RequestsPerSecond  float64
	Scenario           string
}

// SystemSnapshot represents system state at a point in time
type SystemSnapshot struct {
	Timestamp      time.Time
	Stats          tango.Stats
	RequestCount   int
	ElapsedTime    time.Duration
	RequestsPerSec float64
}

// Metrics tracks detailed performance metrics
type Metrics struct {
	TotalRequests   int
	SuccessfulJoins int
	FailedJoins     int
	AverageLatency  time.Duration
	P95Latency      time.Duration
	P99Latency      time.Duration
	ErrorsByType    map[string]int
}

type LoadTest struct {
	tg           *tango.Tango
	cfg          *Config
	snapshots    []SystemSnapshot
	snapshotsMu  sync.RWMutex
	metrics      *Metrics
	metricsMu    sync.RWMutex
	limiter      *rate.Limiter
	latencies    []time.Duration
	latenciesMu  sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	requestCount atomic.Int64
	startTime    time.Time
	done         chan struct{}
}

func New(cfg Config) (*LoadTest, error) {
	if cfg.StatsInterval == 0 {
		cfg.StatsInterval = time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	var limiter *rate.Limiter
	if cfg.RequestsPerSecond > 0 {
		limiter = rate.NewLimiter(rate.Limit(cfg.RequestsPerSecond), 1)
	}

	tg := tango.New(
		tango.WithLogger(loggerutil.NoopLogger()),
		tango.WithOperationBufferSize(100000),
		tango.WithDefaultTimeout(5*time.Second),
		tango.WithStatsUpdateInterval(100*time.Millisecond),
	)

	if err := tg.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("could not start tango: %w", err)
	}

	return &LoadTest{
		tg:      tg,
		cfg:     &cfg,
		metrics: &Metrics{ErrorsByType: make(map[string]int)},
		limiter: limiter,
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}, nil
}

func (lt *LoadTest) Run() time.Duration {
	lt.startTime = time.Now()
	var wg sync.WaitGroup

	go lt.collectStats()

	for i := 0; i < lt.cfg.ConcurrentRequests; i++ {
		wg.Add(1)
		go lt.worker(&wg)
	}

	select {
	case <-time.After(lt.cfg.Duration):
		lt.cancel() // Stop all workers
	case <-lt.done:
		// Test completed naturally
	}

	wg.Wait()
	close(lt.done)

	lt.calculateMetrics()
	return time.Since(lt.startTime)
}

func (lt *LoadTest) worker(wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-lt.ctx.Done():
			return
		default:
			if lt.limiter != nil {
				if err := lt.limiter.Wait(lt.ctx); err != nil {
					return
				}
			}

			start := time.Now()
			err := lt.tg.Enqueue(lt.ctx, lt.generatePlayer())
			latency := time.Since(start)

			lt.recordMetrics(err, latency)
			lt.requestCount.Add(1)
		}
	}
}

func (lt *LoadTest) recordMetrics(err error, latency time.Duration) {
	lt.metricsMu.Lock()
	defer lt.metricsMu.Unlock()

	lt.metrics.TotalRequests++
	if err == nil {
		lt.metrics.SuccessfulJoins++
	} else {
		lt.metrics.FailedJoins++
		lt.metrics.ErrorsByType[err.Error()]++
	}

	lt.latenciesMu.Lock()
	lt.latencies = append(lt.latencies, latency)
	lt.latenciesMu.Unlock()
}

func (lt *LoadTest) calculateMetrics() {
	lt.latenciesMu.Lock()
	defer lt.latenciesMu.Unlock()

	if len(lt.latencies) == 0 {
		return
	}

	// Sort latencies for percentile calculation
	sort.Slice(lt.latencies, func(i, j int) bool {
		return lt.latencies[i] < lt.latencies[j]
	})

	lt.metricsMu.Lock()
	defer lt.metricsMu.Unlock()

	total := time.Duration(0)
	for _, lat := range lt.latencies {
		total += lat
	}

	lt.metrics.AverageLatency = total / time.Duration(len(lt.latencies))
	// Calculate P95 and P99 percentiles
	if len(lt.latencies) > 1 {
		p95Index := len(lt.latencies) * 95 / 100
		p99Index := len(lt.latencies) * 99 / 100

		lt.metrics.P95Latency = lt.latencies[p95Index]
		lt.metrics.P99Latency = lt.latencies[p99Index]
	} else {
		lt.metrics.P95Latency = lt.latencies[0]
		lt.metrics.P99Latency = lt.latencies[0]
	}
}

func (lt *LoadTest) generatePlayer() tango.Player {
	switch lt.cfg.Scenario {
	case "highHostRate":
		return genPlayerWithHighHostRate()
	case "allJoiners":
		return genPlayerJoinerOnly()
	default:
		return genPlayer()
	}
}

// Add scenario-specific player generation
func genPlayerWithHighHostRate() tango.Player {
	return tango.NewPlayer(
		fmt.Sprintf("player-%d", nextID()),
		rand.Float32() < 0.7, // 70% chance of being host
		gameMode(),
		time.Now().Add(50*time.Second),
		[]string{},
	)
}

func genPlayerJoinerOnly() tango.Player {
	return tango.NewPlayer(
		fmt.Sprintf("player-%d", nextID()),
		false, // Never host
		gameMode(),
		time.Now().Add(50*time.Second),
		[]string{},
	)
}

// collectStats periodically collects system stats
func (lt *LoadTest) collectStats() {
	ticker := time.NewTicker(lt.cfg.StatsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			stats, err := lt.tg.Stats(context.Background())
			if err != nil {
				continue
			}

			elapsed := time.Since(lt.startTime)
			reqCount := lt.requestCount.Load()

			snapshot := SystemSnapshot{
				Timestamp:      time.Now(),
				Stats:          stats,
				RequestCount:   int(reqCount),
				ElapsedTime:    elapsed,
				RequestsPerSec: float64(reqCount) / elapsed.Seconds(),
			}

			lt.snapshotsMu.Lock()
			lt.snapshots = append(lt.snapshots, snapshot)
			lt.snapshotsMu.Unlock()

		case <-lt.done:
			return
		}
	}
}

// GetSnapshots returns all collected system snapshots
func (lt *LoadTest) GetSnapshots() []SystemSnapshot {
	lt.snapshotsMu.RLock()
	defer lt.snapshotsMu.RUnlock()

	result := make([]SystemSnapshot, len(lt.snapshots))
	copy(result, lt.snapshots)
	return result
}

// GetSummary returns a summary of the load test results
func (lt *LoadTest) GetSummary() string {
	lt.snapshotsMu.RLock()
	defer lt.snapshotsMu.RUnlock()

	if len(lt.snapshots) == 0 {
		return "No data collected"
	}

	last := lt.snapshots[len(lt.snapshots)-1]

	summary := fmt.Sprintf(
		"Load Test Summary:\n"+
			"Duration: %v\n"+
			"Total Requests: %d\n"+
			"Avg Requests/sec: %.2f\n"+
			"Final System State:\n"+
			"  Total Matches: %d\n"+
			"  Total Players: %d\n"+
			"Matches by Game Mode:\n",
		last.ElapsedTime,
		last.RequestCount,
		last.RequestsPerSec,
		last.Stats.TotalMatches,
		last.Stats.TotalPlayers,
	)

	for mode, stats := range last.Stats.MatchesByGameMode {
		if stats.ActiveMatches > 0 {
			summary += fmt.Sprintf(
				"  %v: %d matches, %d players, %d open slots\n",
				mode,
				stats.ActiveMatches,
				stats.PlayersJoined,
				stats.OpenSlots,
			)
		}
	}

	lt.metricsMu.RLock()
	defer lt.metricsMu.RUnlock()

	summary += fmt.Sprintf("\nDetailed Metrics:\n"+
		"Total Requests: %d\n"+
		"Successful Joins: %d\n"+
		"Failed Joins: %d\n"+
		"Average Latency: %v\n"+
		"P95 Latency: %v\n"+
		"P99 Latency: %v\n"+
		"Errors:\n",
		lt.metrics.TotalRequests,
		lt.metrics.SuccessfulJoins,
		lt.metrics.FailedJoins,
		lt.metrics.AverageLatency,
		lt.metrics.P95Latency,
		lt.metrics.P99Latency,
	)

	for errType, count := range lt.metrics.ErrorsByType {
		summary += fmt.Sprintf("  %s: %d\n", errType, count)
	}
	return summary
}

func genPlayer() tango.Player {
	return tango.NewPlayer(
		fmt.Sprintf("player-%d", nextID()),
		isHost(),
		gameMode(),
		time.Now().Add(50*time.Second),
		[]string{},
	)
}

// 1/3 of the time the player is a host
func isHost() bool {
	return rand.Intn(3) == 0
}

// rand game mode
func gameMode() tango.GameMode {
	n := rand.Intn(3)
	switch n {
	case 0:
		return tango.GameMode1v1
	case 1:
		return tango.GameMode2v2
	default:
		return tango.GameMode3v3
	}
}
