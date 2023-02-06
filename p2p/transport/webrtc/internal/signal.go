package internal

import "sync"

type Signal struct {
	sync.Mutex
	c chan struct{}
}

func (s *Signal) Wait() <-chan struct{} {
	s.Lock()
	defer s.Unlock()
	return s.c
}

func (s *Signal) Signal() {
	s.Lock()
	c := s.c
	s.c = make(chan struct{})
	s.Unlock()
	close(c)
}
