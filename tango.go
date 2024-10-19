package tango

import (
	"errors"
	"log/slog"
	"sync"
	"time"
)

type GameMode string

const (
	GameMode1v1 GameMode = "1v1"
	GameMode2v2 GameMode = "2v2"
	GameMode3v3 GameMode = "3v3"

	attemptToJoinMatchFrequency = time.Millisecond * 500
	checkDeadlinesFrequency     = time.Second
)

type Match struct {
	hostPlayerIP   string
	joinedPlayers  sync.Map
	availableSlots int
	tags           []string
	gameMode       GameMode
	mu             sync.RWMutex
}

type Player struct {
	IP        string
	IsHosting bool
	Tags      []string
	GameMode  GameMode
	Deadline  time.Time
}

type Tango struct {
	logger      *slog.Logger
	players     sync.Map
	matches     sync.Map
	playerQueue chan Player
	mu          sync.Mutex
}

// New creates a new Tango instance.
func New(logger *slog.Logger, queueSize int) *Tango {
	t := &Tango{
		logger:      logger.WithGroup("tango"),
		playerQueue: make(chan Player, queueSize),
	}

	go t.processQueue()
	go t.checkDeadlines()

	return t
}

// Enqueue adds a player to the matchmaking queue.
func (t *Tango) Enqueue(player Player) error {
	if _, found := t.players.LoadOrStore(player.IP, player); found {
		return errors.New("player already enqueued")
	}
	t.playerQueue <- player
	return nil
}

func (t *Tango) ListMatches() []*Match {
	var matches []*Match
	t.matches.Range(func(_, value any) bool {
		match := value.(*Match)
		matches = append(matches, match)
		return true
	})
	return matches
}

// RemovePlayer removes a player from matchmaking.
func (t *Tango) RemovePlayer(playerIP string) error {
	if _, found := t.players.Load(playerIP); !found {
		return errors.New("player not found")
	}

	t.players.Delete(playerIP)

	matchToRemove, isHost := t.findMatchForPlayer(playerIP)

	if isHost && matchToRemove != nil {
		t.removeMatch(matchToRemove.hostPlayerIP)
	} else if matchToRemove != nil {
		t.removePlayer(playerIP)
	}

	return nil
}

func (t *Tango) findMatchForPlayer(playerIP string) (*Match, bool) {
	var (
		matchToRemove *Match
		isHost        bool
	)

	t.mu.Lock()
	defer t.mu.Unlock()

	t.matches.Range(func(_, value any) bool {
		match := value.(*Match)

		if match.hostPlayerIP == playerIP {
			isHost = true
			matchToRemove = match
			return false
		}

		match.mu.Lock()
		defer match.mu.Unlock()

		if _, ok := match.joinedPlayers.Load(playerIP); ok {
			match.joinedPlayers.Delete(playerIP)
			match.availableSlots++

			if match.availableSlots == availableSlotsPerGameMode(match.gameMode) {
				matchToRemove = match
				return false
			}
		}
		return true
	})

	return matchToRemove, isHost
}

func (t *Tango) removePlayer(playerIP string) {
	t.matches.Delete(playerIP)
}

func (t *Tango) removeMatch(hostPlayerIP string) error {
	if _, found := t.matches.Load(hostPlayerIP); !found {
		return errors.New("match not found for removal")
	}

	// Get the match and remove all joined players
	match, _ := t.matches.Load(hostPlayerIP)
	matchInstance := match.(*Match)

	// Lock before accessing joinedPlayers
	matchInstance.mu.Lock()
	defer matchInstance.mu.Unlock()

	// Remove each player from the players map
	matchInstance.joinedPlayers.Range(func(key, _ any) bool {
		playerIP := key.(string)
		t.players.Delete(playerIP)
		return true
	})

	t.matches.Delete(hostPlayerIP)
	t.players.Delete(hostPlayerIP)

	return nil
}

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

func (t *Tango) processQueue() {
	for player := range t.playerQueue {
		if player.IsHosting {
			t.createMatch(player)
		} else {
			go t.attemptToJoinMatch(player)
		}
	}
}

func (t *Tango) attemptToJoinMatch(player Player) {
	deadline := time.After(time.Until(player.Deadline))

	ticker := time.NewTicker(attemptToJoinMatchFrequency)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.handlePlayerTimeout(player)
			return
		case <-ticker.C:
			if t.tryJoinMatch(player) {
				return
			}
		}
	}
}

func (t *Tango) tryJoinMatch(player Player) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	var matchFound bool

	t.matches.Range(func(_, value any) bool {
		match := value.(*Match)

		match.mu.Lock()
		defer match.mu.Unlock()

		if match.gameMode == player.GameMode && match.availableSlots > 0 {
			match.joinedPlayers.Store(player.IP, player)
			match.availableSlots--
			matchFound = true
			return false
		}
		return true
	})

	if !matchFound {
		t.logger.Info("No available matches found, retrying...", slog.String("playerIP", player.IP))
	}
	return matchFound
}

func (t *Tango) handlePlayerTimeout(player Player) {
	t.RemovePlayer(player.IP)
}

func (t *Tango) checkDeadlines() {
	ticker := time.NewTicker(checkDeadlinesFrequency)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		t.players.Range(func(_, value any) bool {
			player := value.(Player)

			if now.After(player.Deadline) {
				t.RemovePlayer(player.IP)
			}
			return true
		})
	}
}

func (t *Tango) createMatch(player Player) {
	match := &Match{
		hostPlayerIP:   player.IP,
		joinedPlayers:  sync.Map{},
		availableSlots: availableSlotsPerGameMode(player.GameMode),
		tags:           player.Tags,
		gameMode:       player.GameMode,
	}

	t.matches.Store(player.IP, match)
}
