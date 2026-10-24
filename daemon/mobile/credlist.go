package mobile

// Cache rules shared by every control-plane proxy method. Ports
// apps/desktop/src/shared/hubCredList.ts.

type credKind[C any] struct {
	valid func(C) bool
	// normalize trims a validated candidate, so hub padding never reaches the cache.
	normalize func(C) C
	same      func(a, b C) bool
}

// restoreCached validates, normalizes and deduplicates, preserving order.
func restoreCached[C any](kind credKind[C], stored []C) []C {
	out := make([]C, 0, len(stored))
	for _, candidate := range stored {
		if !kind.valid(candidate) {
			continue
		}
		normalized := kind.normalize(candidate)
		if indexOfCred(kind, out, normalized) >= 0 {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

// mergeAdvertised caches every node the hub named, or nil when nothing changed.
// One node's credentials alone would strand the client on a rotation.
func mergeAdvertised[C any](kind credKind[C], current, advertised []C) []C {
	next := restoreCached(kind, advertised)
	if len(next) == 0 {
		return nil
	}
	// Keep the node that last worked in front so a refresh does not undo a promotion.
	if len(current) > 0 {
		if at := indexOfCred(kind, next, current[0]); at > 0 {
			next = moveEntryToFront(next, at)
		}
	}
	if len(next) == len(current) {
		unchanged := true
		for i := range next {
			if !kind.same(next[i], current[i]) {
				unchanged = false
				break
			}
		}
		if unchanged {
			return nil
		}
	}
	return next
}

// promoteEntry moves the entry that just worked to the front, or nil when
// there is nothing to move.
func promoteEntry[C any](list []C, index int) []C {
	if index <= 0 || index >= len(list) {
		return nil
	}
	return moveEntryToFront(append([]C(nil), list...), index)
}

// seedCached is the stored list, or copies of the shipped nodes when nothing
// usable is stored.
func seedCached[C any](kind credKind[C], stored, defaults []C) []C {
	if restored := restoreCached(kind, stored); len(restored) > 0 {
		return restored
	}
	return append([]C(nil), defaults...)
}

func indexOfCred[C any](kind credKind[C], list []C, value C) int {
	for i, item := range list {
		if kind.same(item, value) {
			return i
		}
	}
	return -1
}

func moveEntryToFront[C any](list []C, index int) []C {
	item := list[index]
	rest := append(list[:index:index], list[index+1:]...)
	return append([]C{item}, rest...)
}
