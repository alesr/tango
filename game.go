package tango

// GameMode represents different types of game modes available
type GameMode uint8

const (
	GameModeUnknown GameMode = iota
	GameMode1v1
	GameMode2v2
	GameMode3v3
)

// String returns the string representation of a GameMode
func (g GameMode) String() string {
	switch g {
	case GameMode1v1:
		return "1v1"
	case GameMode2v2:
		return "2v2"
	case GameMode3v3:
		return "3v3"
	default:
		return "Unknown"
	}
}

// AllGameModes contains a map of all available game modes
var AllGameModes = map[GameMode]struct{}{
	GameModeUnknown: {},
	GameMode1v1:     {},
	GameMode2v2:     {},
	GameMode3v3:     {},
}

// availableSlotsPerGameMode returns the number of available slots for a game mode
func availableSlotsPerGameMode(mode GameMode) int {
	switch mode {
	case GameMode1v1:
		return 1
	case GameMode2v2:
		return 3
	case GameMode3v3:
		return 5
	default:
		return 0
	}
}
