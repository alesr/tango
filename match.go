package tango

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// Maximum number of tags allowed per player/match.
const maxTags = 8

// matchOp represents match operation types
type matchOp int

const (
	matchJoin matchOp = iota
	matchLeave
	matchQuery
)

// matchRequest represents a request to modify match state
type matchRequest struct {
	op       matchOp
	playerID string
	player   Player
	respCh   chan matchResponse
}

// matchResponse carries the result of a match operation
type matchResponse struct {
	success bool
	err     error
}

// match represents a game match with a host, joined players, and game mode details.
type match struct {
	hostPlayerID       string
	joinedPlayers      sync.Map // Use sync.Map instead of map[string]struct{}
	joinedPlayersCount int32    // Keep track of the number of joined players
	availableSlots     uint8
	tags               [maxTags]string
	tagCount           uint8
	gameMode           GameMode

	// Channel coordination
	requestCh chan matchRequest
	doneCh    chan struct{}

	// Protected state
	mu     sync.RWMutex
	closed atomic.Bool // Track if match is closed
}

// newMatch creates a new match instance
func newMatch(host Player, slots uint8) *match {
	m := &match{
		hostPlayerID:   host.ID,
		availableSlots: slots,
		gameMode:       host.GameMode,
		tagCount:       host.tagCount,
		requestCh:      make(chan matchRequest),
		doneCh:         make(chan struct{}),
		closed:         atomic.Bool{},
	}

	// Copy tags safely before starting goroutine
	copy(m.tags[:], host.tags[:host.tagCount])

	return m
}

// run processes match operations
func (m *match) run() {
	defer func() {
		m.mu.Lock()
		close(m.requestCh) // Close request channel last
		m.mu.Unlock()
	}()

	for {
		select {
		case req, ok := <-m.requestCh:
			if !ok {
				return
			}
			switch req.op {
			case matchJoin:
				m.handleJoin(req)
			case matchLeave:
				if m.handleLeave(req) {
					return
				}
			case matchQuery:
				m.handleQuery(req)
			}
		case <-m.doneCh:
			return
		}
	}
}

// handleJoin adds a player to the match if the game mode and available slots match.
func (m *match) handleJoin(req matchRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.gameMode != req.player.GameMode || m.availableSlots == 0 {
		req.respCh <- matchResponse{success: false}
		return
	}

	m.joinedPlayers.Store(req.player.ID, struct{}{})
	atomic.AddInt32(&m.joinedPlayersCount, 1)
	m.availableSlots--
	req.respCh <- matchResponse{success: true}
}

// handleLeave removes a player from the match. If the player is the host, it returns true to signal the match should be closed.
func (m *match) handleLeave(req matchRequest) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if req.playerID == m.hostPlayerID {
		req.respCh <- matchResponse{success: true}
		return true
	}

	if _, exists := m.joinedPlayers.Load(req.playerID); exists {
		m.joinedPlayers.Delete(req.playerID)
		atomic.AddInt32(&m.joinedPlayersCount, -1)
		m.availableSlots++
		req.respCh <- matchResponse{success: true}
		return false
	}

	req.respCh <- matchResponse{success: false}
	return false
}

// handleQuery checks if a player is in the match.
func (m *match) handleQuery(req matchRequest) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.joinedPlayers.Load(req.playerID)
	req.respCh <- matchResponse{success: exists}
}

// tryJoin attempts to add a player to the match
func (m *match) tryJoin(player Player) bool {
	respCh := make(chan matchResponse)
	m.requestCh <- matchRequest{
		op:     matchJoin,
		player: player,
		respCh: respCh,
	}
	resp := <-respCh
	return resp.success
}

// String returns a string representation of the match.
func (m *match) String() string {
	var sb strings.Builder

	m.mu.RLock()
	defer m.mu.RUnlock()

	sb.WriteString(fmt.Sprintf("Host Player ID: '%s'", m.hostPlayerID))
	sb.WriteString(fmt.Sprintf(" Joined Players: '%d'", atomic.LoadInt32(&m.joinedPlayersCount)))
	sb.WriteString(fmt.Sprintf(" Available Slots: '%d'", m.availableSlots))

	activeTags := make([]string, m.tagCount)
	for i := uint8(0); i < m.tagCount; i++ {
		activeTags[i] = m.tags[i]
	}
	sb.WriteString(fmt.Sprintf(" Tags: '%s' ", strings.Join(activeTags, ", ")))
	sb.WriteString(fmt.Sprintf(" Game Mode: '%s'", m.gameMode))

	return sb.String()
}

// cleanup prepares the match for return to pool
func (m *match) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.closed.Swap(true) {
		close(m.doneCh) // Signal run() to stop first

		// Let run() close the request channel
		// to avoid "send on closed channel" panics
	}
}

// hasPlayer checks if a player is in the match
func (m *match) hasPlayer(playerID string) bool {
	respCh := make(chan matchResponse)
	m.requestCh <- matchRequest{
		op:       matchQuery,
		playerID: playerID,
		respCh:   respCh,
	}
	resp := <-respCh
	return resp.success
}

// removePlayer removes a player from the match
func (m *match) removePlayer(playerID string) bool {
	respCh := make(chan matchResponse)
	m.requestCh <- matchRequest{
		op:       matchLeave,
		playerID: playerID,
		respCh:   respCh,
	}
	resp := <-respCh
	return resp.success
}
