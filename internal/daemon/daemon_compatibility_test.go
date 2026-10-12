package daemon

import (
	"context"
	"strings"
	"testing"

	beadsdk "github.com/steveyegge/beads"
)

type metadataWriter interface {
	SetMetadata(ctx context.Context, key, value string) error
}

func TestCheckBeadsStoreCompatibility_AllowsMatchingVersion(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	metadataStore, ok := store.(metadataWriter)
	if !ok {
		t.Skip("store does not expose metadata writes")
	}
	if err := metadataStore.SetMetadata(ctx, "bd_version", "0.62.0"); err != nil {
		t.Fatalf("SetMetadata(bd_version): %v", err)
	}

	if err := checkBeadsStoreCompatibility(ctx, map[string]beadsdk.Storage{"hq": store}, "0.62.0"); err != nil {
		t.Fatalf("checkBeadsStoreCompatibility returned unexpected error: %v", err)
	}
}

// pilotBDVersion is the bd CLI version that writes the pilot town stores.
// The embedded beads module must be at least this version or the daemon
// refuses to start against those stores.
const pilotBDVersion = "1.3.0"

func TestCheckBeadsStoreCompatibility_EmbeddedVersionAcceptsPilotBDStore(t *testing.T) {
	embedded := embeddedBeadsVersion()
	if embedded == "" {
		t.Skip("build info does not include the beads module version")
	}

	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	metadataStore, ok := store.(metadataWriter)
	if !ok {
		t.Skip("store does not expose metadata writes")
	}
	if err := metadataStore.SetMetadata(ctx, "bd_version", pilotBDVersion); err != nil {
		t.Fatalf("SetMetadata(bd_version): %v", err)
	}

	if err := checkBeadsStoreCompatibility(ctx, map[string]beadsdk.Storage{"hq": store}, embedded); err != nil {
		t.Fatalf("embedded beads %s rejects a store written by bd %s: %v", embedded, pilotBDVersion, err)
	}
}

func TestCheckBeadsStoreCompatibility_RejectsNewerWorkspaceVersion(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	metadataStore, ok := store.(metadataWriter)
	if !ok {
		t.Skip("store does not expose metadata writes")
	}
	if err := metadataStore.SetMetadata(ctx, "bd_version", "9.9.9"); err != nil {
		t.Fatalf("SetMetadata(bd_version): %v", err)
	}

	err := checkBeadsStoreCompatibility(ctx, map[string]beadsdk.Storage{"hq": store}, "0.62.0")
	if err == nil {
		t.Fatal("expected incompatibility error, got nil")
	}
	if !strings.Contains(err.Error(), "workspace bd_version 9.9.9 is newer than embedded beads 0.62.0") {
		t.Fatalf("unexpected error: %v", err)
	}
}
