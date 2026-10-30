package clop

import (
	"net/http"
	"sync"
	"time"
)

const (
	sessionCookie = "CLOPSESSID"
	sessionIdle   = 24 * time.Hour
)

// Session is a server-side bag of string values, the counterpart of $_SESSION.
type Session struct {
	id       string
	mu       sync.Mutex
	vals     map[string]string
	lastSeen time.Time
}

func (s *Session) Get(k string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vals[k]
}

func (s *Session) Int(k string) int64 { return toInt(s.Get(k)) }

func (s *Session) Set(k string, v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vals[k] = str(v)
}

func (s *Session) Del(k string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.vals, k)
}

func (s *Session) snapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.vals))
	for k, v := range s.vals {
		out[k] = v
	}
	return out
}

// SessionStore keeps sessions in memory. Like PHP's default file sessions in
// the old container, they do not survive a restart.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*Session
	secure   bool
}

func NewSessionStore(secureCookies bool) *SessionStore {
	st := &SessionStore{sessions: map[string]*Session{}, secure: secureCookies}
	go st.reap()
	return st
}

func (st *SessionStore) reap() {
	for range time.Tick(10 * time.Minute) {
		st.mu.Lock()
		for id, s := range st.sessions {
			if time.Since(s.lastSeen) > sessionIdle {
				delete(st.sessions, id)
			}
		}
		st.mu.Unlock()
	}
}

// Load returns the request's session, creating one (and its cookie) if needed.
func (st *SessionStore) Load(w http.ResponseWriter, r *http.Request) *Session {
	st.mu.Lock()
	defer st.mu.Unlock()
	if c, err := r.Cookie(sessionCookie); err == nil {
		if s, ok := st.sessions[c.Value]; ok {
			s.lastSeen = time.Now()
			return s
		}
	}
	s := &Session{id: newToken(), vals: map[string]string{}, lastSeen: time.Now()}
	st.sessions[s.id] = s
	st.setCookie(w, s.id)
	return s
}

// Regenerate swaps the session id, keeping the values (used on login).
func (st *SessionStore) Regenerate(w http.ResponseWriter, s *Session) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.sessions, s.id)
	s.id = newToken()
	st.sessions[s.id] = s
	st.setCookie(w, s.id)
}

// Destroy forgets every value in the session.
func (st *SessionStore) Destroy(s *Session) {
	st.mu.Lock()
	delete(st.sessions, s.id)
	st.mu.Unlock()
	s.mu.Lock()
	s.vals = map[string]string{}
	s.mu.Unlock()
}

func (st *SessionStore) setCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   st.secure,
		SameSite: http.SameSiteLaxMode,
	})
}
