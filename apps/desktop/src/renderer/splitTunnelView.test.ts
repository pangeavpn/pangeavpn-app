import assert from "node:assert/strict";
import test from "node:test";
import type { Translate } from "./hubStatusText.ts";
import {
  fallbackSplitEntry,
  foldForSearch,
  groupSplitRows,
  isolateLtr,
  middleEllipsis,
  placeChildren,
  splitAppErrorText,
  splitAppsUnavailable,
  splitChipVisible,
  splitCidrErrorText,
  splitCidrsAdjusted,
  splitEntryKey,
  splitInvalidByRule,
  splitPaneNote,
  splitPickerSummary,
  splitRailSummary,
  splitRangesSaveError,
  splitRowNote,
  splitRuleKey,
  splitStatusOf
} from "./splitTunnelView.ts";

// Echoes the key and params so assertions pin the mapping, not the English copy.
const t: Translate = (key, params) => (params ? `${key}${JSON.stringify(params)}` : key);

function entry(name: string, rule: string, extra: Partial<SplitTunnelAppEntry> = {}): SplitTunnelAppEntry {
  return { id: `id:${name}`, name, rule, exe: rule, kind: "file", ...extra };
}

test("rule keys compare Windows and macOS paths without case", () => {
  assert.equal(splitRuleKey("C:/Games/Foo.EXE", "win32"), "c:\\games\\foo.exe");
  assert.equal(splitRuleKey("/Applications/Foo.app/", "darwin"), "/applications/foo.app");
  assert.equal(splitRuleKey("/opt/Foo/", "linux"), "/opt/Foo/");
  assert.equal(splitEntryKey({ id: "x", rule: "C:\\A.exe" }, "win32"), "rule:c:\\a.exe");
  assert.equal(splitEntryKey({ id: "desktop:flatpak", rule: "" }, "linux"), "id:desktop:flatpak");
});

test("middleEllipsis keeps the last two segments and the start of the path", () => {
  const dir = "C:\\Users\\me\\AppData\\Local\\Discord\\";
  assert.equal(middleEllipsis(dir, dir.length), dir);
  assert.equal(middleEllipsis(dir, 25), "C:\\Users\\…\\Local\\Discord\\");
  assert.equal(middleEllipsis(dir, 25).length, 25);
  const exe = "C:\\Program Files\\Microsoft VS Code\\Code.exe";
  assert.equal(middleEllipsis(exe, 32), "C:\\P…\\Microsoft VS Code\\Code.exe");
});

test("middleEllipsis falls back to the last segment, then to the end of the path", () => {
  const exe = "C:\\Program Files\\Microsoft VS Code\\Code.exe";
  assert.equal(middleEllipsis(exe, 20), "C:\\Program…\\Code.exe");
  assert.equal(middleEllipsis("/Applications/Utilities/Terminal.app", 14), "…/Terminal.app");
  assert.equal(middleEllipsis("/Applications/Utilities/Terminal.app", 6), "…l.app");
  assert.equal(middleEllipsis("abcdef", 1), "…");
});

test("fallback entries name the rule by its last segment", () => {
  assert.deepEqual(fallbackSplitEntry("C:\\Games\\Foo\\", "win32"), {
    id: "rule:c:\\games\\foo\\",
    name: "Foo",
    rule: "C:\\Games\\Foo\\",
    exe: "",
    kind: "dir"
  });
  assert.equal(fallbackSplitEntry("/Applications/Safari.app", "darwin").kind, "bundle");
  assert.equal(fallbackSplitEntry("/Applications/Safari.app", "darwin").name, "Safari");
  assert.equal(fallbackSplitEntry("C:\\Tools\\curl.exe", "win32").name, "curl");
});

test("search ignores case and accents and also matches the path", () => {
  assert.equal(foldForSearch("Éditeur Ñandú"), "editeur nandu");
  const entries = new Map<string, SplitTunnelAppEntry>([
    ["a", entry("Éditeur", "C:\\Apps\\editor.exe")],
    ["b", entry("Steam", "C:\\Program Files (x86)\\Steam\\steam.exe")],
    ["c", entry("Game", "D:\\SteamLibrary\\steamapps\\common\\Game\\", { kind: "dir" })]
  ]);
  const base = { entries, pinned: [], excluded: new Set<string>(), catalog: ["a", "b", "c"], locale: "en" };
  assert.deepEqual(groupSplitRows({ ...base, query: "edit" }).catalog, ["a"]);
  assert.deepEqual(groupSplitRows({ ...base, query: "STEAM" }).catalog, ["c", "b"]);
  assert.deepEqual(groupSplitRows({ ...base, query: "  " }).catalog, ["a", "c", "b"]);
});

test("grouping puts browsed picks first, then excluded by name, and never lists a row twice", () => {
  const entries = new Map<string, SplitTunnelAppEntry>([
    ["zoom", entry("Zoom", "z")],
    ["app10", entry("App 10", "a10")],
    ["app9", entry("App 9", "a9")],
    ["picked", entry("Picked", "p")],
    ["chrome", entry("Chrome", "c")]
  ]);
  const groups = groupSplitRows({
    entries,
    pinned: ["picked", "gone"],
    excluded: new Set(["zoom", "app10", "app9", "picked"]),
    catalog: ["chrome", "zoom", "app9"],
    query: "",
    locale: "en"
  });
  assert.deepEqual(groups.excluded, ["picked", "app9", "app10", "zoom"]);
  assert.deepEqual(groups.catalog, ["chrome"]);
});

test("rail summary counts apps and ranges with plurals, and says Off when disabled", () => {
  const on = { enabled: true, apps: ["a", "b", "c"], cidrs: ["10.0.0.0/8"] };
  assert.equal(
    splitRailSummary("loaded", on, t),
    'settings.splitTunnel.summary.apps{"count":3} · settings.splitTunnel.summary.ranges{"count":1}'
  );
  assert.equal(splitRailSummary("loaded", { ...on, cidrs: [] }, t), 'settings.splitTunnel.summary.apps{"count":3}');
  assert.equal(splitRailSummary("loaded", { enabled: true, apps: [], cidrs: [] }, t), "settings.splitTunnel.summary.empty");
  assert.equal(splitRailSummary("loaded", { ...on, enabled: false }, t), "settings.summary.off");
  assert.equal(splitRailSummary("unsupported", null, t), "settings.splitTunnel.summary.unavailable");
  assert.equal(splitRailSummary("loading", null, t), "");
  assert.equal(splitRailSummary("unreachable", on, t), "");
  assert.equal(splitPickerSummary({ enabled: false, apps: [], cidrs: [] }, t), "settings.splitTunnel.apps.none");
  assert.equal(splitPickerSummary(on, t), 'settings.splitTunnel.summary.apps{"count":3}');
});

test("every app error code has its own message and unknown codes fall back", () => {
  const codes: SplitTunnelErrorCode[] = [
    "notAbsolute",
    "tooLong",
    "nul",
    "unsupportedForm",
    "systemProcess",
    "tooBroad",
    "ownImage",
    "tooMany",
    "duplicate"
  ];
  for (const code of codes) assert.equal(splitAppErrorText(code, t), `settings.splitTunnel.error.apps.${code}`);
  assert.equal(splitAppErrorText("invalid", t), "settings.splitTunnel.error.apps.invalid");
  assert.equal(splitAppErrorText("notIPv4", t), "settings.splitTunnel.error.apps.invalid");
});

test("range errors name the bad tokens once per kind and keep them left-to-right", () => {
  const text = splitCidrErrorText(
    [
      { field: "cidrs", index: 1, code: "notIPv4", value: "nope" },
      { field: "cidrs", index: 3, code: "notIPv4", value: "2001:db8::/32" },
      { field: "cidrs", index: 0, code: "prefixTooShort", value: "10.0.0.0/4" },
      { field: "cidrs", index: 0, code: "tooManyRoutes" },
      { field: "cidrs", index: 2, code: "invalid", value: "x" },
      { field: "apps", index: 0, code: "tooLong", value: "C:\\a.exe" }
    ],
    t
  );
  assert.equal(
    text,
    [
      `settings.splitTunnel.error.cidrs.notIPv4${JSON.stringify({ entries: `${isolateLtr("nope")}, ${isolateLtr("2001:db8::/32")}` })}`,
      `settings.splitTunnel.error.cidrs.prefixTooShort${JSON.stringify({ entries: isolateLtr("10.0.0.0/4") })}`,
      `settings.splitTunnel.error.cidrs.invalid${JSON.stringify({ entries: isolateLtr("x") })}`,
      "settings.splitTunnel.error.cidrs.tooManyRoutes"
    ].join(" ")
  );
});

test("rejected apps map back to their rows by rule", () => {
  const map = splitInvalidByRule(
    [
      { field: "apps", index: 2, code: "systemProcess", value: "C:\\Windows\\explorer.exe" },
      { field: "apps", index: 3, code: "tooMany" },
      { field: "cidrs", index: 0, code: "notIPv4", value: "x" }
    ],
    "win32"
  );
  assert.deepEqual([...map], [["rule:c:\\windows\\explorer.exe", "systemProcess"]]);
});

test("apps-unavailable reasons map to messages; only an unsupported service blocks the list", () => {
  assert.deepEqual(splitAppsUnavailable({ appsSupported: true, unavailableReason: "" }, t), null);
  assert.deepEqual(splitAppsUnavailable({ appsSupported: false, unavailableReason: "" }, t), {
    text: "settings.splitTunnel.appsUnavailable.unsupportedOS",
    blocking: true
  });
  assert.deepEqual(splitAppsUnavailable({ appsSupported: false, unavailableReason: "classifierFailed" }, t), {
    text: "settings.splitTunnel.appsUnavailable.classifierFailed",
    blocking: true
  });
  for (const reason of ["egressFailed", "permitFailed", "stackFailed", "strictReversePath"]) {
    assert.deepEqual(splitAppsUnavailable({ appsSupported: true, unavailableReason: reason }, t), {
      text: `settings.splitTunnel.appsUnavailable.${reason}`,
      blocking: false
    });
  }
  assert.equal(
    splitAppsUnavailable({ appsSupported: true, unavailableReason: "somethingNew" }, t)?.text,
    "settings.splitTunnel.appsUnavailable.unknown"
  );
  assert.equal(
    splitAppsUnavailable({ appsSupported: false, unavailableReason: "toString" }, t)?.text,
    "settings.splitTunnel.appsUnavailable.unsupportedOS"
  );
});

test("row notes put a rejection first, then a missing app, then caveats", () => {
  const steam = entry("Steam", "C:\\Steam\\steam.exe", { warning: "inherits" });
  assert.deepEqual(splitRowNote(steam, "systemProcess", t), {
    text: "settings.splitTunnel.error.apps.systemProcess",
    tone: "error"
  });
  assert.deepEqual(splitRowNote({ ...steam, missing: true }, undefined, t), {
    text: "settings.splitTunnel.apps.missing",
    tone: "error"
  });
  assert.deepEqual(splitRowNote(steam, undefined, t), {
    text: `settings.splitTunnel.apps.inherits${JSON.stringify({ name: "\u2068Steam\u2069" })}`,
    tone: "warn"
  });
  assert.equal(splitRowNote(entry("Safari", "/Applications/Safari.app", { warning: "mayNotWork" }), undefined, t)?.tone, "warn");
  assert.deepEqual(splitRowNote(entry("Krita", "", { unsupported: "appimage" }), undefined, t), {
    text: "settings.splitTunnel.apps.unsupported.appimage",
    tone: "muted"
  });
  assert.equal(splitRowNote(entry("Notes", "C:\\notes.exe"), undefined, t), null);
});

test("adjusted ranges are told apart from bare addresses filled out to /32", () => {
  assert.equal(splitCidrsAdjusted("203.0.113.7, 10.0.0.0/8", ["203.0.113.7/32", "10.0.0.0/8"]), false);
  assert.equal(splitCidrsAdjusted("10.0.0.0/8 203.0.113.7", ["203.0.113.7/32", "10.0.0.0/8"]), false);
  assert.equal(splitCidrsAdjusted("10.1.2.3/8", ["10.0.0.0/8"]), true);
  assert.equal(splitCidrsAdjusted("10.0.0.0/8, 10.1.0.0/16", ["10.0.0.0/8"]), true);
  assert.equal(splitCidrsAdjusted("", []), false);
});

test("the hero chip needs a connected tunnel, split tunnelling on, and something excluded", () => {
  const status = { state: "CONNECTED", splitTunnel: { enabled: true, appCount: 2, cidrCount: 0, pending: false } };
  assert.equal(splitChipVisible(true, status), true);
  assert.equal(splitChipVisible(false, status), false);
  assert.equal(splitChipVisible(true, { splitTunnel: { enabled: false, appCount: 2 } }), false);
  assert.equal(splitChipVisible(true, { splitTunnel: { enabled: true, appCount: 0, cidrCount: 0 } }), false);
  assert.equal(splitChipVisible(true, { splitTunnel: { enabled: true, cidrCount: 1 } }), true);
  assert.equal(splitChipVisible(true, { state: "CONNECTED" }), false);
  assert.equal(splitChipVisible(true, null), false);
  assert.deepEqual(splitStatusOf({ splitTunnel: { enabled: true, appCount: "3", pending: true, unavailableReason: 4 } }), {
    enabled: true,
    rules: 0,
    pending: true,
    unavailableReason: "",
    cidrsDropped: false
  });
});

test("the status block says when this connection keeps the ranges in the tunnel", () => {
  assert.equal(splitStatusOf({ splitTunnel: { enabled: true, cidrCount: 64, cidrsDropped: true } })?.cidrsDropped, true);
  assert.equal(splitStatusOf({ splitTunnel: { enabled: true, cidrCount: 64 } })?.cidrsDropped, false);
  assert.equal(splitStatusOf({ splitTunnel: { enabled: true, cidrsDropped: "true" } })?.cidrsDropped, false);
});

test("the picker says when split tunnelling is off, unless apps can't be excluded at all", () => {
  assert.deepEqual(splitPaneNote(null, false, t), { text: "settings.splitTunnel.paneOff", tone: "warn", turnOn: true });
  assert.equal(splitPaneNote(null, true, t), null);
  assert.equal(splitPaneNote(null, null, t), null);
  assert.deepEqual(splitPaneNote({ text: "unsupported", blocking: true }, false, t), { text: "unsupported", tone: "error", turnOn: false });
  assert.deepEqual(splitPaneNote({ text: "egress", blocking: false }, true, t), { text: "egress", tone: "warn", turnOn: false });
});

test("a ranges save refused only over a stored app still says the ranges weren't saved", () => {
  assert.equal(
    splitRangesSaveError([{ field: "apps", index: 0, code: "ownImage", value: "C:\\Program Files\\PangeaVPN\\" }], "win32", t),
    `settings.splitTunnel.ranges.blockedByApps${JSON.stringify({ apps: "\u2068PangeaVPN\u2069" })}`
  );
  assert.equal(
    splitRangesSaveError(
      [
        { field: "apps", index: 0, code: "tooBroad", value: "/opt/a/" },
        { field: "apps", index: 1, code: "tooBroad", value: "/opt/a/" },
        { field: "apps", index: 2, code: "tooMany" }
      ],
      "linux",
      t
    ),
    `settings.splitTunnel.ranges.blockedByApps${JSON.stringify({ apps: "\u2068a\u2069" })}`
  );
  const mixed = [
    { field: "apps", index: 0, code: "ownImage", value: "C:\\P\\" },
    { field: "cidrs", index: 1, code: "tooManyRoutes" }
  ] satisfies SplitTunnelInvalid[];
  assert.equal(splitRangesSaveError(mixed, "win32", t), splitCidrErrorText(mixed, t));
  assert.equal(splitRangesSaveError([], "win32", t), "");
});

/** Just enough of a DOM parent to see which children get detached on the way to a new order. */
class FakeNode {
  parentNode: FakeNode | null = null;
  childNodes: FakeNode[] = [];
  detached: string[] = [];
  readonly name: string;
  constructor(name: string) {
    this.name = name;
  }
  get firstChild(): FakeNode | null {
    return this.childNodes[0] ?? null;
  }
  get nextSibling(): FakeNode | null {
    const siblings = this.parentNode?.childNodes ?? [];
    return siblings[siblings.indexOf(this) + 1] ?? null;
  }
  private take(node: FakeNode, detaches: boolean): void {
    if (node.parentNode) {
      node.parentNode.childNodes.splice(node.parentNode.childNodes.indexOf(node), 1);
      if (detaches) this.detached.push(node.name);
    }
    node.parentNode = null;
  }
  private put(node: FakeNode, ref: FakeNode | null): void {
    const at = ref ? this.childNodes.indexOf(ref) : this.childNodes.length;
    this.childNodes.splice(at, 0, node);
    node.parentNode = this;
  }
  removeChild(node: FakeNode): void {
    this.take(node, true);
  }
  insertBefore(node: FakeNode, ref: FakeNode | null): void {
    this.take(node, true);
    this.put(node, ref);
  }
  moveBefore(node: FakeNode, ref: FakeNode | null): void {
    this.take(node, false);
    this.put(node, ref);
  }
  replaceChildren(...nodes: FakeNode[]): void {
    for (const child of [...this.childNodes]) this.take(child, true);
    for (const node of nodes) this.insertBefore(node, null);
  }
}

test("placeChildren reorders the list without detaching rows that stay, so a focused switch keeps focus", () => {
  const list = new FakeNode("list");
  const [title, a, b, loading, c, d] = ["title", "a", "b", "loading", "c", "d"].map((name) => new FakeNode(name));
  list.replaceChildren(title, a, b, loading);
  list.detached = [];
  const place = (nodes: FakeNode[]) => placeChildren(list as unknown as Element, nodes as unknown as Node[]);

  place([title, a, b, c, d]);
  assert.deepEqual(list.childNodes.map((node) => node.name), ["title", "a", "b", "c", "d"]);
  assert.deepEqual(list.detached, ["loading"]);

  place([title, d, b, a]);
  assert.deepEqual(list.childNodes.map((node) => node.name), ["title", "d", "b", "a"]);
  assert.deepEqual(list.detached, ["loading", "c"]);
});
