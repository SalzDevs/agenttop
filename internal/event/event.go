package event

import "time"

type Event struct {
	ID               int64
	TraceID          int64
	Time             time.Time
	Provider         string
	Model            string
	Endpoint         string
	Method           string
	Status           int
	Streaming        bool
	Duration         time.Duration
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	CostUSD          float64
	PromptPreview    string
	ResponsePreview  string
	Err              string
}

func (e Event) InFlight() bool { return e.Status == 0 && e.Err == "" }

type Bus struct {
	subs []chan Event
}

func NewBus() *Bus { return &Bus{} }

func (b *Bus) Subscribe() chan Event {
	ch := make(chan Event, 64)
	b.subs = append(b.subs, ch)
	return ch
}

func (b *Bus) Emit(e Event) {
	for _, s := range b.subs {
		select {
		case s <- e:
		default:
		}
	}
}
