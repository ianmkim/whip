package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalListAndWatch(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	const initial = `{"pid":1,"sessionId":"id-1","cwd":"/x","status":"idle","startedAt":1,"updatedAt":2}`
	p := filepath.Join(root, "sessions", "1.json")
	if err := os.WriteFile(p, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	src, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	got, err := src.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "id-1" {
		t.Fatalf("list: %+v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := src.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}

	const updated = `{"pid":1,"sessionId":"id-1","cwd":"/x","status":"busy","startedAt":1,"updatedAt":3}`
	if err := os.WriteFile(p, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-ch:
		if ev.Kind != EventUpsert {
			t.Fatalf("kind: %v", ev.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event after write")
	}

	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		if ev.Kind != EventDelete {
			t.Fatalf("kind after remove: %v", ev.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event after remove")
	}
}
