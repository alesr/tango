package tango

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alesr/tango/pkg/loggerutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testContext struct {
	tango *Tango
	ctx   context.Context
}

func setupTest(t *testing.T) *testContext {
	t.Helper()
	tango := New(
		WithLogger(loggerutil.NoopLogger()),
		WithDefaultTimeout(5*time.Second),
		WithAttemptToJoinFrequency(100*time.Millisecond),
		WithCheckDeadlinesFrequency(10*time.Millisecond),
	)
	require.NoError(t, tango.Start())

	return &testContext{
		tango: tango,
		ctx:   context.Background(),
	}
}

type testPlayer struct {
	id       string
	isHost   bool
	gameMode GameMode
	tags     []string
}

func (tc *testContext) createTestPlayer(p testPlayer) Player {
	return NewPlayer(p.id, p.isHost, p.gameMode, time.Now().Add(10*time.Second), p.tags)
}

func (tc *testContext) cleanup() {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Force stop all worker pools
	if tc.tango.matchWorkers != nil {
		tc.tango.matchWorkers.cancel()
	}
	_ = tc.tango.Shutdown(cleanupCtx)
}

func (tc *testContext) enqueuePlayers(players []Player) error {
	for _, p := range players {
		if err := tc.tango.Enqueue(tc.ctx, p); err != nil {
			return fmt.Errorf("failed to enqueue player %s: %w", p.ID, err)
		}
	}
	return nil
}

func (tc *testContext) assertPlayerInMatch(hostID, playerID string) bool {
	tc.tango.state.RLock()
	defer tc.tango.state.RUnlock()

	match, exists := tc.tango.state.matches[hostID]
	if !exists {
		return false
	}

	match.mu.RLock()
	defer match.mu.RUnlock()
	_, found := match.joinedPlayers.Load(playerID)
	return found
}

func TestTangoMatchmaking(t *testing.T) {
	t.Parallel()

	t.Run("Match for Host Player", func(t *testing.T) {
		t.Parallel()
		tc := setupTest(t)
		defer tc.cleanup()

		hostPlayer := tc.createTestPlayer(testPlayer{
			id:       "host_player_id",
			isHost:   true,
			gameMode: GameMode1v1,
			tags:     []string{"dummy-tag"},
		})

		joiningPlayer := tc.createTestPlayer(testPlayer{
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

		player := tc.createTestPlayer(testPlayer{
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

		ctx := context.Background()
		hostPlayer := tc.createTestPlayer(testPlayer{
			id:       "host_player_id",
			isHost:   true,
			gameMode: GameMode1v1,
			tags:     []string{"dummy-tag"},
		})

		joiningPlayer := tc.createTestPlayer(testPlayer{
			id:       "player_ip",
			isHost:   false,
			gameMode: GameMode1v1,
			tags:     []string{"dummy-tag"},
		})

		require.NoError(t, tc.tango.Enqueue(ctx, hostPlayer))
		require.NoError(t, tc.tango.Enqueue(ctx, joiningPlayer))

		// Wait for match to be created
		assert.Eventually(t, func() bool {
			return tc.assertPlayerInMatch(hostPlayer.ID, joiningPlayer.ID)
		}, 2*time.Second, 100*time.Millisecond)

		// Perform shutdown with longer timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		require.NoError(t, tc.tango.Shutdown(shutdownCtx))
	})

	t.Run("Shutdown Timeout", func(t *testing.T) {
		t.Parallel()
		tc := setupTest(t)

		var players []Player
		for i := 0; i < 50; i++ {
			players = append(players,
				tc.createTestPlayer(testPlayer{
					id:       fmt.Sprintf("host-%d", i),
					isHost:   true,
					gameMode: GameMode1v1,
					tags:     []string{"tag"},
				}),
				tc.createTestPlayer(testPlayer{
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

		// Set deadline to 1 second from now
		deadline := time.Now().Add(time.Second)
		player := NewPlayer(
			"timeout_player_id",
			false,
			GameMode1v1,
			deadline,
			[]string{"dummy-tag"},
		)

		require.NoError(t, tc.tango.Enqueue(tc.ctx, player))

		// Wait a bit longer than the deadline to ensure the timeout check runs
		assert.Eventually(t, func() bool {
			tc.tango.state.RLock()
			defer tc.tango.state.RUnlock()
			_, exists := tc.tango.state.players[player.ID]
			return !exists
		}, 3*time.Second, 100*time.Millisecond, "player should be removed after deadline")
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
			allPlayers = append(allPlayers, tc.createTestPlayer(p))
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
