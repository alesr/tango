package tango

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPlayer represents a test player configuration
type testPlayer struct {
	id       string
	isHost   bool
	gameMode GameMode
	tags     []string
}

// testContext represents common test context
type testContext struct {
	t     *testing.T
	tango *Tango
	ctx   context.Context
}

// setupTest creates a new test context with initialized Tango instance
func setupTest(t *testing.T) *testContext {
	t.Helper()
	ctx := context.TODO()
	tango := New(WithLogger(noopLogger()))
	require.NoError(t, tango.Start())

	return &testContext{
		t:     t,
		tango: tango,
		ctx:   ctx,
	}
}

// cleanup performs test cleanup
func (tc *testContext) cleanup() {
	tc.t.Helper()
	require.NoError(tc.t, tc.tango.Shutdown(tc.ctx))
}

// createTestPlayer creates a new player for testing
func createTestPlayer(cfg testPlayer) Player {
	deadline := time.Now().Add(10 * time.Second)
	return NewPlayer(cfg.id, cfg.isHost, cfg.gameMode, deadline, cfg.tags)
}

// assertPlayerInMatch checks if a player is in a specific match
func (tc *testContext) assertPlayerInMatch(hostID, playerID string) bool {
	tc.t.Helper()

	tc.tango.state.RLock()
	defer tc.tango.state.RUnlock()

	match, exists := tc.tango.state.matches[hostID]
	if !exists {
		return false
	}

	match.mu.RLock()
	defer match.mu.RUnlock()

	_, found := match.joinedPlayers.Load(playerID)
	return found && match.availableSlots == 0
}

// enqueuePlayers enqueues multiple players concurrently
func (tc *testContext) enqueuePlayers(players []Player) error {
	errCh := make(chan error, len(players))

	for _, player := range players {
		go func(p Player) {
			errCh <- tc.tango.Enqueue(tc.ctx, p)
		}(player)
	}

	for i := 0; i < len(players); i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}
	return nil
}

func TestTangoMatchmaking(t *testing.T) {
	t.Parallel()

	t.Run("Match for Host Player", func(t *testing.T) {
		t.Parallel()
		tc := setupTest(t)
		defer tc.cleanup()

		hostPlayer := createTestPlayer(testPlayer{
			id:       "host_player_id",
			isHost:   true,
			gameMode: GameMode1v1,
			tags:     []string{"dummy-tag"},
		})

		joiningPlayer := createTestPlayer(testPlayer{
			id:       "player_ip",
			isHost:   false,
			gameMode: GameMode1v1,
			tags:     []string{"dummy-tag"},
		})

		require.NoError(t, tc.tango.Enqueue(tc.ctx, hostPlayer))
		require.NoError(t, tc.tango.Enqueue(tc.ctx, joiningPlayer))

		assert.Eventually(t, func() bool {
			return tc.assertPlayerInMatch(hostPlayer.ID, joiningPlayer.ID)
		}, 2*time.Second, 100*time.Millisecond, "match should be created and player should join")
	})

	t.Run("Context Cancellation", func(t *testing.T) {
		t.Parallel()
		tc := setupTest(t)
		defer tc.cleanup()

		ctx, cancel := context.WithCancel(context.TODO())
		cancel() // Cancel immediately

		player := createTestPlayer(testPlayer{
			id:       "test-player",
			isHost:   false,
			gameMode: GameMode1v1,
			tags:     nil,
		})

		err := tc.tango.Enqueue(ctx, player)
		assert.ErrorIs(t, err, context.Canceled, "should return context.Canceled error")

		tc.tango.state.RLock()
		_, exists := tc.tango.state.players[player.ID]

		tc.tango.state.RUnlock()
		assert.False(t, exists, "player should not be enqueued after context cancellation")
	})

	t.Run("Graceful Shutdown", func(t *testing.T) {
		t.Parallel()
		tc := setupTest(t)

		hostPlayer := createTestPlayer(testPlayer{
			id:       "host_player_id",
			isHost:   true,
			gameMode: GameMode1v1,
			tags:     []string{"dummy-tag"},
		})

		joiningPlayer := createTestPlayer(testPlayer{
			id:       "player_ip",
			isHost:   false,
			gameMode: GameMode1v1,
			tags:     []string{"dummy-tag"},
		})

		require.NoError(t, tc.tango.Enqueue(tc.ctx, hostPlayer))
		require.NoError(t, tc.tango.Enqueue(tc.ctx, joiningPlayer))

		time.Sleep(200 * time.Millisecond)

		shutdownCtx, cancel := context.WithTimeout(tc.ctx, 5*time.Second)
		defer cancel()

		require.NoError(t, tc.tango.Shutdown(shutdownCtx))

		_, err := tc.tango.ListMatches()
		assert.ErrorIs(t, err, errServiceNotStarted)

		tc.tango.state.RLock()
		playerCount := len(tc.tango.state.players)
		tc.tango.state.RUnlock()
		assert.Zero(t, playerCount, "all players should be removed after shutdown")
	})

	t.Run("Shutdown Timeout", func(t *testing.T) {
		t.Parallel()
		tc := setupTest(t)

		var players []Player
		for i := 0; i < 50; i++ {
			players = append(players,
				createTestPlayer(testPlayer{
					id:       fmt.Sprintf("host-%d", i),
					isHost:   true,
					gameMode: GameMode1v1,
					tags:     []string{"tag"},
				}),
				createTestPlayer(testPlayer{
					id:       fmt.Sprintf("player-%d", i),
					isHost:   false,
					gameMode: GameMode1v1,
					tags:     []string{"tag"},
				}),
			)
		}

		require.NoError(t, tc.enqueuePlayers(players))
		time.Sleep(100 * time.Millisecond)

		shutdownCtx, cancel := context.WithTimeout(tc.ctx, time.Nanosecond)
		defer cancel()

		err := tc.tango.Shutdown(shutdownCtx)
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		cleanupCtx, cleanupCancel := context.WithTimeout(context.TODO(), 5*time.Second)
		defer cleanupCancel()
		_ = tc.tango.Shutdown(cleanupCtx)
	})

	t.Run("Player Deadline Timeout", func(t *testing.T) {
		t.Parallel()
		tc := setupTest(t)
		defer tc.cleanup()

		player := NewPlayer(
			"timeout_player_id",
			false,
			GameMode1v1,
			time.Now().Add(500*time.Millisecond),
			[]string{"dummy-tag"},
		)

		require.NoError(t, tc.tango.Enqueue(tc.ctx, player))

		assert.Eventually(t, func() bool {
			tc.tango.state.RLock()
			_, found := tc.tango.state.players[player.ID]
			tc.tango.state.RUnlock()
			return !found
		}, 2*time.Second, 100*time.Millisecond, "player should be removed after deadline")
	})
}

func TestTangoMultiplePlayers(t *testing.T) {
	t.Parallel()

	t.Run("Concurrent Matchmaking", func(t *testing.T) {
		t.Parallel()
		tc := setupTest(t)
		defer tc.cleanup()

		var allPlayers []Player
		for _, p := range []testPlayer{
			{id: "host1", isHost: true, gameMode: GameMode1v1, tags: []string{"tag"}},
			{id: "host2", isHost: true, gameMode: GameMode2v2, tags: []string{"tag"}},
			{id: "host3", isHost: true, gameMode: GameMode1v1, tags: []string{"tag"}},
			{id: "player1", isHost: false, gameMode: GameMode1v1, tags: []string{"tag"}},
			{id: "player2", isHost: false, gameMode: GameMode2v2, tags: []string{"tag"}},
			{id: "player3", isHost: false, gameMode: GameMode2v2, tags: []string{"tag"}},
			{id: "player4", isHost: false, gameMode: GameMode1v1, tags: []string{"tag"}},
		} {
			allPlayers = append(allPlayers, createTestPlayer(p))
		}

		require.NoError(t, tc.enqueuePlayers(allPlayers))

		assert.Eventually(t, func() bool {
			tc.tango.state.RLock()
			defer tc.tango.state.RUnlock()

			matchedCount := 0
			for _, player := range allPlayers {
				if !player.IsHosting {
					for _, match := range tc.tango.state.matches {
						match.mu.RLock()
						_, found := match.joinedPlayers.Load(player.ID)
						match.mu.RUnlock()
						if found {
							matchedCount++
							break
						}
					}
				}
			}
			return matchedCount == 4 // number of non-host players
		}, 3*time.Second, 100*time.Millisecond, "all players should be matched")
	})
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
