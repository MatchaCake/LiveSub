package bot

import "sync"

// Pool manages a collection of bots, indexed by name.
type Pool struct {
	mu   sync.RWMutex
	bots map[string]Bot
}

// NewPool creates an empty bot pool.
func NewPool() *Pool {
	return &Pool{bots: make(map[string]Bot)}
}

// Add registers a bot by name.
func (p *Pool) Add(b Bot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.bots[b.Name()] = b
}

// Get returns a bot by name.
func (p *Pool) Get(name string) Bot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.bots[name]
}

// Remove removes a bot by name.
func (p *Pool) Remove(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.bots, name)
}

// All returns all bots.
func (p *Pool) All() []Bot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Bot, 0, len(p.bots))
	for _, b := range p.bots {
		out = append(out, b)
	}
	return out
}

// Names returns all bot names.
func (p *Pool) Names() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, 0, len(p.bots))
	for name := range p.bots {
		out = append(out, name)
	}
	return out
}
