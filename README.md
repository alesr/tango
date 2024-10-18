# Tango: Matchmaking System

[![codecov](https://codecov.io/gh/alesr/tango/branch/main/graph/badge.svg)](https://codecov.io/gh/alesr/tango)

Tango is a simple matchmaking system for players looking to host or join matches in different game modes. It handles queueing players, creating matches, managing available slots, and removing players when necessary. Tango is designed for flexibility and efficiency, supporting multiple game modes (such as 1v1, 2v2, and 3v3) and ensuring thread safety with `sync.Map` and atomic operations.

## Features

- **Player Queueing**: Players can be enqueued to either host or join a match.
- **Match Creation**: Hosting players automatically create matches with available slots based on the game mode.
- **Match Joining**: Non-hosting players attempt to join matches with compatible game modes and available slots.
- **Player Timeout**: Players are automatically removed from the queue or match if their deadline expires.
- **Automatic Cleanup**: When a host player leaves, all players in the match are removed, and the match is deleted.
- **Thread Safety**: `sync.Map` is used for concurrent player and match management, ensuring that the system handles high concurrency gracefully.

## Game Modes

The system currently supports three game modes:

- `1v1`: One available slot for a joining player.
- `2v2`: Three available slots for joining players.
- `3v3`: Five available slots for joining players.

## Usage

### Creating a Tango Instance

To create a new instance of the Tango matchmaking system, you need to provide a logger and a queue size for managing players:

```go
logger := slog.New(slog.NewTextHandler(os.Stdout))
tango := tango.New(logger, 100) // 100 is the player queue size
```

## Enqueuing Players

Players are enqueued with their details. A player can either host a match or join an existing one based on their IsHosting flag:

```go
player := tango.Player{
    IP:        "192.168.1.10",
    IsHosting: true, // This player will host a match
    Tags:      []string{"competitive"},
    GameMode:  tango.GameMode1v1,
    Deadline:  time.Now().Add(2 * time.Minute), // 2-minute timeout
}

if err := tango.Enqueue(player); err != nil {
    log.Println("Error enqueuing player:", err)
}
```

## Listing Active Matches

You can retrieve a list of currently active matches:

```go
matches := tango.ListMatches()
for _, match := range matches {
    fmt.Printf("Match hosted by: %s with %d available slots\n", match.hostPlayerIP, match.getAvailableSlots())
}
```

## Removing Players

Players can be removed from the queue or their current match by specifying their IP address:

```go
if err := tango.RemovePlayer("192.168.1.10"); err != nil {
    log.Println("Error removing player:", err)
}
```

When a host is removed, all players in that match are automatically removed as well.

## Player Timeout

If a player exceeds their specified deadline without joining a match, they are automatically removed from the system. The timeout check runs continuously in the background.

## Internals

### Match Structure

A Match contains:

	•	hostPlayerIP: The IP address of the hosting player.
	•	joinedPlayers: A sync.Map of players who have joined the match.
	•	availableSlots: The number of available slots left in the match.
	•	tags: Metadata tags that can be used to filter matches.
	•	gameMode: The game mode of the match (1v1, 2v2, or 3v3).

### Player Structure

A Player contains:

	•	IP: The player’s unique IP address.
	•	IsHosting: A flag indicating if the player is hosting a match.
	•	Tags: Metadata tags for matchmaking purposes.
	•	GameMode: The game mode the player is interested in.
	•	Deadline: The time after which the player should be removed from the system if not matched.

### Concurrency

The system uses `sync.Map` for thread-safe storage of players and matches, ensuring that operations like player enqueueing, match creation, and player removal can be done concurrently without race conditions.


## Future Enhancements

- Match Filtering: Add advanced filtering options based on tags or player preferences.
- Load Balancing: Distribute players across multiple matches more evenly when there are many hosting players.
- Metrics: Collect statistics on match times, player wait times, and success rates to optimize the system.
- Improve test coverage.
- Memory profilling.
- CI.
- Use `sync.Pool` is used to manage matches.
