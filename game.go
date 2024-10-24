package tango

const (
	// GameMode1v1 represents a 1v1 match type.
	GameMode1v1 GameMode = "1v1"
	// GameMode2v2 represents a 2v2 match type.
	GameMode2v2 GameMode = "2v2"
	// GameMode3v3 represents a 3v3 match type.
	GameMode3v3 GameMode = "3v3"
)

// GameMode defines different game modes for matches.
// The game mode determines the number of available slots in a match,
// with the host always occupying one slot.
type GameMode string

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
