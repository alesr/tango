/*
BENCHMARK WARNING:

If you're looking for reliable, reproducible benchmarks, you're in the wrong place.
These numbers are about as stable as a caffeinated squirrel on a tightrope.

Use these benchmarks as rough guides for implementation, not gospel.
*/

package tango

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/alesr/tango/pkg/loggerutil"
)

func BenchmarkMatchmaking(b *testing.B) {
	b.ReportAllocs()

	const (
		numHosts   = 20
		numPlayers = 100
	)

	fmt.Printf("\nRunning matchmaking benchmark with:\n"+
		"- Hosts: %d\n"+
		"- Players: %d\n"+
		"- Game Mode Distribution: 1v1 (40%%), 2v2 (30%%), 3v3 (30%%)\n"+
		"- Join Frequency: 100ms\n"+
		"- Deadline Check Frequency: 100ms\n"+
		"- Player Timeout: 1 minute\n\n",
		numHosts, numPlayers)

	// Create player and host templates
	deadline := time.Now().Add(time.Minute)
	hostTemplates := map[GameMode]Player{
		GameMode1v1: NewPlayer("host-template-1v1", true, GameMode1v1, deadline, nil),
		GameMode2v2: NewPlayer("host-template-2v2", true, GameMode2v2, deadline, nil),
		GameMode3v3: NewPlayer("host-template-3v3", true, GameMode3v3, deadline, nil),
	}

	playerTemplates := map[GameMode]Player{
		GameMode1v1: NewPlayer("player-template-1v1", false, GameMode1v1, deadline, nil),
		GameMode2v2: NewPlayer("player-template-2v2", false, GameMode2v2, deadline, nil),
		GameMode3v3: NewPlayer("player-template-3v3", false, GameMode3v3, deadline, nil),
	}

	tango := New(
		WithLogger(loggerutil.NoopLogger()),
		WithAttemptToJoinFrequency(time.Millisecond*100),
		WithCheckDeadlinesFrequency(time.Millisecond*100),
	)

	if err := tango.Start(); err != nil {
		b.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Enqueue hosts and players in mixed order
		for j := 0; j < numHosts+numPlayers; j++ {
			var p Player
			if j < numHosts {
				mode := selectGameMode()
				p = hostTemplates[mode]
				p.ID = fmt.Sprintf("host-%d", j+1)
			} else {
				mode := selectGameMode()
				p = playerTemplates[mode]
				p.ID = fmt.Sprintf("player-%d", j-numHosts+1)
			}

			if err := tango.Enqueue(ctx, p); err != nil {
				b.Errorf("error enqueueing player/host: %v", err)
			}
		}

		// Wait for the matchmaking to complete
		select {
		case <-tango.doneCh:
		case <-time.After(time.Second * 10):
			cancel()
		}
	}

	b.StopTimer()
	if err := tango.Shutdown(context.Background()); err != nil {
		b.Fatalf("error shutting down Tango service: %v", err)
	}
}

func selectGameMode() GameMode {
	r := rand.Float64()
	switch {
	case r < 0.4:
		return GameMode1v1
	case r < 0.7:
		return GameMode2v2
	default:
		return GameMode3v3
	}
}
