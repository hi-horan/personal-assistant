package store

import (
	"iter"
	"sort"
	"time"

	"google.golang.org/adk/session"
)

type Session struct {
	IDVal         string
	AppNameVal    string
	UserIDVal     string
	Title         string
	StateVal      State
	EventsVal     Events
	LastUpdateVal time.Time
}

func (s *Session) ID() string {
	return s.IDVal
}

func (s *Session) AppName() string {
	return s.AppNameVal
}

func (s *Session) UserID() string {
	return s.UserIDVal
}

func (s *Session) State() session.State {
	return s.StateVal
}

func (s *Session) Events() session.Events {
	return s.EventsVal
}

func (s *Session) LastUpdateTime() time.Time {
	return s.LastUpdateVal
}

type State map[string]any

func (s State) Get(key string) (any, error) {
	value, ok := s[key]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}
	return value, nil
}

func (s State) Set(key string, value any) error {
	s[key] = value
	return nil
}

func (s State) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		keys := make([]string, 0, len(s))
		for key := range s {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if !yield(key, s[key]) {
				return
			}
		}
	}
}

type Events []*session.Event

func (e Events) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, event := range e {
			if !yield(event) {
				return
			}
		}
	}
}

func (e Events) Len() int {
	return len(e)
}

func (e Events) At(i int) *session.Event {
	return e[i]
}
