package state

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestBlockLogIsBoundedAndOrdered(t *testing.T) {
	m := NewManager(Options{BlockLogSize: 3, MaxChunkSummaryBytes: 16})
	base := time.Unix(100, 0)
	for i := 0; i < 5; i++ {
		m.AppendBlockEvent(BlockEvent{BlockID: string(rune('a' + i)), ServerTime: base.UnixMilli() + int64(i)})
	}
	events := m.BlockEvents()
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	got := []string{events[0].BlockID, events[1].BlockID, events[2].BlockID}
	if !reflect.DeepEqual(got, []string{"c", "d", "e"}) {
		t.Fatalf("unexpected order: %v", got)
	}
	if events[0].Sequence != 3 || events[2].Sequence != 5 {
		t.Fatalf("unexpected sequence numbers: %+v", events)
	}
}

func TestChunkAuthorityConflictAndTakeover(t *testing.T) {
	m := NewManager(Options{BlockLogSize: 4, MaxChunkSummaryBytes: 32})
	now := time.Unix(100, 0)
	chunk, result, err := m.ClaimChunk("overworld:0,0", "a", "Alice", []byte("one"), now)
	if err != nil || result != ChunkClaimed {
		t.Fatalf("claim failed: result=%v err=%v", result, err)
	}
	if chunk.Revision != 1 {
		t.Fatalf("unexpected revision: %d", chunk.Revision)
	}

	_, result, err = m.ClaimChunk("overworld:0,0", "b", "Bob", []byte("two"), now.Add(time.Second))
	if !errors.Is(err, ErrChunkOwned) || result != ChunkConflict {
		t.Fatalf("expected conflict: result=%v err=%v", result, err)
	}

	m.AddPlayer("a", "Alice", "survival", "overworld", now)
	m.RemovePlayer("a", now.Add(2*time.Second))
	chunk, result, err = m.ClaimChunk("overworld:0,0", "b", "Bob", []byte("two"), now.Add(3*time.Second))
	if err != nil || result != ChunkTakenOver {
		t.Fatalf("expected takeover: result=%v err=%v", result, err)
	}
	if chunk.OwnerID != "b" || string(chunk.Summary) != "two" {
		t.Fatalf("unexpected takeover result: %+v", chunk)
	}
}

func TestOfflineChunkExpiry(t *testing.T) {
	m := NewManager(Options{BlockLogSize: 4, MaxChunkSummaryBytes: 32})
	now := time.Unix(100, 0)
	m.AddPlayer("a", "Alice", "survival", "overworld", now)
	_, _, _ = m.ClaimChunk("overworld:0,0", "a", "Alice", nil, now)
	m.RemovePlayer("a", now.Add(time.Second))

	stats := m.Cleanup(now.Add(4*time.Second), 2*time.Second, time.Minute)
	if stats.Chunks != 1 {
		t.Fatalf("expected one expired chunk, got %+v", stats)
	}
	if _, ok := m.GetChunk("overworld:0,0"); ok {
		t.Fatal("chunk should have been removed")
	}
}

func TestRemovePlayerReleasesEntityAuthority(t *testing.T) {
	m := NewManager(Options{BlockLogSize: 4, MaxChunkSummaryBytes: 32})
	now := time.Unix(100, 0)
	m.AddPlayer("a", "Alice", "survival", "overworld", now)
	m.UpsertEntity("e1", "zombie", Vec3{}, "a", "Alice", now)

	released := m.RemovePlayer("a", now.Add(time.Second))
	if len(released) != 1 || released[0].AuthorityID != "" {
		t.Fatalf("expected one released entity: %+v", released)
	}
}

func TestChunkAuthorityCapacity(t *testing.T) {
	m := NewManager(Options{BlockLogSize: 4, MaxChunkSummaryBytes: 32, MaxChunkAuthorities: 2})
	now := time.Unix(100, 0)
	_, _, _ = m.ClaimChunk("c1", "a", "Alice", nil, now)
	_, _, _ = m.ClaimChunk("c2", "a", "Alice", nil, now.Add(time.Second))
	_, _, _ = m.ClaimChunk("c3", "a", "Alice", nil, now.Add(2*time.Second))

	players, chunks, entities := m.Stats()
	if players != 0 || chunks != 2 || entities != 0 {
		t.Fatalf("unexpected stats: players=%d chunks=%d entities=%d", players, chunks, entities)
	}
	if _, ok := m.GetChunk("c1"); ok {
		t.Fatal("oldest chunk should have been evicted")
	}
	if _, ok := m.GetChunk("c3"); !ok {
		t.Fatal("newest chunk should remain")
	}
}
