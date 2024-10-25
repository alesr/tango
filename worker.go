package tango

import (
	"context"
	"sync"
	"time"
)

// matchFinder defines the interface for finding and joining matches
type matchFinder interface {
	findSuitableMatch(player Player) *match
	RemovePlayer(playerID string) error
	isStarted() bool
	getAttemptFrequency() time.Duration
}

// matchJob represents a job for finding a match for a player
type matchJob struct {
	player   Player
	deadline time.Time
}

// matchWorkerPool manages a pool of workers for matchmaking
type matchWorkerPool struct {
	numWorkers int
	jobCh      chan matchJob
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	finder     matchFinder
}

// newMatchWorkerPool creates a new worker pool for matchmaking
func newMatchWorkerPool(numWorkers int, jobBuffer int, finder matchFinder) *matchWorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	pool := &matchWorkerPool{
		numWorkers: numWorkers,
		jobCh:      make(chan matchJob, jobBuffer),
		ctx:        ctx,
		cancel:     cancel,
		finder:     finder,
	}

	pool.start()
	return pool
}

// start initializes and starts the worker pool
func (p *matchWorkerPool) start() {
	p.wg.Add(p.numWorkers)
	for i := 0; i < p.numWorkers; i++ {
		go p.worker()
	}
}

// shutdown gracefully shuts down the worker pool
func (p *matchWorkerPool) shutdown() {
	p.cancel()
	close(p.jobCh)
	p.wg.Wait()
}

// submit adds a new matchmaking job to the pool
func (p *matchWorkerPool) submit(player Player) {
	deadline := time.Unix(player.timeout, 0)
	select {
	case p.jobCh <- matchJob{player: player, deadline: deadline}:
	case <-p.ctx.Done():
		return
	}
}

// worker processes matchmaking jobs
func (p *matchWorkerPool) worker() {
	defer p.wg.Done()

	for {
		select {
		case job, ok := <-p.jobCh:
			if !ok {
				return
			}
			p.processJob(job)
		case <-p.ctx.Done():
			return
		}
	}
}

// processJob handles the matchmaking attempt for a single player
func (p *matchWorkerPool) processJob(job matchJob) {
	ticker := time.NewTicker(p.finder.getAttemptFrequency())
	defer ticker.Stop()

	timeoutCh := time.After(time.Until(job.deadline))

	for {
		select {
		case <-timeoutCh:
			_ = p.finder.RemovePlayer(job.player.ID)
			return
		case <-ticker.C:
			if !p.finder.isStarted() {
				return
			}

			if match := p.finder.findSuitableMatch(job.player); match != nil {
				respCh := make(chan matchResponse)

				select {
				case match.requestCh <- matchRequest{
					op:     matchJoin,
					player: job.player,
					respCh: respCh,
				}:
					select {
					case resp := <-respCh:
						if resp.success {
							return
						}
					case <-p.ctx.Done():
						return
					}
				case <-p.ctx.Done():
					return
				}
			}
		case <-p.ctx.Done():
			return
		}
	}
}
