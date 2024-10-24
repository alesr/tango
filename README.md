# Tango
[![codecov](https://codecov.io/gh/alesr/tango/branch/main/graph/badge.svg)](https://codecov.io/gh/alesr/tango)

An experimental matchmaking service written in Go that I built for fun. It handles player queues and match creation with a focus on concurrent operations.

## ⚠️ Heads Up

This is an **experimental** project. I built it to explore some ideas around matchmaking and concurrent patterns in Go. While it works, I wouldn't recommend using it in production just yet. Feel free to poke around and maybe grab some ideas for your own projects.

## How Tango Works

### Flow Overview

When you start Tango, it spins up two main background processes:
1. A queue processor that handles new players/hosts
2. A timeout checker that removes players with the expired matching timeouts

```
                           ┌── Host → Creates new match
Player Enqueue → Queue ────┤
                           └── Player → Attempts to join existing match
```

Players can be either hosts (create matches) or joiners (look for matches). When a host joins:

A match is created immediately
Tango starts actively looking for suitable players to join this match

When a regular player joins, Tango keeps trying to find a suitable match until either:

- A match is found
- The player times out
- The context is cancelled

```mermaid
%%{init: {'theme': 'base', 'themeVariables': { 'fontFamily': 'arial', 'fontSize': '16px', 'textColor': '#2A4365' }}}%%
flowchart TB
    subgraph Matchmaking
        Start[Start Tango] --> Queue{Player Queue}
        Queue -->|Host| CreateMatch[Create Match]
        CreateMatch --> SeekPlayers[Seek Players]
        Queue -->|Player| FindMatch[Find Match]
        FindMatch -->|Success| JoinMatch[Join Match]
        FindMatch -->|Retry| FindMatch
        SeekPlayers -->|Match Found| JoinMatch
    end

    subgraph Cleanup
        Timer[Timeout Checker] --> CheckPlayers{Check Players}
        CheckPlayers -->|Expired| RemovePlayer[Remove Player]
        CheckPlayers -->|Active| Continue[Continue Checking]
        RemovePlayer -->|Is Host| CleanMatch[Cleanup Match]
        RemovePlayer -->|Not Host| DeletePlayer[Delete Player]
        CleanMatch --> ReturnMatchPool[Return Match to Pool]
    end

    style Start fill:#48BB78,color:#2F855A
    style Queue fill:#667EEA,color:#F7FAFC
    style Timer fill:#4299E1,color:#F7FAFC
    style JoinMatch fill:#48BB78,color:#2F855A
    style ReturnMatchPool fill:#ED64A6,color:#F7FAFC
    style CleanMatch fill:#9F7AEA,color:#F7FAFC
    style DeletePlayer fill:#ED8936,color:#F7FAFC
    style Matchmaking fill:#EBF8FF,color:#2C5282
    style Cleanup fill:#F0FFF4,color:#2C5282
```

### Setup and Lifecycle

```go
import "github.com/alesr/tango"

// Create a new Tango instance
tango := tango.New(
    tango.WithQueueSize(100),
    tango.WithDefaultTimeout(5*time.Second),
    // More options available...
)

// Start the service
err := tango.Start()

// Shut it down!
err := tango.Shutdown(ctx)
```

### Player Management

```go
// Create a new player
player := tango.NewPlayer(
    "player-1",     // ID
    false,          // IsHost
    tango.Mode1v1,  // Game mode
    deadline,       // Timeout (how long it should look for a match)
    []string{"tag"} // Optional tags (not in use yet)
)

// Add to matchmaking queue
err := tango.Enqueue(ctx, player)

// Remove from system
err := tango.RemovePlayer("player-1")
```

### Match Operations

```go
// Get all active matches
matches := tango.ListMatches()

// Remove a player from match/queue
err := tango.RemovePlayer("player-1")
```

## Configuration Options

- `WithLogger`: Custom logger for the service
- `WithQueueSize`: Size of the player queue
- `WithAttemptToJoinFrequency`: How often to try matching players
- `WithCheckDeadlinesFrequency`: How often to check for timeouts
- `WithDefaultTimeout`: Default operation timeout

## Game Modes

- `GameMode1v1`: 1 host + 1 player
- `GameMode2v2`: 1 host + 3 players
- `GameMode3v3`: 1 host + 5 players

Player timeouts and match assignment are handled automatically in the background. The service is entirely concurrent, so all operations are safe to call from multiple goroutines.

## Performance

I've included some benchmarks for exploration, but given the concurrent nature of the system, take the numbers with a grain of salt. If you're curious:

```bash
make bench      # Run benchmarks
make pprof-cpu  # CPU profile analysis
make pprof-mem  # Memory profile analysis
```

