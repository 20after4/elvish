// Package prompt provides an implementation of the core.Prompt interface.
package prompt

import (
	"os"
	"sync"
	"time"

	"github.com/elves/elvish/styled"
)

type Prompt struct {
	config Config

	// Working directory when prompt was last updated.
	lastWd string
	// Channel for update requests.
	updateReq chan struct{}
	// Channel on which prompt contents are delivered.
	ch chan styled.Text
	// Last computed prompt content.
	last styled.Text
	// Mutex for guarding access to the last field.
	lastMutex sync.RWMutex
}

var unknownContent = styled.Unstyled("???> ")

func defaultStaleTransform(t styled.Text) styled.Text {
	return styled.Transform(t, "inverse")
}

// New makes a new prompt.
func New(fn func() styled.Text) *Prompt {
	p := &Prompt{
		Config{Raw: RawConfig{
			fn, defaultStaleTransform, 2 * time.Millisecond, 5}},
		"", make(chan struct{}, 1), make(chan styled.Text, 1),
		unknownContent, sync.RWMutex{}}
	// TODO: Don't keep a goroutine running.
	go p.loop()
	return p
}

// Config returns the config for the prompt.
func (p *Prompt) Config() *Config {
	return &p.config
}

func (p *Prompt) loop() {
	content := unknownContent
	ch := make(chan styled.Text)
	for range p.updateReq {
		go func() {
			ch <- p.config.Compute()()
		}()

		select {
		case <-time.After(p.config.StaleThreshold()):
			// The prompt callback did not finish within the threshold. Send the
			// previous content, marked as stale.
			p.send(p.config.StaleTransform()(content))
			content = <-ch

			select {
			case <-p.updateReq:
				// If another update is already requested by the time we finish,
				// keep marking the prompt as stale. This reduces flickering.
				p.send(p.config.StaleTransform()(content))
				p.queueUpdate()
			default:
				p.send(content)
			}
		case content = <-ch:
			p.send(content)
		}
	}
}

func (p *Prompt) Trigger(force bool) {
	if force || p.shouldUpdate() {
		p.queueUpdate()
	}
}

func (p *Prompt) Get() styled.Text {
	p.lastMutex.RLock()
	defer p.lastMutex.RUnlock()
	return p.last
}

func (p *Prompt) LateUpdates() <-chan styled.Text {
	return p.ch
}

func (p *Prompt) queueUpdate() {
	select {
	case p.updateReq <- struct{}{}:
	default:
	}
}

func (p *Prompt) send(content styled.Text) {
	p.lastMutex.Lock()
	p.last = content
	p.lastMutex.Unlock()
	p.ch <- content
}

func (p *Prompt) shouldUpdate() bool {
	eagerness := p.config.Eagerness()
	if eagerness >= 10 {
		return true
	}
	if eagerness >= 5 {
		wd, err := os.Getwd()
		if err != nil {
			wd = "error"
		}
		oldWd := p.lastWd
		p.lastWd = wd
		return wd != oldWd
	}
	return false
}