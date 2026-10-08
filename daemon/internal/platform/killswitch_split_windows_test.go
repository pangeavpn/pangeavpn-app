//go:build windows && (amd64 || arm64)

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	fwpETxnInProgress   uint32 = 0x8032000D
	fwpENoTxnInProgress uint32 = 0x8032000E
	fwpEInUse           uint32 = 0x80320006
)

type fakeCondition struct {
	field windows.GUID
	match wtFwpMatchType
	kind  wtFwpDataType
	value uint64
	addr  wtFwpV4AddrAndMask
	blob  string
}

type fakeFilter struct {
	id         uint64
	key        windows.GUID
	layer      windows.GUID
	sublayer   windows.GUID
	name       string
	weight     uint8
	action     wtFwpActionType
	flags      wtFwpmFilterFlags
	conditions []fakeCondition
}

// fakeBFE is an in-memory filter engine with BFE's transaction semantics: an
// abort puts back exactly what was there at begin.
type fakeBFE struct {
	nextID    uint64
	installed map[uint64]*fakeFilter
	sublayer  bool

	inTxn      bool
	saved      map[uint64]*fakeFilter
	savedLayer bool

	ops        []string
	failAdd    func(name string) bool
	failDelete func(f *fakeFilter) bool
}

func newFakeBFE() *fakeBFE {
	return &fakeBFE{installed: map[uint64]*fakeFilter{}}
}

func (b *fakeBFE) engineClose(windows.Handle) uintptr { return 0 }

func (b *fakeBFE) transactionBegin(windows.Handle) uintptr {
	if b.inTxn {
		return uintptr(fwpETxnInProgress)
	}
	b.inTxn = true
	b.saved = make(map[uint64]*fakeFilter, len(b.installed))
	for id, f := range b.installed {
		b.saved[id] = f
	}
	b.savedLayer = b.sublayer
	b.ops = append(b.ops, "begin")
	return 0
}

func (b *fakeBFE) transactionCommit(windows.Handle) uintptr {
	if !b.inTxn {
		return uintptr(fwpENoTxnInProgress)
	}
	b.inTxn, b.saved = false, nil
	b.ops = append(b.ops, "commit")
	return 0
}

func (b *fakeBFE) transactionAbort(windows.Handle) uintptr {
	if !b.inTxn {
		return uintptr(fwpENoTxnInProgress)
	}
	b.installed, b.sublayer = b.saved, b.savedLayer
	b.inTxn, b.saved = false, nil
	b.ops = append(b.ops, "abort")
	return 0
}

func (b *fakeBFE) subLayerAdd(windows.Handle, *wtFwpmSublayer0) uintptr {
	if b.sublayer {
		return uintptr(fwpEAlreadyExists)
	}
	b.sublayer = true
	return 0
}

func (b *fakeBFE) subLayerDeleteByKey(windows.Handle, *windows.GUID) uintptr {
	if !b.sublayer {
		return uintptr(fwpESublayerNotFound)
	}
	for _, f := range b.installed {
		if f.sublayer == pangeaVPNSublayerKey {
			return uintptr(fwpEInUse)
		}
	}
	b.sublayer = false
	return 0
}

func (b *fakeBFE) filterAdd(_ windows.Handle, filter *wtFwpmFilter0, id *uint64) uintptr {
	name := windows.UTF16PtrToString(filter.displayData.name)
	if b.failAdd != nil && b.failAdd(name) {
		return uintptr(windows.ERROR_ACCESS_DENIED)
	}
	if filter.filterKey != (windows.GUID{}) && b.byKey(filter.filterKey) != nil {
		return uintptr(fwpEAlreadyExists)
	}
	b.nextID++
	b.installed[b.nextID] = &fakeFilter{
		id:         b.nextID,
		key:        filter.filterKey,
		layer:      filter.layerKey,
		sublayer:   filter.subLayerKey,
		name:       name,
		weight:     uint8(filter.weight.value),
		action:     filter.action._type,
		flags:      filter.flags,
		conditions: decodeFakeConditions(filter),
	}
	*id = b.nextID
	b.ops = append(b.ops, "add "+name)
	return 0
}

// decodeFakeConditions copies the conditions out while the caller still keeps
// the values they point at alive.
func decodeFakeConditions(filter *wtFwpmFilter0) []fakeCondition {
	if filter.numFilterConditions == 0 {
		return nil
	}
	var out []fakeCondition
	for _, c := range unsafe.Slice(filter.filterCondition, filter.numFilterConditions) {
		fc := fakeCondition{field: c.fieldKey, match: c.matchType, kind: c.conditionValue._type}
		ptr := *(*unsafe.Pointer)(unsafe.Pointer(&c.conditionValue.value))
		switch c.conditionValue._type {
		case cFWP_UINT8, cFWP_UINT16, cFWP_UINT32:
			fc.value = uint64(c.conditionValue.value)
		case cFWP_UINT64:
			fc.value = *(*uint64)(ptr)
		case cFWP_V4_ADDR_MASK:
			fc.addr = *(*wtFwpV4AddrAndMask)(ptr)
		case cFWP_BYTE_BLOB_TYPE:
			blob := (*wtFwpByteBlob)(ptr)
			fc.blob = string(unsafe.Slice(blob.data, blob.size))
		}
		out = append(out, fc)
	}
	return out
}

func (b *fakeBFE) remove(f *fakeFilter) uintptr {
	if b.failDelete != nil && b.failDelete(f) {
		return uintptr(windows.ERROR_ACCESS_DENIED)
	}
	delete(b.installed, f.id)
	b.ops = append(b.ops, "delete "+f.name)
	return 0
}

func (b *fakeBFE) filterDeleteByID(_ windows.Handle, id uint64) uintptr {
	f, ok := b.installed[id]
	if !ok {
		return uintptr(fwpEFilterNotFound)
	}
	return b.remove(f)
}

func (b *fakeBFE) filterDeleteByKey(_ windows.Handle, key *windows.GUID) uintptr {
	f := b.byKey(*key)
	if f == nil {
		return uintptr(fwpEFilterNotFound)
	}
	return b.remove(f)
}

func (b *fakeBFE) filterExistsByKey(_ windows.Handle, key *windows.GUID) (bool, uintptr) {
	if b.byKey(*key) == nil {
		return false, uintptr(fwpEFilterNotFound)
	}
	return true, 0
}

func (b *fakeBFE) filters(windows.Handle) ([]wtFwpmFilter0, error) {
	out := make([]wtFwpmFilter0, 0, len(b.installed))
	for _, f := range b.installed {
		out = append(out, wtFwpmFilter0{filterID: f.id, subLayerKey: f.sublayer, flags: f.flags})
	}
	return out, nil
}

func (b *fakeBFE) byKey(key windows.GUID) *fakeFilter {
	for _, f := range b.installed {
		if f.key == key {
			return f
		}
	}
	return nil
}

func (b *fakeBFE) named(name string) []*fakeFilter {
	var out []*fakeFilter
	for _, f := range b.installed {
		if f.name == name || f.name == name+" Inbound" {
			out = append(out, f)
		}
	}
	slices.SortFunc(out, func(x, y *fakeFilter) int { return int(x.id) - int(y.id) })
	return out
}

func (b *fakeBFE) withPrefix(prefix string) []*fakeFilter {
	var out []*fakeFilter
	for _, f := range b.installed {
		if strings.HasPrefix(f.name, prefix) {
			out = append(out, f)
		}
	}
	return out
}

func filterIDs(filters []*fakeFilter) []uint64 {
	ids := make([]uint64, 0, len(filters))
	for _, f := range filters {
		ids = append(ids, f.id)
	}
	slices.Sort(ids)
	return ids
}

func sortedIDs(ids []uint64) []uint64 {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

const (
	fakeDaemonImage = `C:\Program Files\PangeaVPN\resources\daemon\pangea-daemon.exe`
	fakeDaemonAppID = `\device\harddiskvolume3\program files\pangeavpn\resources\daemon\pangea-daemon.exe`

	splitEgressName = "PangeaVPN Allow Split Tunnel Egress"
	splitRangeName  = "PangeaVPN Allow Split Range "
	fwdSplitName    = "PangeaVPN Allow Forwarding To Split Range "
)

type windowsSplitHarness struct {
	t        *testing.T
	bfe      *fakeBFE
	ks       *windowsKillSwitch
	opened   int
	freed    int
	appIDErr error
	warnings []string
}

func newWindowsSplitHarness(t *testing.T) *windowsSplitHarness {
	t.Helper()
	isolateStateDir(t)
	h := &windowsSplitHarness{t: t, bfe: newFakeBFE(), ks: &windowsKillSwitch{}}

	prevOpen, prevAppID, prevImage, prevIndex, prevWarn := openWFPEngine, wfpAppID, splitEgressImage, interfaceIndexForLUID, KillSwitchWarnf
	t.Cleanup(func() {
		openWFPEngine, wfpAppID, splitEgressImage, interfaceIndexForLUID, KillSwitchWarnf = prevOpen, prevAppID, prevImage, prevIndex, prevWarn
	})
	openWFPEngine = func() (*wfpEngine, error) {
		h.opened++
		return &wfpEngine{handle: 1, calls: h.bfe}, nil
	}
	splitEgressImage = func() (string, error) { return fakeDaemonImage, nil }
	wfpAppID = func(path string) (*wtFwpByteBlob, func(), error) {
		if h.appIDErr != nil {
			return nil, nil, h.appIDErr
		}
		if path != fakeDaemonImage {
			t.Errorf("app id asked for %q, want the daemon image", path)
		}
		data := []byte(fakeDaemonAppID)
		return &wtFwpByteBlob{size: uint32(len(data)), data: &data[0]}, func() { h.freed++ }, nil
	}
	interfaceIndexForLUID = func(luid uint64) (uint32, error) { return uint32(luid & 0xffff), nil }
	KillSwitchWarnf = func(format string, args ...any) { h.warnings = append(h.warnings, fmt.Sprintf(format, args...)) }
	return h
}

func (h *windowsSplitHarness) arm(endpoints []string, allowLAN bool) {
	h.t.Helper()
	if err := h.ks.arm(h.t.Context(), endpoints, allowLAN, false); err != nil {
		h.t.Fatalf("arm: %v", err)
	}
}

func (h *windowsSplitHarness) setCIDRs(cidrs ...string) error {
	return h.ks.SetSplitCIDRs(h.t.Context(), cidrs)
}

func (h *windowsSplitHarness) setEgress(on bool) error {
	return h.ks.SetSplitEgress(h.t.Context(), on)
}

// assertSplitInstalled checks the engine holds exactly the split filters for want
// and that the switch tracks exactly those IDs.
func (h *windowsSplitHarness) assertSplitInstalled(egress bool, tunnelLUID uint64, cidrs ...string) {
	h.t.Helper()
	egressFilters := h.bfe.named(splitEgressName)
	if !egress {
		if len(egressFilters) != 0 || len(h.ks.splitEgressFilterIds) != 0 {
			h.t.Fatalf("egress permit present (%d filters, ids %v) when not wanted", len(egressFilters), h.ks.splitEgressFilterIds)
		}
	} else {
		if len(egressFilters) != 2 {
			h.t.Fatalf("got %d egress filters, want the CONNECT and RECV_ACCEPT pair", len(egressFilters))
		}
		if !slices.Equal(filterIDs(egressFilters), sortedIDs(h.ks.splitEgressFilterIds)) {
			h.t.Fatalf("tracked egress ids %v, installed %v", h.ks.splitEgressFilterIds, filterIDs(egressFilters))
		}
		layers := []windows.GUID{egressFilters[0].layer, egressFilters[1].layer}
		if !slices.Contains(layers, cFWPM_LAYER_ALE_AUTH_CONNECT_V4) || !slices.Contains(layers, cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4) {
			h.t.Fatalf("egress filters on layers %v, want CONNECT_V4 and RECV_ACCEPT_V4", layers)
		}
		for _, f := range egressFilters {
			assertSplitEgressFilter(h.t, f, tunnelLUID)
		}
	}

	var wantRange, wantForward []uint64
	for _, cidr := range cidrs {
		ale := h.bfe.named(splitRangeName + cidr)
		if len(ale) != 2 {
			h.t.Fatalf("range %s has %d ALE filters, want 2", cidr, len(ale))
		}
		for _, f := range ale {
			assertSplitRangeFilter(h.t, f, cidr)
		}
		wantRange = append(wantRange, filterIDs(ale)...)
		if h.ks.forwardLock {
			fwd := h.bfe.named(fwdSplitName + cidr)
			if len(fwd) != 1 || fwd[0].layer != cFWPM_LAYER_IPFORWARD_V4 || fwd[0].weight != weightSplitPermit {
				h.t.Fatalf("range %s forward permit = %+v, want one IPFORWARD_V4 permit at the split weight", cidr, fwd)
			}
			wantForward = append(wantForward, fwd[0].id)
		}
	}
	if got := len(h.bfe.withPrefix(splitRangeName)); got != 2*len(cidrs) {
		h.t.Fatalf("%d split range filters installed, want %d", got, 2*len(cidrs))
	}
	if got := len(h.bfe.withPrefix(fwdSplitName)); got != len(wantForward) {
		h.t.Fatalf("%d forward split filters installed, want %d", got, len(wantForward))
	}
	slices.Sort(wantRange)
	slices.Sort(wantForward)
	if !slices.Equal(wantRange, sortedIDs(h.ks.splitCIDRFilterIds)) {
		h.t.Fatalf("tracked range ids %v, installed %v", h.ks.splitCIDRFilterIds, wantRange)
	}
	if !slices.Equal(wantForward, sortedIDs(h.ks.forwardSplitFilterIds)) {
		h.t.Fatalf("tracked forward ids %v, installed %v", h.ks.forwardSplitFilterIds, wantForward)
	}
	if h.ks.appliedSplit.Egress != egress || !slices.Equal(h.ks.appliedSplit.CIDRs, cidrs) {
		h.t.Fatalf("appliedSplit = %+v, want egress=%v cidrs=%v", h.ks.appliedSplit, egress, cidrs)
	}
}

func assertSplitEgressFilter(t *testing.T, f *fakeFilter, tunnelLUID uint64) {
	t.Helper()
	if f.weight != weightSplitPermit || f.action != cFWP_ACTION_PERMIT || f.sublayer != pangeaVPNSublayerKey || !isEphemeralFilter(&wtFwpmFilter0{flags: f.flags}) {
		t.Fatalf("egress filter %q: weight=%d action=%#x flags=%#x, want an ephemeral permit at %d", f.name, f.weight, f.action, f.flags, weightSplitPermit)
	}
	want := 1
	if tunnelLUID != 0 {
		want = 2
	}
	if len(f.conditions) != want {
		t.Fatalf("egress filter %q has %d conditions, want %d", f.name, len(f.conditions), want)
	}
	app := f.conditions[0]
	if app.field != cFWPM_CONDITION_ALE_APP_ID || app.match != cFWP_MATCH_EQUAL || app.kind != cFWP_BYTE_BLOB_TYPE || app.blob != fakeDaemonAppID {
		t.Fatalf("egress filter %q app condition = %+v, want ALE_APP_ID equal to the daemon image", f.name, app)
	}
	if tunnelLUID != 0 {
		iface := f.conditions[1]
		if iface.field != cFWPM_CONDITION_IP_LOCAL_INTERFACE || iface.match != cFWP_MATCH_NOT_EQUAL || iface.kind != cFWP_UINT64 || iface.value != tunnelLUID {
			t.Fatalf("egress filter %q interface condition = %+v, want IP_LOCAL_INTERFACE != %#x", f.name, iface, tunnelLUID)
		}
	}
}

func assertSplitRangeFilter(t *testing.T, f *fakeFilter, cidr string) {
	t.Helper()
	want, err := parseV4CIDRAddrMask(cidr)
	if err != nil {
		t.Fatal(err)
	}
	if f.weight != weightSplitPermit || f.action != cFWP_ACTION_PERMIT {
		t.Fatalf("range filter %q: weight=%d action=%#x", f.name, f.weight, f.action)
	}
	wantConds := 1
	switch f.layer {
	case cFWPM_LAYER_ALE_AUTH_CONNECT_V4:
	case cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4:
		// A host in the range must not open TCP connections to local services.
		wantConds = 2
	default:
		t.Fatalf("range filter %q on layer %v", f.name, f.layer)
	}
	if len(f.conditions) != wantConds || f.conditions[0].field != cFWPM_CONDITION_IP_REMOTE_ADDRESS || f.conditions[0].kind != cFWP_V4_ADDR_MASK || f.conditions[0].addr != want {
		t.Fatalf("range filter %q conditions = %+v, want remote address %s", f.name, f.conditions, cidr)
	}
	if wantConds == 2 {
		c := f.conditions[1]
		if c.field != cFWPM_CONDITION_IP_PROTOCOL || c.match != cFWP_MATCH_EQUAL || c.kind != cFWP_UINT8 || c.value != uint64(cIPPROTO_UDP) {
			t.Fatalf("inbound range filter %q protocol condition = %+v, want UDP only", f.name, c)
		}
	}
}

// transactions splits the op log into the transactions it ran.
func transactions(ops []string) [][]string {
	var out [][]string
	var cur []string
	in := false
	for _, op := range ops {
		switch op {
		case "begin":
			in, cur = true, nil
		case "commit", "abort":
			if in {
				out = append(out, append(cur, op))
			}
			in = false
		default:
			if in {
				cur = append(cur, op)
			}
		}
	}
	return out
}

func opIndex(ops []string, op string) int {
	return slices.Index(ops, op)
}

func TestWindowsSplitPermits_InactiveSwitchOnlyRecords(t *testing.T) {
	h := newWindowsSplitHarness(t)
	if err := h.setCIDRs("198.51.100.0/24", "10.20.0.0/16"); err != nil {
		t.Fatalf("SetSplitCIDRs: %v", err)
	}
	if err := h.setEgress(true); err != nil {
		t.Fatalf("SetSplitEgress: %v", err)
	}
	if h.opened != 0 || len(h.bfe.installed) != 0 {
		t.Fatalf("an idle switch opened BFE %d times and added %d filters", h.opened, len(h.bfe.installed))
	}
	want := splitPermits{Egress: true, CIDRs: []string{"10.20.0.0/16", "198.51.100.0/24"}}
	if !h.ks.split.equal(want) {
		t.Fatalf("desired = %+v, want %+v", h.ks.split, want)
	}
	if err := h.setCIDRs("198.51.100.1/24"); err == nil {
		t.Fatal("a range with host bits was accepted")
	}
	if !h.ks.split.equal(want) {
		t.Fatalf("a refused set changed the desired one to %+v", h.ks.split)
	}
	if st, err := loadKillSwitchState(); err != nil || st.Active {
		t.Fatalf("recording split permits wrote kill switch state: %+v, %v", st, err)
	}
}

// The fresh arm sweeps every ephemeral permit, so it must put the wanted split
// permits back inside the very transaction that installs the lock.
func TestWindowsFreshArmInstallsSplitInsideTheLockTransaction(t *testing.T) {
	h := newWindowsSplitHarness(t)
	if err := h.setCIDRs("198.51.100.0/24"); err != nil {
		t.Fatal(err)
	}
	if err := h.setEgress(true); err != nil {
		t.Fatal(err)
	}
	h.arm([]string{"203.0.113.5"}, true)

	h.assertSplitInstalled(true, 0, "198.51.100.0/24")
	if h.freed != 1 {
		t.Fatalf("app id freed %d times, want once after the install", h.freed)
	}

	txns := transactions(h.bfe.ops)
	lock := txns[0]
	if lock[len(lock)-1] != "commit" || opIndex(lock, "add PangeaVPN Block All Outbound") < 0 {
		t.Fatalf("first transaction is not the lock install: %v", lock)
	}
	block := opIndex(lock, "add PangeaVPN Block All Outbound")
	for _, op := range []string{"add " + splitEgressName, "add " + splitEgressName + " Inbound", "add " + splitRangeName + "198.51.100.0/24", "add " + splitRangeName + "198.51.100.0/24 Inbound"} {
		if at := opIndex(lock, op); at < block {
			t.Fatalf("%q is not in the lock transaction after the blocks (at %d, block at %d): %v", op, at, block, lock)
		}
	}
	if opIndex(lock, "add "+fwdSplitName+"198.51.100.0/24") >= 0 {
		t.Fatalf("forward split permits rode in the lock transaction; a failure there would abort the lock: %v", lock)
	}
	found := false
	for _, txn := range txns[1:] {
		if opIndex(txn, "add "+fwdSplitName+"198.51.100.0/24") >= 0 {
			found = true
			if opIndex(txn, "add PangeaVPN Allow Forwarding To LAN 10.0.0.0/8") >= 0 {
				t.Fatalf("forward split permits share the forward LAN transaction: %v", txn)
			}
		}
	}
	if !found {
		t.Fatalf("no transaction added the forward split permit: %v", txns)
	}
	if st, err := loadKillSwitchState(); err != nil || !st.Active {
		t.Fatalf("state after arm = %+v, %v", st, err)
	}
}

// DL-2: a re-arm that finds the persistent block gone rebuilds from scratch, and
// the rebuild must not lose (or double) the split permits.
func TestWindowsFreshArmReAddsSplitAfterThePersistentBlockVanished(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.arm([]string{"203.0.113.5"}, false)
	if err := h.ks.Update(t.Context(), TunnelRef{Name: "wg0", WindowsLUID: 0x5001}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := h.setCIDRs("198.51.100.0/24", "10.20.0.0/16"); err != nil {
		t.Fatal(err)
	}
	if err := h.setEgress(true); err != nil {
		t.Fatal(err)
	}
	h.assertSplitInstalled(true, 0x5001, "10.20.0.0/16", "198.51.100.0/24")
	before := sortedIDs(slices.Concat(h.ks.splitEgressFilterIds, h.ks.splitCIDRFilterIds))

	if r := h.bfe.filterDeleteByKey(0, &pangeaBlockAllOutboundV4FilterKey); r != 0 {
		t.Fatalf("simulating an external removal: %#x", r)
	}
	h.arm([]string{"203.0.113.5"}, false)

	if h.bfe.byKey(pangeaBlockAllOutboundV4FilterKey) == nil {
		t.Fatal("the lock was not rebuilt")
	}
	h.assertSplitInstalled(true, 0x5001, "10.20.0.0/16", "198.51.100.0/24")
	after := sortedIDs(slices.Concat(h.ks.splitEgressFilterIds, h.ks.splitCIDRFilterIds))
	for _, id := range after {
		if slices.Contains(before, id) {
			t.Fatalf("split id %d survived a rebuild that swept every ephemeral permit", id)
		}
	}
}

// New permits in before old ones out, in one transaction; forward ones in their own.
func TestWindowsSetSplitCIDRsSwapsNewInBeforeOldOut(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.arm([]string{"203.0.113.5"}, false)
	if err := h.setCIDRs("198.51.100.0/24"); err != nil {
		t.Fatal(err)
	}
	h.bfe.ops = nil
	if err := h.setCIDRs("10.20.0.0/16"); err != nil {
		t.Fatalf("SetSplitCIDRs: %v", err)
	}
	h.assertSplitInstalled(false, 0, "10.20.0.0/16")

	txns := transactions(h.bfe.ops)
	if len(txns) != 2 {
		t.Fatalf("got %d transactions, want the ALE swap and the forward swap: %v", len(txns), txns)
	}
	ale := txns[0]
	addNew := opIndex(ale, "add "+splitRangeName+"10.20.0.0/16")
	delOld := opIndex(ale, "delete "+splitRangeName+"198.51.100.0/24")
	if addNew < 0 || delOld < 0 || addNew > delOld || ale[len(ale)-1] != "commit" {
		t.Fatalf("ALE swap not new-in-before-old-out in one transaction: %v", ale)
	}
	fwd := txns[1]
	fwdAdd, fwdDel := opIndex(fwd, "add "+fwdSplitName+"10.20.0.0/16"), opIndex(fwd, "delete "+fwdSplitName+"198.51.100.0/24")
	if fwdAdd < 0 || fwdDel < 0 || fwdAdd > fwdDel || fwd[len(fwd)-1] != "commit" {
		t.Fatalf("forward swap not new-in-before-old-out in its own transaction: %v", fwd)
	}

	h.bfe.ops = nil
	if err := h.setCIDRs("10.20.0.0/16"); err != nil {
		t.Fatal(err)
	}
	if len(h.bfe.ops) != 0 {
		t.Fatalf("an unchanged set touched the engine: %v", h.bfe.ops)
	}
}

func TestWindowsSplitEgressExcludesTheTunnelOnceKnown(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.arm([]string{"203.0.113.5"}, false)
	if err := h.setEgress(true); err != nil {
		t.Fatal(err)
	}
	h.assertSplitInstalled(true, 0)

	if err := h.setEgress(false); err != nil {
		t.Fatal(err)
	}
	h.assertSplitInstalled(false, 0)
	if h.freed != 1 {
		t.Fatalf("app id freed %d times after one install", h.freed)
	}

	if err := h.ks.Update(t.Context(), TunnelRef{Name: "wg0", WindowsLUID: 0x8000_0000_0000_1234}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := h.setEgress(true); err != nil {
		t.Fatal(err)
	}
	h.assertSplitInstalled(true, 0x8000_0000_0000_1234)
}

// Update and DropTunnelPermit move the tunnel permits only; split IDs stay put.
func TestWindowsTunnelPermitChangesLeaveSplitAlone(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.arm([]string{"203.0.113.5"}, false)
	if err := h.ks.Update(t.Context(), TunnelRef{Name: "wg0", WindowsLUID: 0x5001}); err != nil {
		t.Fatal(err)
	}
	if err := h.setCIDRs("198.51.100.0/24"); err != nil {
		t.Fatal(err)
	}
	if err := h.setEgress(true); err != nil {
		t.Fatal(err)
	}
	ids := sortedIDs(slices.Concat(h.ks.splitEgressFilterIds, h.ks.splitCIDRFilterIds, h.ks.forwardSplitFilterIds))

	if err := h.ks.Update(t.Context(), TunnelRef{Name: "wg0", WindowsLUID: 0x5002}); err != nil {
		t.Fatal(err)
	}
	if err := h.ks.DropTunnelPermit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := sortedIDs(slices.Concat(h.ks.splitEgressFilterIds, h.ks.splitCIDRFilterIds, h.ks.forwardSplitFilterIds)); !slices.Equal(got, ids) {
		t.Fatalf("split ids changed from %v to %v", ids, got)
	}
	h.assertSplitInstalled(true, 0x5001, "198.51.100.0/24")
}

// The endpoint/LAN swap of a re-arm retires only endpoint and LAN permits.
func TestWindowsReArmSwapLeavesSplitAlone(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.arm([]string{"203.0.113.5"}, false)
	if err := h.setCIDRs("198.51.100.0/24"); err != nil {
		t.Fatal(err)
	}
	if err := h.setEgress(true); err != nil {
		t.Fatal(err)
	}
	ids := sortedIDs(slices.Concat(h.ks.splitEgressFilterIds, h.ks.splitCIDRFilterIds, h.ks.forwardSplitFilterIds))

	h.arm([]string{"203.0.113.9"}, true)
	if got := sortedIDs(slices.Concat(h.ks.splitEgressFilterIds, h.ks.splitCIDRFilterIds, h.ks.forwardSplitFilterIds)); !slices.Equal(got, ids) {
		t.Fatalf("re-arm swap replaced split ids %v with %v", ids, got)
	}
	h.assertSplitInstalled(true, 0, "198.51.100.0/24")
}

// KS-12: a split removal that failed (the Lockdown narrowing, say) is retired by
// the next arm, whichever re-arm path it takes.
func TestWindowsFailedSplitRemovalIsRetiredByTheNextArm(t *testing.T) {
	for name, endpoints := range map[string][]string{"fast path": {"203.0.113.5"}, "swap": {"203.0.113.9"}} {
		t.Run(name, func(t *testing.T) {
			h := newWindowsSplitHarness(t)
			h.arm([]string{"203.0.113.5"}, false)
			if err := h.setCIDRs("198.51.100.0/24"); err != nil {
				t.Fatal(err)
			}
			if err := h.setEgress(true); err != nil {
				t.Fatal(err)
			}

			h.bfe.failDelete = func(f *fakeFilter) bool { return strings.HasPrefix(f.name, "PangeaVPN Allow Split") }
			if err := h.setCIDRs(); err == nil {
				t.Fatal("SetSplitCIDRs(nil) reported success with every delete failing")
			}
			if err := h.setEgress(false); err == nil {
				t.Fatal("SetSplitEgress(false) reported success with every delete failing")
			}
			if !h.ks.split.empty() {
				t.Fatalf("a failed removal kept %+v desired", h.ks.split)
			}
			h.assertSplitInstalled(true, 0, "198.51.100.0/24")

			h.bfe.failDelete = nil
			h.arm(endpoints, false)
			h.assertSplitInstalled(false, 0)
		})
	}
}

// KS-R2-3: the forward mirror swaps in its own transaction, so a failed forward
// narrowing must be retried by the next arm even though the ALE set already matches.
func TestWindowsFailedForwardSplitRemovalIsRetiredByTheNextArm(t *testing.T) {
	for name, endpoints := range map[string][]string{"fast path": {"203.0.113.5"}, "swap": {"203.0.113.9"}} {
		t.Run(name, func(t *testing.T) {
			h := newWindowsSplitHarness(t)
			if err := h.ks.arm(t.Context(), []string{"203.0.113.5"}, false, true); err != nil {
				t.Fatal(err)
			}
			if err := h.setCIDRs("198.51.100.0/24"); err != nil {
				t.Fatal(err)
			}
			h.assertSplitInstalled(false, 0, "198.51.100.0/24")

			h.bfe.failDelete = func(f *fakeFilter) bool { return strings.HasPrefix(f.name, fwdSplitName) }
			h.warnings = nil
			if err := h.setCIDRs(); err != nil {
				t.Fatalf("a forward-only failure failed the host-side narrowing: %v", err)
			}
			if len(h.warnings) == 0 {
				t.Fatal("the failed forward narrowing was not reported")
			}
			if len(h.bfe.withPrefix(splitRangeName)) != 0 {
				t.Fatal("the host-side range permits outlived the narrowing")
			}

			h.bfe.failDelete = nil
			if err := h.ks.DropTunnelPermit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := h.ks.arm(t.Context(), endpoints, false, true); err != nil {
				t.Fatal(err)
			}
			if fwd := h.bfe.withPrefix(fwdSplitName); len(fwd) != 0 || len(h.ks.forwardSplitFilterIds) != 0 {
				t.Fatalf("%d forward split permits (ids %v) survived the next arm", len(fwd), h.ks.forwardSplitFilterIds)
			}
			h.assertSplitInstalled(false, 0)
			if !h.ks.forwardLock {
				t.Fatal("the retry dropped the forward lock")
			}
		})
	}
}

// A forward split permit that could not be added is retried by the next arm too.
func TestWindowsFailedForwardSplitAddIsRetriedByTheNextArm(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.arm([]string{"203.0.113.5"}, false)
	h.bfe.failAdd = func(name string) bool { return strings.HasPrefix(name, fwdSplitName) }
	if err := h.setCIDRs("198.51.100.0/24"); err != nil {
		t.Fatal(err)
	}
	if len(h.bfe.withPrefix(fwdSplitName)) != 0 {
		t.Fatal("a failed forward add left a permit behind")
	}
	h.bfe.failAdd = nil
	h.arm([]string{"203.0.113.5"}, false)
	h.assertSplitInstalled(false, 0, "198.51.100.0/24")
}

// KS-5: a forward split permit that fails is just not there; the forward lock
// and the other forward permits stay.
func TestWindowsForwardSplitFailureKeepsTheForwardLock(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.arm([]string{"203.0.113.5"}, true)
	lanForward := sortedIDs(h.ks.forwardLANFilterIds)
	if len(lanForward) == 0 {
		t.Fatal("no forward LAN permits to keep")
	}

	h.bfe.failAdd = func(name string) bool { return strings.HasPrefix(name, fwdSplitName) }
	h.warnings = nil
	if err := h.setCIDRs("198.51.100.0/24"); err != nil {
		t.Fatalf("a forward-only failure failed the whole change: %v", err)
	}
	if !h.ks.forwardLock {
		t.Fatal("the forward lock was dropped over an optional permit")
	}
	for _, key := range []windows.GUID{pangeaBlockForwardV4FilterKey, pangeaBlockForwardV6FilterKey} {
		if h.bfe.byKey(key) == nil {
			t.Fatalf("forward block %v removed", key)
		}
	}
	if got := sortedIDs(h.ks.forwardLANFilterIds); !slices.Equal(got, lanForward) {
		t.Fatalf("forward LAN permits changed from %v to %v", lanForward, got)
	}
	if len(h.bfe.named(splitRangeName+"198.51.100.0/24")) != 2 {
		t.Fatal("host-side range permits missing after a forward-only failure")
	}
	if len(h.bfe.withPrefix(fwdSplitName)) != 0 || len(h.ks.forwardSplitFilterIds) != 0 {
		t.Fatal("a half-added forward split set was left behind")
	}
	if len(h.warnings) == 0 {
		t.Fatal("the failure was not reported")
	}
}

// A lock that cannot carry its split permits still arms, without them; the next
// arm retries them.
func TestWindowsFreshArmLocksEvenWhenSplitPermitsFail(t *testing.T) {
	h := newWindowsSplitHarness(t)
	if err := h.setCIDRs("198.51.100.0/24"); err != nil {
		t.Fatal(err)
	}
	h.bfe.failAdd = func(name string) bool { return strings.HasPrefix(name, splitRangeName) }
	h.arm([]string{"203.0.113.5"}, false)

	if h.bfe.byKey(pangeaBlockAllOutboundV4FilterKey) == nil || !h.ks.active {
		t.Fatal("the lock did not arm")
	}
	if len(h.bfe.named("PangeaVPN Allow Endpoint 203.0.113.5")) == 0 {
		t.Fatal("the endpoint permit is missing from the retried lock")
	}
	h.assertSplitInstalled(false, 0)
	if !h.ks.split.equal(splitPermits{CIDRs: []string{"198.51.100.0/24"}}) {
		t.Fatalf("desired split lost: %+v", h.ks.split)
	}

	h.bfe.failAdd = nil
	h.arm([]string{"203.0.113.5"}, false)
	h.assertSplitInstalled(false, 0, "198.51.100.0/24")
}

func TestWindowsSplitEgressWithoutAnAppID(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.appIDErr = errors.New("image path not found")
	if err := h.setEgress(true); err != nil {
		t.Fatalf("an idle switch only records: %v", err)
	}
	h.arm([]string{"203.0.113.5"}, false)
	if h.bfe.byKey(pangeaBlockAllOutboundV4FilterKey) == nil {
		t.Fatal("the lock did not arm")
	}
	h.assertSplitInstalled(false, 0)

	if err := h.setEgress(true); err == nil {
		t.Fatal("SetSplitEgress(true) succeeded with no app id to match")
	}
	if h.ks.split.Egress {
		t.Fatal("a failed egress permit stayed desired")
	}
	h.assertSplitInstalled(false, 0)
}

func TestWindowsDisarmForgetsSplit(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.arm([]string{"203.0.113.5"}, true)
	if err := h.setCIDRs("198.51.100.0/24"); err != nil {
		t.Fatal(err)
	}
	if err := h.setEgress(true); err != nil {
		t.Fatal(err)
	}
	if err := h.ks.disarm(); err != nil {
		t.Fatalf("disarm: %v", err)
	}
	if len(h.bfe.installed) != 0 {
		t.Fatalf("%d filters left after Clear", len(h.bfe.installed))
	}
	if !h.ks.split.empty() || !h.ks.appliedSplit.empty() || h.ks.splitEgressFilterIds != nil || h.ks.splitCIDRFilterIds != nil || h.ks.forwardSplitFilterIds != nil {
		t.Fatalf("split bookkeeping survived Clear: %+v %+v", h.ks.split, h.ks.appliedSplit)
	}
	h.arm([]string{"203.0.113.5"}, false)
	h.assertSplitInstalled(false, 0)
}

// A failed Clear still forgets what was wanted, so a later re-arm narrows.
func TestWindowsFailedDisarmStillForgetsDesiredSplit(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.arm([]string{"203.0.113.5"}, false)
	if err := h.setCIDRs("198.51.100.0/24"); err != nil {
		t.Fatal(err)
	}
	h.bfe.failDelete = func(f *fakeFilter) bool { return f.key == pangeaBlockAllOutboundV4FilterKey }
	if err := h.ks.disarm(); err == nil {
		t.Fatal("disarm reported success with the block undeletable")
	}
	if !h.ks.split.empty() {
		t.Fatalf("desired split survived a Clear: %+v", h.ks.split)
	}
}

func TestWindowsDropForwardLockRetiresForwardSplitPermits(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.arm([]string{"203.0.113.5"}, false)
	if err := h.setCIDRs("198.51.100.0/24"); err != nil {
		t.Fatal(err)
	}
	if len(h.ks.forwardSplitFilterIds) != 1 {
		t.Fatalf("forward split ids = %v, want one", h.ks.forwardSplitFilterIds)
	}
	interfaceIndexForLUID = func(uint64) (uint32, error) { return 0, errors.New("adapter gone") }
	if err := h.ks.Update(t.Context(), TunnelRef{Name: "wg0", WindowsLUID: 0x5001}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if h.ks.forwardLock || h.ks.forwardSplitFilterIds != nil || len(h.bfe.withPrefix(fwdSplitName)) != 0 {
		t.Fatalf("forward split permits outlived the forward lock: ids=%v", h.ks.forwardSplitFilterIds)
	}
	if len(h.bfe.named(splitRangeName+"198.51.100.0/24")) != 2 {
		t.Fatal("dropping the forward lock took the host's range permits with it")
	}

	h.bfe.ops = nil
	if err := h.setCIDRs("10.20.0.0/16"); err != nil {
		t.Fatal(err)
	}
	if len(h.bfe.withPrefix(fwdSplitName)) != 0 {
		t.Fatal("forward split permits added with no forward lock to sit on")
	}
}

// The real call needs neither elevation nor an engine handle.
func TestAppIDFromFileNameNamesTheImage(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	blob, free, err := appIDFromFileName(exe)
	if err != nil {
		t.Fatalf("appIDFromFileName: %v", err)
	}
	raw := slices.Clone(unsafe.Slice(blob.data, blob.size))
	free()
	if len(raw) < 4 || len(raw)%2 != 0 || raw[len(raw)-1] != 0 || raw[len(raw)-2] != 0 {
		t.Fatalf("app id is not a NUL-terminated UTF-16 string: % x", raw)
	}
	units := make([]uint16, 0, len(raw)/2-1)
	for i := 0; i+1 < len(raw)-2; i += 2 {
		units = append(units, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	id := string(utf16.Decode(units))
	if !strings.HasPrefix(id, `\device\`) || !strings.HasSuffix(id, strings.ToLower(filepath.Base(exe))) {
		t.Fatalf("app id %q is not the image's NT path", id)
	}

	if _, _, err := appIDFromFileName(filepath.Join(t.TempDir(), "missing.exe")); err == nil {
		t.Fatal("an app id was made for a file that does not exist")
	}
}
