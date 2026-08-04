package files

import "testing"

func TestNoopSpaceGuardAlwaysAllows(t *testing.T) {
	g := NoopSpaceGuard{}
	if allowed, err := g.Allow(1 << 40); err != nil || !allowed {
		t.Fatalf("expected noop guard to always allow, got allowed=%v err=%v", allowed, err)
	}
	if _, _, ok := g.Usage(); ok {
		t.Fatal("expected noop guard to report no usage snapshot")
	}
}

func TestLocalDiskSpaceGuardBeforeFirstRefresh(t *testing.T) {
	g := NewLocalDiskSpaceGuard(1 << 30)
	// Before setFree has ever run, the guard must not reject -- a startup
	// race shouldn't turn into spurious upload failures.
	if allowed, err := g.Allow(1 << 40); err != nil || !allowed {
		t.Fatalf("expected pre-refresh guard to allow, got allowed=%v err=%v", allowed, err)
	}
	if _, _, ok := g.Usage(); ok {
		t.Fatal("expected no usage snapshot before first refresh")
	}
}

func TestLocalDiskSpaceGuardThreshold(t *testing.T) {
	const minFree = int64(1000)
	g := NewLocalDiskSpaceGuard(minFree)
	g.setFree(1500, 10000)

	if allowed, err := g.Allow(400); err != nil || !allowed {
		t.Fatalf("writing 400 bytes leaves 1100 free (>= 1000 min): want allow, got allowed=%v err=%v", allowed, err)
	}
	if allowed, err := g.Allow(600); err != nil || allowed {
		t.Fatalf("writing 600 bytes leaves 900 free (< 1000 min): want reject, got allowed=%v err=%v", allowed, err)
	}

	used, total, ok := g.Usage()
	if !ok || total != 10000 || used != 8500 {
		t.Fatalf("usage snapshot = used=%d total=%d ok=%v, want used=8500 total=10000 ok=true", used, total, ok)
	}
}

func TestLocalDiskSpaceGuardDisabled(t *testing.T) {
	g := NewLocalDiskSpaceGuard(0)
	g.setFree(10, 1000)
	if allowed, err := g.Allow(1 << 40); err != nil || !allowed {
		t.Fatalf("minFreeBytes<=0 must disable the check, got allowed=%v err=%v", allowed, err)
	}
}

func TestS3BudgetSpaceGuardThreshold(t *testing.T) {
	const maxTotal = int64(10000)
	g := NewS3BudgetSpaceGuard(maxTotal)
	g.setUsed(9000)

	if allowed, err := g.Allow(1000); err != nil || !allowed {
		t.Fatalf("9000+1000 == budget: want allow, got allowed=%v err=%v", allowed, err)
	}
	if allowed, err := g.Allow(1001); err != nil || allowed {
		t.Fatalf("9000+1001 exceeds budget: want reject, got allowed=%v err=%v", allowed, err)
	}

	used, total, ok := g.Usage()
	if !ok || total != maxTotal || used != 9000 {
		t.Fatalf("usage snapshot = used=%d total=%d ok=%v, want used=9000 total=%d ok=true", used, total, ok, maxTotal)
	}
}

func TestS3BudgetSpaceGuardBeforeFirstRefresh(t *testing.T) {
	g := NewS3BudgetSpaceGuard(100)
	if allowed, err := g.Allow(1 << 40); err != nil || !allowed {
		t.Fatalf("expected pre-refresh guard to allow, got allowed=%v err=%v", allowed, err)
	}
}
