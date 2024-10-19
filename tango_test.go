package tango

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTangoMatchmaking(t *testing.T) {
	t.Parallel()

	logger := noopLogger()

	hostPlayer := Player{
		IP:        "host_player_id",
		IsHosting: true,
		Tags:      []string{"dummy-tag"},
		GameMode:  GameMode1v1,
		Deadline:  time.Now().Add(10 * time.Second),
	}

	joiningPlayer := Player{
		IP:       "player_ip",
		Tags:     []string{"dummy-tag"},
		GameMode: GameMode1v1,
		Deadline: time.Now().Add(10 * time.Second),
	}

	t.Run("Match for Host Player", func(t *testing.T) {
		t.Parallel()

		tango := New(logger, 10)

		// Enqueue players
		require.NoError(t, tango.Enqueue(hostPlayer))
		require.NoError(t, tango.Enqueue(joiningPlayer))

		// Wait for the matchmaking process to complete
		sleep()

		m, hostFound := tango.matches.Load(hostPlayer.IP)
		require.True(t, hostFound, "hosting player not found in match")

		match := m.(*Match)

		assert.NotNil(t, match, "match should not be nil")
		assert.Equal(t, hostPlayer.IP, match.hostPlayerIP, "host player IP does not match")
		assert.Equal(t, hostPlayer.GameMode, match.gameMode, "game mode does not match")
		assert.ElementsMatch(t, hostPlayer.Tags, match.tags, "tags do not match")

		_, playerFound := match.joinedPlayers.Load(joiningPlayer.IP)
		assert.True(t, playerFound, "player not found in match")

		match.mu.RLock()
		defer match.mu.RUnlock()

		assert.Equal(t, 0, match.availableSlots, "expected available slots to be 0")
	})

	t.Run("Remove Players", func(t *testing.T) {
		t.Parallel()

		tango := New(logger, 10)

		// Enqueue players
		require.NoError(t, tango.Enqueue(hostPlayer))
		require.NoError(t, tango.Enqueue(joiningPlayer))

		sleep()

		// Remove joining player
		require.NoError(t, tango.RemovePlayer(joiningPlayer.IP))
		_, playerFound := tango.players.Load(joiningPlayer.IP)
		assert.False(t, playerFound, "player should not be found after removal")

		// Now remove the host player
		require.NoError(t, tango.RemovePlayer(hostPlayer.IP))
		_, hostFound := tango.players.Load(hostPlayer.IP)
		assert.False(t, hostFound, "host should not be found after removal")
	})

	t.Run("Close Match", func(t *testing.T) {
		t.Parallel()

		tango := New(logger, 10)

		// Create a player who is hosting a match
		hostPlayer := Player{
			IP:        "host_player_id",
			IsHosting: true,
			Tags:      []string{"dummy-tag"},
			GameMode:  GameMode1v1,
			Deadline:  time.Now().Add(5 * time.Second),
		}

		require.NoError(t, tango.Enqueue(hostPlayer))

		sleep()

		// Validate that the match exists again
		_, hostFound := tango.matches.Load(hostPlayer.IP)
		require.True(t, hostFound, "hosting player not found in match after re-enqueue")

		// Close the match
		require.NoError(t, tango.RemovePlayer(hostPlayer.IP))

		// Validate that the match is removed
		_, matchFound := tango.matches.Load(hostPlayer.IP)
		assert.False(t, matchFound, "match should not be found after closing")
	})

	t.Run("List Matches", func(t *testing.T) {
		t.Parallel()

		tango := New(logger, 10)

		hostPlayer := Player{
			IP:        "host_player_id",
			IsHosting: true,
			Tags:      []string{"dummy-tag"},
			GameMode:  GameMode1v1,
			Deadline:  time.Now().Add(5 * time.Second),
		}

		require.NoError(t, tango.Enqueue(hostPlayer))

		sleep()

		// Check if the match is listed correctly
		matches := tango.ListMatches()
		assert.Len(t, matches, 1, "expected 1 match in the list")
		assert.Equal(t, hostPlayer.IP, matches[0].hostPlayerIP, "host player IP does not match")
		assert.Equal(t, hostPlayer.GameMode, matches[0].gameMode, "game mode does not match")

		// Close the match
		require.NoError(t, tango.RemovePlayer(hostPlayer.IP))

		// Validate that no matches exist after closing
		matches = tango.ListMatches()
		assert.Len(t, matches, 0, "expected 0 matches after closing")
	})

	t.Run("Player Deadline Timeout", func(t *testing.T) {
		t.Parallel()

		tango := New(logger, 10)

		player := Player{
			IP:       "timeout_player_id",
			Tags:     []string{"dummy-tag"},
			GameMode: GameMode1v1,
			Deadline: time.Now().Add(1 * time.Second), // Set a short deadline
		}

		require.NoError(t, tango.Enqueue(player))

		// Wait for the deadline to pass
		time.Sleep(2 * time.Second) // Wait longer than the deadline

		// Verify that the player has been removed due to timeout
		_, found := tango.players.Load(player.IP)
		assert.False(t, found, "player should not be found after deadline timeout")
	})
}

func TestTangoMultiplePlayers(t *testing.T) {
	t.Parallel()

	hosts := []Player{
		{IP: "host1", IsHosting: true, Tags: []string{"tag"}, GameMode: GameMode1v1, Deadline: time.Now().Add(10 * time.Second)},
		{IP: "host2", IsHosting: true, Tags: []string{"tag"}, GameMode: GameMode2v2, Deadline: time.Now().Add(10 * time.Second)},
		{IP: "host3", IsHosting: true, Tags: []string{"tag"}, GameMode: GameMode1v1, Deadline: time.Now().Add(10 * time.Second)},
	}

	players := []Player{
		{IP: "player1", Tags: []string{"tag"}, GameMode: GameMode1v1, Deadline: time.Now().Add(10 * time.Second)},
		{IP: "player2", Tags: []string{"tag"}, GameMode: GameMode2v2, Deadline: time.Now().Add(10 * time.Second)},
		{IP: "player3", Tags: []string{"tag"}, GameMode: GameMode2v2, Deadline: time.Now().Add(10 * time.Second)},
		{IP: "player4", Tags: []string{"tag"}, GameMode: GameMode1v1, Deadline: time.Now().Add(10 * time.Second)},
		{IP: "timeout_player", Tags: []string{"tag"}, GameMode: GameMode1v1, Deadline: time.Now().Add(1 * time.Second)},
	}

	t.Run("Enqueue Players and Hosts", func(t *testing.T) {
		t.Parallel()

		logger := noopLogger()
		tango := New(logger, 10)

		for _, host := range hosts {
			require.NoError(t, tango.Enqueue(host))
		}

		for _, player := range players {
			require.NoError(t, tango.Enqueue(player))
		}

		// Wait longer for all players to potentially join matches
		time.Sleep(2 * time.Second)

		// Check matches for hosts
		for _, host := range hosts {
			match, found := tango.matches.Load(host.IP)
			require.True(t, found, "host not found in match")
			assert.NotNil(t, match, "match should not be nil")
		}

		// Check for players in matches
		for _, player := range players {
			var matchFound bool
			for _, host := range hosts {
				// Load the match from the sync.Map
				if match, playerFound := tango.matches.Load(host.IP); playerFound {
					// Check if the player is in the joinedPlayers map of the match
					if _, found := match.(*Match).joinedPlayers.Load(player.IP); found {
						matchFound = true
						break
					}
				}
			}

			if player.IP == "timeout_player" {
				_, found := tango.players.Load(players[4].IP)
				assert.False(t, found, "timeout player should not be found after deadline timeout")

				assert.False(t, matchFound, "player %s found in a match", player.IP)
			} else {
				assert.True(t, matchFound, "player %s not found in any match", player.IP)
			}
		}
	})

	t.Run("No Available Matches", func(t *testing.T) {
		t.Parallel()

		logger := noopLogger()
		tango := New(logger, 10)

		noMatchPlayer := Player{
			IP:       "no_match_player",
			Tags:     []string{"tag"},
			GameMode: GameMode3v3, // No match available for this mode
			Deadline: time.Now().Add(5 * time.Second),
		}

		require.NoError(t, tango.Enqueue(noMatchPlayer))

		sleep()

		_, found := tango.players.Load(noMatchPlayer.IP)
		assert.True(t, found, "player should still be in queue as no matches are available")
	})

	t.Run("Remove Host and Cleanup", func(t *testing.T) {
		t.Parallel()

		logger := noopLogger()
		tango := New(logger, 10)

		require.NoError(t, tango.Enqueue(hosts[0]))
		require.NoError(t, tango.Enqueue(hosts[1]))

		sleep()

		require.NoError(t, tango.RemovePlayer(hosts[0].IP))

		sleep()

		_, found := tango.matches.Load(hosts[0].IP)
		assert.False(t, found, "match should not be found after host removal")

		// Check other host's match still exists
		match, found := tango.matches.Load(hosts[1].IP)
		require.True(t, found, "remaining host match should still exist")
		assert.NotNil(t, match, "match should not be nil")
	})
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func sleep() {
	time.Sleep(time.Second)
}
