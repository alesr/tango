package tango

import (
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GameMode defines different game modes for matches.
// The game mode determines the number of available slots in a match,
// with the host always occupying one slot.
type GameMode string

const (
	// GameMode1v1 represents a 1v1 match type.
	GameMode1v1 GameMode = "1v1"
	// GameMode2v2 represents a 2v2 match type.
	GameMode2v2 GameMode = "2v2"
	// GameMode3v3 represents a 3v3 match type.
	GameMode3v3 GameMode = "3v3"

	// Frequency which Tango tries to find a match for a player.
	attemptToJoinMatchFrequency = time.Millisecond * 500

	// Frequency which Tango looks for players with matching deadline reached.
	checkDeadlinesFrequency = time.Second
)

type TangoError struct {
	Title string
}

type PlayerAlreadyEnqueuedError struct{ TangoError }

func (e PlayerAlreadyEnqueuedError) Error() string {
	return e.Title
}

var errPlayerAlreadyEnqueued = PlayerAlreadyEnqueuedError{TangoError{"player already enqueued"}}

// Match represents a game match with a host, joined players, and game mode details.
type Match struct {
	hostPlayerIP   string
	joinedPlayers  sync.Map
	availableSlots int
	tags           []string
	gameMode       GameMode
	mu             sync.RWMutex
}

// String returns a string representation of the Match.
func (m *Match) String() string {
	var sb strings.Builder

	sb.WriteString("Match Details:\n")
	sb.WriteString("Host Player IP: " + m.hostPlayerIP + "\n")
	sb.WriteString("Joined Players: ")

	var playerCount int
	m.joinedPlayers.Range(func(_, _ any) bool {
		playerCount++
		return true
	})

	sb.WriteString(strconv.Itoa(playerCount) + "\n")
	sb.WriteString("Available Slots: " + strconv.Itoa(m.availableSlots) + "\n")
	sb.WriteString("Tags: " + strings.Join(m.tags, ", ") + "\n")
	sb.WriteString("Game Mode: " + string(m.gameMode) + "\n")

	return sb.String()
}

// Player represents a player attempting to join or host a match,
// along with game mode preferences.
type Player struct {
	IP        string
	IsHosting bool
	Tags      []string
	GameMode  GameMode
	Deadline  time.Time
}

// Tango is the main structure that manages players,
// matches, and matchmaking logic.
type Tango struct {
	logger      *slog.Logger
	players     sync.Map
	matches     sync.Map
	playerQueue chan Player
	mu          sync.Mutex
}

// New creates and initializes a new Tango instance.
func New(logger *slog.Logger, queueSize int) *Tango {
	t := &Tango{
		logger:      logger.WithGroup("tango"),
		playerQueue: make(chan Player, queueSize),
	}

	go t.processQueue()
	go t.checkDeadlines()

	return t
}

// Enqueue adds a player to the matchmaking queue.
// If the player is already enqueued, an error is returned.
func (t *Tango) Enqueue(player Player) error {
	if _, found := t.players.LoadOrStore(player.IP, player); found {
		return errPlayerAlreadyEnqueued
	}
	t.playerQueue <- player
	return nil
}

// ListMatches returns a list of all active matches managed by Tango.
func (t *Tango) ListMatches() []*Match {
	var matches []*Match
	t.matches.Range(func(_, value any) bool {
		match := value.(*Match)
		matches = append(matches, match)
		return true
	})
	return matches
}

// RemovePlayer removes a player from the matchmaking system.
// If the player is hosting a match, the match is also removed.
func (t *Tango) RemovePlayer(playerIP string) error {
	if _, found := t.players.Load(playerIP); !found {
		return errors.New("player not found")
	}

	t.players.Delete(playerIP)

	matchToRemove, isHost := t.findMatchForPlayer(playerIP)

	if isHost && matchToRemove != nil {
		t.removeMatch(matchToRemove.hostPlayerIP)
	} else if matchToRemove != nil {
		t.removePlayer(playerIP)
	}

	return nil
}

// findMatchForPlayer searches for the match associated with a player,
// returning it if found along with a boolean indicating if the player is the host.
func (t *Tango) findMatchForPlayer(playerIP string) (*Match, bool) {
	var (
		matchToRemove *Match
		isHost        bool
	)

	t.mu.Lock()
	defer t.mu.Unlock()

	t.matches.Range(func(_, value any) bool {
		match := value.(*Match)

		if match.hostPlayerIP == playerIP {
			isHost = true
			matchToRemove = match
			return false
		}

		match.mu.Lock()
		defer match.mu.Unlock()

		if _, ok := match.joinedPlayers.Load(playerIP); ok {
			match.joinedPlayers.Delete(playerIP)
			match.availableSlots++

			if match.availableSlots == availableSlotsPerGameMode(match.gameMode) {
				matchToRemove = match
				return false
			}
		}
		return true
	})

	return matchToRemove, isHost
}

// removePlayer deletes a player from the active matches.
func (t *Tango) removePlayer(playerIP string) {
	t.matches.Delete(playerIP)
}

// removeMatch removes a match by its host's IP,
// also removing all joined players from the matchmaking system.
func (t *Tango) removeMatch(hostPlayerIP string) error {
	if _, found := t.matches.Load(hostPlayerIP); !found {
		return errors.New("match not found for removal")
	}

	// Get the match and remove all joined players
	match, _ := t.matches.Load(hostPlayerIP)
	matchInstance := match.(*Match)

	// Lock before accessing joinedPlayers
	matchInstance.mu.Lock()
	defer matchInstance.mu.Unlock()

	// Remove each player from the players map
	matchInstance.joinedPlayers.Range(func(key, _ any) bool {
		playerIP := key.(string)
		t.players.Delete(playerIP)
		return true
	})

	t.matches.Delete(hostPlayerIP)
	t.players.Delete(hostPlayerIP)

	return nil
}

// availableSlotsPerGameMode returns the number of available
// slots based on the provided game mode.
func availableSlotsPerGameMode(gm GameMode) int {
	switch gm {
	case GameMode1v1:
		return 1
	case GameMode2v2:
		return 3
	default:
		return 5
	}
}

// processQueue handles player requests by either creating
// new matches or attempting to join existing ones.
func (t *Tango) processQueue() {
	for player := range t.playerQueue {
		if player.IsHosting {
			t.createMatch(player)
		} else {
			go t.attemptToJoinMatch(player)
		}
	}
}

// attemptToJoinMatch tries to find an existing match
// for a player within a specified deadline.
func (t *Tango) attemptToJoinMatch(player Player) {
	deadline := time.After(time.Until(player.Deadline))

	ticker := time.NewTicker(attemptToJoinMatchFrequency)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.handlePlayerTimeout(player)
			return
		case <-ticker.C:
			if t.tryJoinMatch(player) {
				return
			}
		}
	}
}

// tryJoinMatch attempts to place a player into an existing match,
// returning true if successful.
func (t *Tango) tryJoinMatch(player Player) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	var matchFound bool

	t.matches.Range(func(_, value any) bool {
		match := value.(*Match)

		match.mu.Lock()
		defer match.mu.Unlock()

		if match.gameMode == player.GameMode && match.availableSlots > 0 {
			match.joinedPlayers.Store(player.IP, player)
			match.availableSlots--
			matchFound = true
			return false
		}
		return true
	})

	if !matchFound {
		t.logger.Info("No available matches found, retrying...", slog.String("playerIP", player.IP))
	}
	return matchFound
}

// handlePlayerTimeout removes a player from the system
// if their deadline expires before joining a match.
func (t *Tango) handlePlayerTimeout(player Player) {
	t.RemovePlayer(player.IP)
}

// checkDeadlines periodically checks for players whose deadlines
//
//	have expired and removes them from the system.
func (t *Tango) checkDeadlines() {
	ticker := time.NewTicker(checkDeadlinesFrequency)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		t.players.Range(func(_, value any) bool {
			player := value.(Player)

			if now.After(player.Deadline) {
				t.RemovePlayer(player.IP)
			}
			return true
		})
	}
}

// createMatch creates a new match hosted by the specified player.
func (t *Tango) createMatch(player Player) {
	match := Match{
		hostPlayerIP:   player.IP,
		joinedPlayers:  sync.Map{},
		availableSlots: availableSlotsPerGameMode(player.GameMode),
		tags:           player.Tags,
		gameMode:       player.GameMode,
	}
	t.matches.Store(player.IP, &match)
}
