package async

import "sync"

type MutexExec[T any] struct {
	mu    sync.Mutex
	value T
}

func NewMutexExec[T any](value T) *MutexExec[T] {
	return &MutexExec[T]{value: value}
}

func (m *MutexExec[T]) Exec(fn func(T) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn(m.value)
}

type MutexGetterSetter[T any] struct {
	mu    sync.Mutex
	value T
	set   bool
}

func NewMutexGetterSetter[T any](value T) *MutexGetterSetter[T] {
	return &MutexGetterSetter[T]{value: value, set: true}
}

func (m *MutexGetterSetter[T]) Get() (T, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.value, m.set
}

func (m *MutexGetterSetter[T]) Set(value T) (T, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldValue, wasSet := m.value, m.set
	m.value, m.set = value, true
	return oldValue, wasSet
}

func (m *MutexGetterSetter[T]) SetWithCond(value T, cv *CondVar) (T, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldValue, wasSet := m.value, m.set
	m.value, m.set = value, true
	cv.Signal()
	return oldValue, wasSet
}
