import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { HUB_HOSTNAME, HUB_MIRROR_HOSTNAME, normalHubHosts } from "./hubHosts.ts";

describe("normalHubHosts", () => {
  it("tries the primary domain before the mirror", () => {
    assert.deepEqual(normalHubHosts(), [HUB_HOSTNAME, HUB_MIRROR_HOSTNAME]);
  });

  it("has no duplicates", () => {
    const hosts = normalHubHosts();
    assert.equal(new Set(hosts).size, hosts.length);
  });

  it("keeps the mirror distinct from the name the no-SNI paths dial", () => {
    assert.notEqual(HUB_MIRROR_HOSTNAME, HUB_HOSTNAME);
  });
});
