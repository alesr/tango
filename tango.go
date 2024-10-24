package tango

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// Tango manages players, matches, and matchmaking.
type Tango struct {
	// Configuration
	logger                  *slog.Logger
	attemptToJoinFrequency  time.Duration
	checkDeadlinesFrequency time.Duration
	defaultTimeout          time.Duration

	// State management
	mu      sync.RWMutex
	players map[string]Player
	matches map[string]*match

	// Concurrency control
	playerOps singleflight.Group // For deduplicating player operations
	matchPool sync.Pool          // For match reuse

	// Runtime control
	playerQueue chan Player
	done        chan struct{}
	shutdown    sync.Once
	wg          sync.WaitGroup
	started     atomic.Bool
}

// New creates and initializes a new Tango instance with the provided options.
func New(opts ...Option) *Tango {
	t := Tango{
		logger:                  slog.Default().WithGroup("tango"),
		attemptToJoinFrequency:  defaultAttemptToJoinFrequency,
		checkDeadlinesFrequency: defaultCheckDeadlinesFrequency,
		defaultTimeout:          defaultTimeout,
		playerQueue:             make(chan Player, defaultPlayerQueueSize),
		done:                    make(chan struct{}),
		players:                 make(map[string]Player),
		matches:                 make(map[string]*match),
		matchPool: sync.Pool{
			New: func() any {
				return &match{
					joinedPlayers: make(map[string]struct{}),
				}
			},
		},
	}

	for _, opt := range opts {
		opt(&t)
	}
	return &t
}

// Start initializes and starts all background processing.
// It returns an error if the service is already started.
func (t *Tango) Start() error {
	if !t.started.CompareAndSwap(false, true) {
		return errServiceAlreadyStarted
	}

	t.wg.Add(2)
	go t.processQueue()
	go t.checkTimeouts()

	t.logger.Info("Tango started")
	return nil
}

// Shutdown shuts down the Tango service and cleans up resources.
func (t *Tango) Shutdown(ctx context.Context) error {
	if !t.started.Load() {
		return errServiceNotStarted
	}

	var shutdownErr error

	t.shutdown.Do(func() {
		t.started.Store(false)
		close(t.done)

		cleanupDone := make(chan struct{})
		go func() {
			cleanupCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			t.cleanupMatchesAndPlayers(cleanupCtx)
			close(cleanupDone)
		}()

		select {
		case <-ctx.Done():
			shutdownErr = ctx.Err()
		case <-cleanupDone:
			t.wg.Wait()
		}

		close(t.playerQueue)
		for range t.playerQueue {
		}
	})
	return shutdownErr
}

// Enqueue adds a player to the matchmaking queue.
// Context cancellation can be used for removing the player from queue.
func (t *Tango) Enqueue(ctx context.Context, player Player) error {
	if !t.started.Load() {
		return errServiceNotStarted
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	_, err, _ := t.playerOps.Do(player.ID, func() (any, error) {
		t.mu.Lock()

		if _, exists := t.players[player.ID]; exists {
			t.mu.Unlock()
			return nil, errPlayerAlreadyEnqueued
		}

		t.players[player.ID] = player

		t.mu.Unlock()

		select {
		case t.playerQueue <- player:
			t.logger.Info("Player enqueued", slog.String("player", player.String()))
			return nil, nil
		case <-ctx.Done():
			t.mu.Lock()
			delete(t.players, player.ID)
			t.mu.Unlock()
			return nil, ctx.Err()
		}
	})
	return err
}

// ListMatches returns a list of all active matches.
func (t *Tango) ListMatches() []*match {
	t.mu.RLock()
	matches := make([]*match, 0, len(t.matches))
	for _, match := range t.matches {
		matches = append(matches, match)
	}
	t.mu.RUnlock()
	return matches
}

// RemovePlayer removes a player from the matchmaking system.
func (t *Tango) RemovePlayer(playerID string) error {
	_, err, _ := t.playerOps.Do(fmt.Sprintf("remove-%s", playerID), func() (any, error) {
		t.mu.Lock()
		_, exists := t.players[playerID]
		if !exists {
			t.mu.Unlock()
			return nil, errPlayerNotFound
		}
		delete(t.players, playerID)
		t.mu.Unlock()

		matchToRemove, isHost := t.findMatchForPlayer(playerID)
		if isHost && matchToRemove != nil {
			t.removeMatch(matchToRemove.hostPlayerID)
		} else if matchToRemove != nil {
			matchToRemove.mu.Lock()
			delete(matchToRemove.joinedPlayers, playerID)
			matchToRemove.mu.Unlock()
		}

		t.logger.Info("Player removed", slog.String("player-id", playerID))
		return nil, nil
	})

	return err
}

// tryJoinMatch attempts to place a player into an existing match.
func (t *Tango) tryJoinMatch(player Player) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var matchFound bool
	for _, match := range t.matches {
		if matchFound = match.tryJoin(player); matchFound {
			break
		}
	}

	if !matchFound {
		t.logger.Info("No available matches found, retrying...",
			slog.String("playerID", player.ID),
			slog.String("gameMode", string(player.GameMode)))
	}
	return matchFound
}

// processQueue handles player requests by either creating
// new matches or attempting to join existing ones.
func (t *Tango) processQueue() {
	defer t.wg.Done()

	for {
		select {
		case player, ok := <-t.playerQueue:
			if !ok {
				return
			}
			if player.IsHosting {
				t.createMatch(player)
			} else {
				t.wg.Add(1)
				go func(p Player) {
					defer t.wg.Done()
					t.attemptToJoinMatch(p)
				}(player)
			}
		case <-t.done:
			return
		}
	}
}

// checkTimeouts periodically checks for players whose timeouts
// have expired and removes them from the system.
func (t *Tango) checkTimeouts() {
	defer t.wg.Done()

	ticker := time.NewTicker(t.checkDeadlinesFrequency)
	defer ticker.Stop()

	// Pre-allocate slice for expired players
	expired := make([]string, 0, 32)

	for {
		select {
		case <-ticker.C:
			now := time.Now().Unix()

			t.mu.RLock()
			for id, player := range t.players {
				if now > player.timeout {
					expired = append(expired, id)
				}
			}
			t.mu.RUnlock()

			// Batch remove expired players
			if len(expired) > 0 {
				for _, id := range expired {
					t.RemovePlayer(id)
				}
				expired = expired[:0]
			}
		case <-t.done:
			return
		}
	}
}

// attemptToJoinMatch tries to find an existing match
// for a player within their deadline.
func (t *Tango) attemptToJoinMatch(player Player) {
	ticker := time.NewTicker(t.attemptToJoinFrequency)
	defer ticker.Stop()

	timeoutChan := time.After(
		time.Until(time.Unix(player.timeout, 0)),
	)

	for {
		select {
		case <-timeoutChan:
			t.RemovePlayer(player.ID)
			return
		case <-ticker.C:
			if t.tryJoinMatch(player) {
				return
			}
		case <-t.done:
			return
		}
	}
}

// handlePlayerTimeout removes a player from the system
// when their deadline expires.
func (t *Tango) handlePlayerTimeout(player Player) {
	t.RemovePlayer(player.ID)
}

// findMatchForPlayer searches for the match associated with a player.
func (t *Tango) findMatchForPlayer(playerID string) (*match, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, match := range t.matches {
		match.mu.RLock()
		isHost := match.hostPlayerID == playerID
		_, isJoined := match.joinedPlayers[playerID]
		match.mu.RUnlock()

		if isHost || isJoined {
			return match, isHost
		}
	}
	return nil, false
}

// createMatch creates a new match from the pool.
func (t *Tango) createMatch(player Player) {
	match := t.matchPool.Get().(*match)

	match.mu.Lock()
	match.hostPlayerID = player.ID
	match.availableSlots = uint8(availableSlotsPerGameMode(player.GameMode))
	match.gameMode = player.GameMode
	match.tagCount = player.tagCount
	for i := uint8(0); i < player.tagCount; i++ {
		match.tags[i] = player.tags[i]
	}
	match.mu.Unlock()

	t.mu.Lock()
	t.matches[player.ID] = match
	t.mu.Unlock()
}

// removeMatch removes a match and returns it to the pool.
func (t *Tango) removeMatch(hostPlayerID string) error {
	t.mu.Lock()
	match, exists := t.matches[hostPlayerID]
	if !exists {
		t.mu.Unlock()
		return errors.New("match not found for removal")
	}
	delete(t.matches, hostPlayerID)
	t.mu.Unlock()

	// Cleanup and return to pool after removing from map
	match.mu.Lock()
	// Clear the joined players map
	for playerID := range match.joinedPlayers {
		delete(match.joinedPlayers, playerID)
	}

	// Reset match fields
	match.hostPlayerID = ""
	match.availableSlots = 0
	match.tagCount = 0
	match.gameMode = ""

	// Clear tags array
	for i := range match.tags {
		match.tags[i] = ""
	}
	match.mu.Unlock()

	// Return to pool
	t.matchPool.Put(match)

	return nil
}

// cleanupMatchesAndPlayers cleans up existing matches and players.
func (t *Tango) cleanupMatchesAndPlayers(ctx context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for hostID, match := range t.matches {
		select {
		case <-ctx.Done():
			return
		default:
			t.cleanupAndReturnMatch(match)
			delete(t.matches, hostID)
		}
	}

	for playerID := range t.players {
		select {
		case <-ctx.Done():
			return
		default:
			delete(t.players, playerID)
		}
	}
}

// cleanupAndReturnMatch prepares a match for return to the pool.
func (t *Tango) cleanupAndReturnMatch(match *match) {
	match.mu.Lock()
	defer match.mu.Unlock()

	// Clear the joined players map
	for playerID := range match.joinedPlayers {
		delete(match.joinedPlayers, playerID)
	}

	// Reset match fields
	match.hostPlayerID = ""
	match.availableSlots = 0
	match.tagCount = 0
	match.gameMode = ""

	// Clear tags array
	for i := range match.tags {
		match.tags[i] = ""
	}

	// Return to pool
	t.matchPool.Put(match)
}
