package tango

import (
	"fmt"
	"strings"
	"sync"
)

// Maximum number of tags allowed per player/match.
const maxTags = 8

// match represents a game match with a host,
// joined players, and game mode details.
type match struct {
	hostPlayerID   string
	joinedPlayers  map[string]struct{}
	availableSlots uint8
	tags           [maxTags]string
	tagCount       uint8
	gameMode       GameMode
	mu             sync.RWMutex
}

// String returns a string representation of the match.
func (m *match) String() string {
	var sb strings.Builder

	m.mu.RLock()
	sb.WriteString(fmt.Sprintf("Host Player ID: '%s'", m.hostPlayerID))
	sb.WriteString(fmt.Sprintf(" Joined Players: '%d'", len(m.joinedPlayers)))
	sb.WriteString(fmt.Sprintf(" Available Slots: '%d'", m.availableSlots))

	activeTags := make([]string, m.tagCount)
	for i := uint8(0); i < m.tagCount; i++ {
		activeTags[i] = m.tags[i]
	}
	sb.WriteString(fmt.Sprintf(" Tags: '%s' ", strings.Join(activeTags, ", ")))
	sb.WriteString(fmt.Sprintf(" Game Mode: '%s'", m.gameMode))
	m.mu.RUnlock()

	return sb.String()
}

func (m *match) tryJoin(player Player) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.gameMode != player.GameMode || m.availableSlots == 0 {
		return false
	}

	m.joinedPlayers[player.ID] = struct{}{}
	m.availableSlots--
	return true
}
