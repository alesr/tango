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
		time.Sleep(1 * time.Second)

		match, hostFound := tango.matches.Load(hostPlayer.IP)
		require.True(t, hostFound, "hosting player not found in match")

		assert.NotNil(t, match, "match should not be nil")
		assert.Equal(t, hostPlayer.IP, match.(*Match).hostPlayerIP, "host player IP does not match")
		assert.Equal(t, hostPlayer.GameMode, match.(*Match).gameMode, "game mode does not match")
		assert.ElementsMatch(t, hostPlayer.Tags, match.(*Match).tags, "tags do not match")

		_, playerFound := match.(*Match).joinedPlayers.Load(joiningPlayer.IP)
		assert.True(t, playerFound, "player not found in match")

		assert.Equal(t, int32(0), match.(*Match).getAvailableSlots(), "expected available slots to be 0")
	})

	t.Run("Remove Players", func(t *testing.T) {
		t.Parallel()

		tango := New(logger, 10)

		// Enqueue players
		require.NoError(t, tango.Enqueue(hostPlayer))
		require.NoError(t, tango.Enqueue(joiningPlayer))

		time.Sleep(1 * time.Second)

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

		time.Sleep(1 * time.Second)

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

		// Wait for the matchmaking process to complete
		time.Sleep(1 * time.Second)

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

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
