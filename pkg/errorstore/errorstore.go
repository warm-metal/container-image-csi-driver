package errorstore

import "sync"

type ErrorStore struct {
	store map[string]error
	mutex *sync.Mutex
}

func New() *ErrorStore {
	return &ErrorStore{
		mutex: &sync.Mutex{},
		store: make(map[string]error),
	}
}

func (a *ErrorStore) Put(key string, err error) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.store[key] = err

	return err
}

func (a *ErrorStore) Get(key string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.store[key]
}

func (a *ErrorStore) Remove(key string) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	delete(a.store, key)
}
