package pioneer

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/EraldCaka/PIoneer/pkg/config"
)

const watchInterval = 50 * time.Millisecond

type watcher struct {
	mu       sync.Mutex
	watchers map[int]*pinWatcher
	pool     *sshPool
	log      *zap.Logger
}

type pinWatcher struct {
	pin  int
	ch   chan config.PinEvent
	stop chan struct{}
	last int
}

func newWatcher(pool *sshPool, log *zap.Logger) *watcher {
	return &watcher{
		watchers: make(map[int]*pinWatcher),
		pool:     pool,
		log:      log,
	}
}

func (w *watcher) Watch(pin int, readFn func(int) (int, error)) (<-chan config.PinEvent, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.watchers[pin]; exists {
		return nil, fmt.Errorf("pin %d is already being watched", pin)
	}

	initial, err := readFn(pin)
	if err != nil {
		return nil, fmt.Errorf("failed to read initial value for pin %d: %v", pin, err)
	}

	pw := &pinWatcher{
		pin:  pin,
		ch:   make(chan config.PinEvent, 16),
		stop: make(chan struct{}),
		last: initial,
	}
	w.watchers[pin] = pw

	go w.poll(pw, readFn)
	w.log.Info("watching pin", zap.Int("pin", pin), zap.Int("initial_value", initial))
	return pw.ch, nil
}

func (w *watcher) poll(pw *pinWatcher, readFn func(int) (int, error)) {
	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()
	defer close(pw.ch)

	for {
		select {
		case <-pw.stop:
			w.log.Info("stopped watching pin", zap.Int("pin", pw.pin))
			return
		case <-ticker.C:
			val, err := readFn(pw.pin)
			if err != nil {
				w.log.Warn("watch read error",
					zap.Int("pin", pw.pin),
					zap.Error(err),
				)
				continue
			}
			if val != pw.last {
				event := config.PinEvent{
					Pin:      pw.pin,
					OldValue: pw.last,
					NewValue: val,
				}
				select {
				case pw.ch <- event:
					w.log.Debug("pin changed",
						zap.Int("pin", pw.pin),
						zap.Int("old", pw.last),
						zap.Int("new", val),
					)
				default:
					w.log.Warn("pin event channel full, dropping event",
						zap.Int("pin", pw.pin),
					)
				}
				pw.last = val
			}
		}
	}
}

func (w *watcher) StopWatch(pin int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	pw, exists := w.watchers[pin]
	if !exists {
		return
	}
	close(pw.stop)
	delete(w.watchers, pin)
}

func (w *watcher) StopAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for pin, pw := range w.watchers {
		close(pw.stop)
		delete(w.watchers, pin)
	}
}

func (w *watcher) ActiveCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.watchers)
}
