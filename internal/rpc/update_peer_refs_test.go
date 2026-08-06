package rpc

import (
	"testing"

	"telesrv/internal/domain"
)

func TestRemoveKnownChannelRefs(t *testing.T) {
	refs := map[int64]struct{}{
		1001: {},
		1002: {},
		1003: {},
	}

	removeKnownChannelRefs(refs, []domain.Channel{
		{ID: 1002},
		{ID: 0},
		{ID: 1004},
	})

	if _, ok := refs[1002]; ok {
		t.Fatalf("known channel ref was not removed: %+v", refs)
	}
	for _, id := range []int64{1001, 1003} {
		if _, ok := refs[id]; !ok {
			t.Fatalf("unexpectedly removed channel %d from refs %+v", id, refs)
		}
	}
}
