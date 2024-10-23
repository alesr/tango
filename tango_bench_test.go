package tango

import (
	"math/rand"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Benchmark that enqueues players, simulating them either hosting or joining matches.
func BenchmarkMainFlow(b *testing.B) {
	tango := New(noopLogger(), 5000)

	// Number of players to enqueue per iteration
	numPlayers := 1000

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(numPlayers)

		for j := 0; j < numPlayers; j++ {
			go func(j int) {
				defer wg.Done()

				player := Player{
					IP:        "192.168.1." + strconv.Itoa(j%255),
					IsHosting: j%2 == 0,         // Alternate between hosting and joining
					GameMode:  randomGameMode(), // Randomized game mode
					Deadline:  time.Now().Add(1 * time.Minute),
				}
				_ = tango.Enqueue(player)
			}(j)
		}
		wg.Wait()
	}
}

func randomGameMode() GameMode {
	modes := []GameMode{GameMode1v1, GameMode2v2, GameMode3v3}
	return modes[rand.Intn(len(modes))]
}
