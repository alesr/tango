package tango

import "time"

type EventType uint8

const (
	EventPlayerJoinedQueue EventType = iota + 1
	EventPlayerLeftQueue
	EventPlayerJoinedMatch
	EventPlayerSearchExpired
	EventMatchCreated
	EventMatchCompleted
	EventMatchPopulated // All slots filled
	EventMatchPlayerLeft
	EventMatchExpired
)

type Event struct {
	Type      EventType
	Timestamp time.Time
	PlayerID  string
	MatchID   string
	GameMode  GameMode
	Data      map[string]any // Additional context-specific data
}

// String returns a human-readable representation of the event type
func (et EventType) String() string {
	switch et {
	case EventPlayerJoinedQueue:
		return "PlayerJoinedQueue"
	case EventPlayerLeftQueue:
		return "PlayerLeftQueue"
	case EventPlayerJoinedMatch:
		return "PlayerJoinedMatch"
	case EventPlayerSearchExpired:
		return "PlayerSearchExpired"
	case EventMatchCreated:
		return "MatchCreated"
	case EventMatchCompleted:
		return "MatchCompleted"
	case EventMatchPopulated:
		return "MatchPopulated"
	case EventMatchPlayerLeft:
		return "MatchPlayerLeft"
	case EventMatchExpired:
		return "MatchExpired"
	default:
		return "Unknown"
	}
}
