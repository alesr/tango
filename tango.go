package tango

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	opEnqueuePlayer operationType = iota + 1
	opRemovePlayer
	opListMatches
	opTimeout
	opStats
)

// operationType defines the types of operations that can be performed
type operationType int

// operation represents a request to modify system state
type operation struct {
	op       operationType
	player   Player
	playerID string
	respCh   chan response
	doneCh   chan struct{}
}

// response carries the result of an operation
type response struct {
	data any
	err  error
}

// Stats holds information about the current state of matches
type Stats struct {
	MatchesByGameMode map[GameMode]GameModeStats
	TotalMatches      int
	TotalPlayers      int
}

type GameModeStats struct {
	ActiveMatches int
	OpenSlots     int
	PlayersJoined int
}

// Tango manages players, matches, and matchmaking.
type Tango struct {
	// Configuration
	logger                  *slog.Logger
	attemptToJoinFrequency  time.Duration
	checkDeadlinesFrequency time.Duration
	defaultTimeout          time.Duration

	// State management
	state struct {
		sync.RWMutex
		players map[string]Player
		matches map[string]*match
	}

	// Channel-based coordination
	opCh      chan operation
	timeoutCh chan string
	doneCh    chan struct{}

	// Match pooling
	matchPool sync.Pool

	// Worker pool for matchmaking
	matchWorkers *matchWorkerPool

	// Runtime control
	shutdown sync.Once
	wg       sync.WaitGroup
	started  atomic.Bool

	// Stats tracking
	statsSnapshot       atomic.Pointer[Stats]
	statsUpdateInterval time.Duration

	// Event handling
	eventCh     chan Event
	eventBuffer int
}

// New creates and initializes a new Tango instance with the provided options.
func New(opts ...Option) *Tango {
	t := Tango{
		logger:                  slog.Default().WithGroup("tango"),
		attemptToJoinFrequency:  defaultAttemptToJoinFrequency,
		checkDeadlinesFrequency: defaultCheckDeadlinesFrequency,
		defaultTimeout:          defaultTimeout,
		statsUpdateInterval:     time.Second,

		opCh:      make(chan operation),
		timeoutCh: make(chan string),
		doneCh:    make(chan struct{}),
	}

	t.state.players = make(map[string]Player)
	t.state.matches = make(map[string]*match)

	t.matchPool = sync.Pool{
		New: func() any {
			return newMatch(Player{}, 0)
		},
	}

	// Initialize stats snapshot
	initialStats := &Stats{MatchesByGameMode: make(map[GameMode]GameModeStats)}
	t.statsSnapshot.Store(initialStats)

	for _, opt := range opts {
		opt(&t)
	}
	return &t
}

// Start initializes and starts all background processing.
func (t *Tango) Start() error {
	if !t.started.CompareAndSwap(false, true) {
		return errServiceAlreadyStarted
	}

	// Initialize worker pool if not set
	if t.matchWorkers == nil {
		t.matchWorkers = newMatchWorkerPool(defaultNumWorkers, defaultJobBufferSize, t)
	}

	t.wg.Add(3)
	go t.processOperations()
	go t.processTimeouts()
	go t.updateStats()

	t.logger.Info("Tango started")
	return nil
}

// Shutdown gracefully shuts down the service.
func (t *Tango) Shutdown(ctx context.Context) error {
	if !t.started.Load() {
		return errServiceNotStarted
	}

	var shutdownErr error

	t.shutdown.Do(func() {
		t.started.Store(false)

		// Shutdown worker pool first
		if t.matchWorkers != nil {
			t.matchWorkers.shutdown()
		}

		// First stop accepting new operations
		close(t.doneCh)

		// Wait group for cleanup
		var cleanupWg sync.WaitGroup
		cleanupWg.Add(1)

		go func() {
			defer cleanupWg.Done()
			t.cleanupMatchesAndPlayers(ctx)
		}()

		// Wait for cleanup  done or context cancellation
		done := make(chan struct{})
		go func() {
			cleanupWg.Wait()
			close(done)
		}()

		select {
		case <-ctx.Done():
			shutdownErr = ctx.Err()
		case <-done:
			t.wg.Wait() // Wait for all goroutines to finish
		}

		// Now it's safe to close operation channels
		close(t.opCh)
		close(t.timeoutCh)
	})
	return shutdownErr
}

// Enqueue adds a player to the matchmaking queue.
func (t *Tango) Enqueue(ctx context.Context, player Player) error {
	if !t.started.Load() {
		return errServiceNotStarted
	}

	respCh := make(chan response, 1)
	done := make(chan struct{})
	defer close(done)

	select {
	case t.opCh <- operation{
		op:     opEnqueuePlayer,
		player: player,
		respCh: respCh,
		doneCh: done,
	}:
		select {
		case resp := <-respCh:
			if resp.err != nil {
				return resp.err
			}
			t.logger.Info("Player enqueued", slog.String("player", player.String()))
			return nil
		case <-ctx.Done():
			// Remove player if context is cancelled
			t.opCh <- operation{
				op:       opRemovePlayer,
				playerID: player.ID,
				respCh:   make(chan response, 1),
				doneCh:   done,
			}
			t.logger.Info("Enqueue operation cancelled", slog.String("player", player.String()))
			return ctx.Err()
		case <-time.After(t.defaultTimeout):
			return fmt.Errorf("enqueue operation timed out")
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ListMatches returns a list of all active matches.
func (t *Tango) ListMatches() ([]*match, error) {
	if !t.started.Load() {
		return nil, errServiceNotStarted
	}

	respCh := make(chan response)

	select {
	case t.opCh <- operation{op: opListMatches, respCh: respCh}:
		resp := <-respCh

		if matches, ok := resp.data.([]*match); ok {
			return matches, nil
		}
		return nil, nil
	case <-t.doneCh:
		return nil, nil
	}
}

// RemovePlayer removes a player from the system.
func (t *Tango) RemovePlayer(playerID string) error {
	if !t.started.Load() {
		return errServiceNotStarted
	}

	respCh := make(chan response)
	t.opCh <- operation{op: opRemovePlayer, playerID: playerID, respCh: respCh}
	resp := <-respCh
	return resp.err
}

// Stats returns current statistics about matches and players
func (t *Tango) Stats(ctx context.Context) (Stats, error) {
	if !t.started.Load() {
		return Stats{}, errServiceNotStarted
	}

	select {
	case <-ctx.Done():
		return Stats{}, ctx.Err()
	case <-t.doneCh:
		return Stats{}, errServiceNotStarted
	default:
		if snapshot := t.statsSnapshot.Load(); snapshot != nil {
			return *snapshot, nil
		}
		return Stats{}, fmt.Errorf("stats not available")
	}
}

// processOperations handles all state-modifying operations
func (t *Tango) processOperations() {
	defer t.wg.Done()

	for {
		select {
		case op := <-t.opCh:
			// Check if service is shutting down
			if !t.started.Load() {
				op.respCh <- response{err: errServiceNotStarted}
				continue
			}

			switch op.op {
			case opEnqueuePlayer:
				t.handleEnqueue(op)
			case opRemovePlayer:
				t.handleRemove(op)
			case opListMatches:
				t.handleList(op)
			}
		case <-t.doneCh:
			return
		}
	}
}

func (t *Tango) isStarted() bool {
	return t.started.Load()
}

func (t *Tango) getAttemptFrequency() time.Duration {
	return t.attemptToJoinFrequency
}

// handleEnqueue adds a player to the system and initiates the matchmaking process.
func (t *Tango) handleEnqueue(op operation) {
	t.state.Lock()
	if _, exists := t.state.players[op.player.ID]; exists {
		t.state.Unlock()
		op.respCh <- response{err: errPlayerAlreadyEnqueued}
		return
	}

	// Store player
	t.state.players[op.player.ID] = op.player
	t.state.Unlock()

	if op.player.IsHosting {
		match := t.createMatch(op.player)
		t.state.Lock()
		t.state.matches[op.player.ID] = match
		t.state.Unlock()
	} else {
		t.matchWorkers.submit(op.player)
	}

	t.logger.Info("Player enqueued", slog.String("player", op.player.String()))
	op.respCh <- response{}
}

// handleRemove removes a player from the system and cleans up any associated matches.
func (t *Tango) handleRemove(op operation) {
	t.state.Lock()

	if _, exists := t.state.players[op.playerID]; !exists {
		t.state.Unlock()
		op.respCh <- response{err: errPlayerNotFound}
		return
	}

	delete(t.state.players, op.playerID)

	t.state.Unlock()

	match, isHost := t.findMatchForPlayer(op.playerID)
	if match != nil {
		if isHost {
			t.removeMatch(match.hostPlayerID)
		} else {
			match.removePlayer(op.playerID)
		}
	}

	t.logger.Info("Player removed", slog.String("player-id", op.playerID))
	op.respCh <- response{}
}

// handleList returns a list of all active matches.
func (t *Tango) handleList(op operation) {
	t.state.RLock()
	matches := make([]*match, 0, len(t.state.matches))
	for _, m := range t.state.matches {
		matches = append(matches, m)
	}
	t.state.RUnlock()
	op.respCh <- response{data: matches}
}

// processTimeouts handles player timeout checks and removes expired players.
func (t *Tango) processTimeouts() {
	defer t.wg.Done()

	ticker := time.NewTicker(t.checkDeadlinesFrequency)
	defer ticker.Stop()

	// TODO: fix magic number
	expired := make([]string, 0, 32)

	for {
		select {
		case <-ticker.C:
			now := time.Now().Unix()

			t.state.RLock()
			for id, player := range t.state.players {
				if now > player.timeout {
					expired = append(expired, id)
				}
			}
			t.state.RUnlock()

			for _, id := range expired {
				t.RemovePlayer(id)
			}
			expired = expired[:0]

		case <-t.doneCh:
			return
		}
	}
}

// findSuitableMatch searches for a match that has the same
// game mode and available slots for the given player.
// TODO: extend this to consider tags
func (t *Tango) findSuitableMatch(player Player) *match {
	t.state.RLock()
	defer t.state.RUnlock()

	for _, m := range t.state.matches {
		m.mu.RLock()
		if m.gameMode == player.GameMode && m.availableSlots > 0 {
			m.mu.RUnlock()
			return m
		}
		m.mu.RUnlock()
	}
	return nil
}

// createMatch creates a new match with the given host player and starts the match goroutine.
func (t *Tango) createMatch(player Player) *match {
	match := t.matchPool.Get().(*match)

	match.mu.Lock()
	match.hostPlayerID = player.ID
	match.availableSlots = uint8(availableSlotsPerGameMode(player.GameMode))
	match.gameMode = player.GameMode
	match.tagCount = uint8(len(player.tags)) // safe since we only store up to 'maxTags'
	match.tagCount = player.tagCount
	copy(match.tags[:], player.tags[:player.tagCount])
	match.joinedPlayers = sync.Map{}
	match.requestCh = make(chan matchRequest)
	match.doneCh = make(chan struct{})
	match.closed.Store(false)
	match.mu.Unlock()

	go match.run()

	return match
}

// removeMatch removes a match from the system and returns it to the match pool.
func (t *Tango) removeMatch(hostPlayerID string) error {
	t.state.Lock()
	match, exists := t.state.matches[hostPlayerID]
	if !exists {
		t.state.Unlock()
		return fmt.Errorf("match not found")
	}
	delete(t.state.matches, hostPlayerID)
	t.state.Unlock()

	match.cleanup()
	t.matchPool.Put(match)
	return nil
}

// findMatchForPlayer finds the match that a player is either hosting or joined.
func (t *Tango) findMatchForPlayer(playerID string) (*match, bool) {
	t.state.RLock()
	defer t.state.RUnlock()

	for _, m := range t.state.matches {
		if m.hostPlayerID == playerID {
			return m, true
		}
		if m.hasPlayer(playerID) {
			return m, false
		}
	}
	return nil, false
}

// cleanupMatchesAndPlayers removes all matches and players from the system during shutdown.
func (t *Tango) cleanupMatchesAndPlayers(ctx context.Context) {
	t.state.Lock()
	defer t.state.Unlock()

	for hostID, match := range t.state.matches {
		select {
		case <-ctx.Done():
			return
		default:
			match.cleanup()
			t.matchPool.Put(match)
			delete(t.state.matches, hostID)
		}
	}

	for playerID := range t.state.players {
		select {
		case <-ctx.Done():
			return
		default:
			delete(t.state.players, playerID)
		}
	}
}

// Add new method to periodically update stats
func (t *Tango) updateStats() {
	defer t.wg.Done()
	ticker := time.NewTicker(t.statsUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			stats := t.collectStats()
			t.statsSnapshot.Store(&stats)
		case <-t.doneCh:
			return
		}
	}
}

// Move stats collection to a separate method
func (t *Tango) collectStats() Stats {
	t.state.RLock()
	stats := Stats{
		MatchesByGameMode: make(map[GameMode]GameModeStats),
		TotalMatches:      len(t.state.matches),
		TotalPlayers:      len(t.state.players),
	}

	// Take a snapshot of matches to minimize lock time
	matches := make([]*match, 0, len(t.state.matches))
	for _, m := range t.state.matches {
		matches = append(matches, m)
	}
	t.state.RUnlock()

	// Initialize stats for each game mode
	for mode := range AllGameModes {
		stats.MatchesByGameMode[mode] = GameModeStats{}
	}

	// Process matches without holding the main lock
	for _, m := range matches {
		m.mu.RLock()
		modeStats := stats.MatchesByGameMode[m.gameMode]
		modeStats.ActiveMatches++
		modeStats.OpenSlots += int(m.availableSlots)

		var playerCount int
		m.joinedPlayers.Range(func(_, _ interface{}) bool {
			playerCount++
			return true
		})
		modeStats.PlayersJoined += playerCount + 1 // +1 for host player

		stats.MatchesByGameMode[m.gameMode] = modeStats
		m.mu.RUnlock()
	}
	return stats
}
