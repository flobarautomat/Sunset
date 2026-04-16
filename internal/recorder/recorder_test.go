package recorder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	sessions map[string]Session
	events   map[string][]Event
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		sessions: make(map[string]Session),
		events:   make(map[string][]Event),
	}
}

func (f *fakeStore) CreateSession(s Session) error {
	f.sessions[s.ID] = s
	return nil
}

func (f *fakeStore) AppendEvents(sessionID string, events []Event) error {
	f.events[sessionID] = append(f.events[sessionID], events...)
	return nil
}

func (f *fakeStore) UpdateLastSeen(sessionID string, at int64) error {
	if s, ok := f.sessions[sessionID]; ok {
		s.LastSeenAt = at
		f.sessions[sessionID] = s
	}
	return nil
}

func TestRecorder_CreateSession(t *testing.T) {
	store := newFakeStore()
	rec := New(store)

	id, err := rec.CreateSession("Mozilla/5.0", "default")
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	assert.Contains(t, store.sessions, id)
	assert.Equal(t, "default", store.sessions[id].VideoID)
	assert.Equal(t, "Mozilla/5.0", store.sessions[id].UserAgent)
}

func TestRecorder_BatchesHeartbeats(t *testing.T) {
	store := newFakeStore()
	rec := New(store)

	id, _ := rec.CreateSession("ua", "default")

	// 5 heartbeats within 5 seconds should collapse to 1.
	events := make([]Event, 5)
	baseTime := int64(1000)
	for i := range events {
		events[i] = Event{Kind: "heartbeat", At: baseTime + int64(i)*1000} // 1s apart
	}

	err := rec.RecordEvents(id, events)
	require.NoError(t, err)
	assert.Len(t, store.events[id], 1)
}

func TestRecorder_RejectsUnknownKind(t *testing.T) {
	store := newFakeStore()
	rec := New(store)

	id, _ := rec.CreateSession("ua", "default")

	err := rec.RecordEvents(id, []Event{
		{Kind: "invalid_kind", At: 1000},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown event kind")
}

func TestRecorder_ClampsVideoPos(t *testing.T) {
	store := newFakeStore()
	rec := New(store)

	id, _ := rec.CreateSession("ua", "default")

	neg := -5.0
	err := rec.RecordEvents(id, []Event{
		{Kind: "video_play", At: 1000, VideoPos: &neg},
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, *store.events[id][0].VideoPos)
}

func TestRecorder_OrderingWithSeek(t *testing.T) {
	store := newFakeStore()
	rec := New(store)

	id, _ := rec.CreateSession("ua", "default")

	seekPos := 30.0
	playPos := 30.0
	events := []Event{
		{Kind: "video_seek", At: 1000, VideoPos: &seekPos},
		{Kind: "video_play", At: 1001, VideoPos: &playPos},
	}

	err := rec.RecordEvents(id, events)
	require.NoError(t, err)

	stored := store.events[id]
	require.Len(t, stored, 2)
	assert.Equal(t, "video_seek", stored[0].Kind)
	assert.Equal(t, "video_play", stored[1].Kind)
	assert.Less(t, stored[0].At, stored[1].At)
}

func TestRecorder_IdleTransition(t *testing.T) {
	store := newFakeStore()
	rec := New(store)

	id, _ := rec.CreateSession("ua", "default")

	// Initial last_seen_at is the session creation time.
	initialLastSeen := store.sessions[id].LastSeenAt

	laterTime := initialLastSeen + 60000 // 60s later
	err := rec.RecordEvents(id, []Event{
		{Kind: "heartbeat", At: laterTime},
	})
	require.NoError(t, err)
	assert.Equal(t, laterTime, store.sessions[id].LastSeenAt)
}
