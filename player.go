package tango

import (
	"fmt"
	"strings"
	"time"
)

// Player represents a player attempting to join or host a match.
type Player struct {
	ID        string
	IsHosting bool
	GameMode  GameMode
	tags      [maxTags]string
	tagCount  uint8
	timeout   int64
}

// NewPlayer creates a new Player instance.
func NewPlayer(id string, isHosting bool, gameMode GameMode, timeout time.Time, tags []string) Player {
	p := Player{
		ID:        id,
		IsHosting: isHosting,
		GameMode:  gameMode,
		timeout:   timeout.Unix(),
	}

	for i := 0; i < len(tags) && i < maxTags; i++ {
		p.tags[i] = tags[i]
		p.tagCount++
	}
	return p
}

// String returns a string representation of the Player.
func (p Player) String() string {
	tags := strings.Join(p.tags[:p.tagCount], ", ")
	return fmt.Sprintf("Player(ID: %s, IsHosting: %t, GameMode: %s, Timeout: %s, Tags: [%s])",
		p.ID, p.IsHosting, p.GameMode, time.Unix(p.timeout, 0).Format(time.RFC3339), tags)
}
