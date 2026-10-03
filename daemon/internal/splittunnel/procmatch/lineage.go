package procmatch

const maxChainDepth = 16

type procKey struct {
	pid   int
	start int64
}

type procInfo struct {
	pid   int
	ppid  int
	start int64
	path  string
}

type lineageNode struct {
	parent    procKey
	hasParent bool
	path      string
	alive     bool
	refs      int
	seen      uint64
}

// lineage records parent links when both processes are observed alive together and keeps
// exited processes while a recorded descendant still refers to them.
type lineage struct {
	nodes map[procKey]*lineageNode
	live  map[int]procKey
	tick  uint64
}

func newLineage() *lineage {
	return &lineage{nodes: make(map[procKey]*lineageNode), live: make(map[int]procKey)}
}

// mark returns a tick; a later sync retires only nodes not observed since then.
func (l *lineage) mark() uint64 {
	l.tick++
	return l.tick
}

func (l *lineage) has(k procKey) bool {
	return l.nodes[k] != nil
}

func (l *lineage) known(k procKey) bool {
	n := l.nodes[k]
	return n != nil && n.path != ""
}

// add records a process observed alive now; parent, if given, was verified alive at the same time.
func (l *lineage) add(p procInfo, parent *procKey) {
	k := procKey{p.pid, p.start}
	if old, ok := l.live[p.pid]; ok && old != k {
		l.retire(old)
	}
	n := l.nodes[k]
	if n == nil {
		n = &lineageNode{path: p.path}
		l.nodes[k] = n
	} else if n.path == "" {
		n.path = p.path
	}
	if parent != nil && !n.hasParent {
		l.link(k, n, *parent)
	}
	n.alive = true
	n.seen = l.mark()
	l.live[p.pid] = k
}

func (l *lineage) link(k procKey, n *lineageNode, parent procKey) {
	if parent.pid == k.pid || parent.start > k.start {
		return
	}
	pn := l.nodes[parent]
	if pn == nil {
		return
	}
	n.parent, n.hasParent = parent, true
	pn.refs++
}

// sync applies a full snapshot of live processes taken after tick since.
func (l *lineage) sync(procs []procInfo, since uint64) {
	present := make(map[procKey]bool, len(procs))
	byPID := make(map[int]procKey, len(procs))
	for _, p := range procs {
		k := procKey{p.pid, p.start}
		if cur, ok := l.live[p.pid]; ok && cur != k && l.nodes[cur] != nil && l.nodes[cur].seen >= since {
			continue
		}
		present[k] = true
		byPID[p.pid] = k
		n := l.nodes[k]
		if n == nil {
			l.nodes[k] = &lineageNode{path: p.path}
		} else if n.path == "" {
			n.path = p.path
		}
	}
	for _, p := range procs {
		k := procKey{p.pid, p.start}
		n := l.nodes[k]
		if !present[k] || n.hasParent {
			continue
		}
		if pk, ok := byPID[p.ppid]; ok {
			l.link(k, n, pk)
		}
	}
	tick := l.mark()
	for k := range present {
		if old, ok := l.live[k.pid]; ok && old != k {
			l.retire(old)
		}
		n := l.nodes[k]
		n.alive, n.seen = true, tick
		l.live[k.pid] = k
	}
	for k, n := range l.nodes {
		if n.alive && !present[k] && n.seen < since {
			l.retire(k)
		}
	}
}

func (l *lineage) retire(k procKey) {
	n := l.nodes[k]
	if n == nil {
		return
	}
	n.alive = false
	if cur, ok := l.live[k.pid]; ok && cur == k {
		delete(l.live, k.pid)
	}
	l.collect(k)
}

func (l *lineage) collect(k procKey) {
	for {
		n := l.nodes[k]
		if n == nil || n.alive || n.refs > 0 {
			return
		}
		delete(l.nodes, k)
		if !n.hasParent {
			return
		}
		pn := l.nodes[n.parent]
		if pn == nil {
			return
		}
		pn.refs--
		k = n.parent
	}
}

// lineageView is one owner's recorded ancestry: keys and paths for the protection checks,
// chain for rule matching (cut at the first session root, system host or unreadable image).
type lineageView struct {
	keys  []procKey
	paths []string
	chain []string
}

func (l *lineage) view(k procKey, isStop func(string) bool) lineageView {
	var v lineageView
	stopped := false
	for len(v.keys) < maxChainDepth {
		n := l.nodes[k]
		if n == nil || k.pid == 0 || k.pid == 1 || k.pid == 4 {
			break
		}
		v.keys = append(v.keys, k)
		v.paths = append(v.paths, n.path)
		if !stopped && (n.path == "" || (isStop != nil && isStop(n.path))) {
			stopped = true
		}
		if !stopped {
			v.chain = append(v.chain, n.path)
		}
		if !n.hasParent {
			break
		}
		k = n.parent
	}
	return v
}

// setPath records the image a live process exec'd into.
func (l *lineage) setPath(k procKey, p string) bool {
	n := l.nodes[k]
	if n == nil || p == "" || n.path == p {
		return false
	}
	n.path = p
	return true
}

// hasAncestor reports whether any recorded ancestor of k (k excluded) is in set.
func (l *lineage) hasAncestor(k procKey, set map[procKey]bool) bool {
	for depth := 0; depth < maxChainDepth; depth++ {
		n := l.nodes[k]
		if n == nil || !n.hasParent {
			return false
		}
		k = n.parent
		if set[k] {
			return true
		}
	}
	return false
}

// protected reports whether the owner is in the classifier's own tree or below a never-bypass image.
func (v lineageView) protected(never []*Rules, self procKey) bool {
	for _, k := range v.keys {
		if k == self {
			return true
		}
	}
	for _, p := range v.paths {
		for _, n := range never {
			if n.MatchPath(p) {
				return true
			}
		}
	}
	return false
}

// verdictFor applies the matching semantics; protected owners get an empty chain so a
// later MatchChain recompute can never flip them to bypass.
func verdictFor(rules *Rules, never []*Rules, self procKey, v lineageView) (Verdict, []string) {
	if v.protected(never, self) {
		return VerdictTunnel, nil
	}
	if rules.MatchChain(v.chain) {
		return VerdictBypass, v.chain
	}
	return VerdictTunnel, v.chain
}
