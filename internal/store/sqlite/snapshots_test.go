package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/store"
)

func TestSnapshotPersistenceAndChildProtection(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "snapshots.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	session := store.SessionRecord{ID: "ses_snapshot", OwnerKeyID: "key_snapshot", ImageReference: "alpine", State: "ready", LimitsJSON: []byte(`{}`), NetworkPolicyJSON: []byte(`{}`), EnvJSON: []byte(`{}`), LabelsJSON: []byte(`{}`), CreatedAt: now, LastActivityAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := db.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	parent := store.SnapshotRecord{ID: "snp_parent", SourceSessionID: session.ID, RuntimeSnapshotID: "native-parent", Size: 24, CreatedAt: now, LabelsJSON: []byte(`{"branch":"base"}`)}
	child := store.SnapshotRecord{ID: "snp_child", SourceSessionID: session.ID, ParentSnapshotID: parent.ID, RuntimeSnapshotID: "native-child", Flattened: true, CreatedAt: now.Add(time.Second), LabelsJSON: []byte(`{}`)}
	if err := db.CreateSnapshot(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSnapshot(context.Background(), child); err != nil {
		t.Fatal(err)
	}
	items, err := db.ListSnapshots(context.Background(), session.ID)
	if err != nil || len(items) != 2 || items[0].ID != parent.ID || items[0].Size != parent.Size {
		t.Fatalf("snapshot list=%+v err=%v", items, err)
	}
	got, err := db.GetSnapshot(context.Background(), child.ID)
	if err != nil || got.ParentSnapshotID != parent.ID || !got.Flattened {
		t.Fatalf("snapshot get=%+v err=%v", got, err)
	}
	hasChildren, err := db.SnapshotHasChildren(context.Background(), parent.ID)
	if err != nil || !hasChildren {
		t.Fatalf("has children=%v err=%v", hasChildren, err)
	}
	if err := db.DeleteSnapshot(context.Background(), parent.ID); err == nil {
		t.Fatal("deleted parent while child snapshot exists")
	}
	if err := db.DeleteSnapshot(context.Background(), child.ID); err != nil {
		t.Fatal(err)
	}
	fork := store.SessionRecord{ID: "ses_snapshot_fork", OwnerKeyID: session.OwnerKeyID, ImageReference: session.ImageReference, ParentSnapshotID: parent.ID, State: "creating", LimitsJSON: []byte(`{}`), NetworkPolicyJSON: []byte(`{}`), EnvJSON: []byte(`{}`), LabelsJSON: []byte(`{}`), CreatedAt: now, LastActivityAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := db.CreateSession(context.Background(), fork); err != nil {
		t.Fatal(err)
	}
	fork.State = "ready"
	if err := db.UpdateSession(context.Background(), fork); err != nil {
		t.Fatal(err)
	}
	hasChildren, err = db.SnapshotHasChildren(context.Background(), parent.ID)
	if err != nil || !hasChildren {
		t.Fatalf("live fork reference not detected: found=%v err=%v", hasChildren, err)
	}
	if err := db.DeleteSnapshot(context.Background(), parent.ID); err == nil {
		t.Fatal("deleted snapshot while a live fork references it")
	}
	fork.State = "stopping"
	if err := db.UpdateSession(context.Background(), fork); err != nil {
		t.Fatal(err)
	}
	fork.State = "stopped"
	if err := db.UpdateSession(context.Background(), fork); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteSnapshot(context.Background(), parent.ID); err != nil {
		t.Fatal(err)
	}
}
