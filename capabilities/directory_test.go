package capabilities

import "testing"

func TestDirectoryCapabilityTracksSharers(t *testing.T) {
	cap := NewDirectoryCapability("directory-test")
	store := cap.Directory()

	addr := uint64(0x3000)
	if sharers := store.Sharers(addr); sharers != nil {
		t.Fatalf("expected no sharers initially")
	}

	store.Add(addr, 1)
	store.Add(addr, 2)

	sharers := store.Sharers(addr)
	if len(sharers) != 2 {
		t.Fatalf("expected 2 sharers, got %d", len(sharers))
	}

	store.Remove(addr, 1)
	sharers = store.Sharers(addr)
	if len(sharers) != 1 || sharers[0] != 2 {
		t.Fatalf("expected remaining sharer 2, got %v", sharers)
	}

	store.Clear(addr)
	if sharers := store.Sharers(addr); sharers != nil {
		t.Fatalf("expected sharers cleared")
	}
}
