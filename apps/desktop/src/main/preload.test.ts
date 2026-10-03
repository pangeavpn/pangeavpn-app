import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { IPC_CHANNELS } from "../shared/ipc.ts";

test("preload's inlined channel map matches IPC_CHANNELS", async () => {
  const source = await readFile(new URL("./preload.ts", import.meta.url), "utf8");
  const block = /const CH = \{([\s\S]*?)\r?\n\}( as const)?;/.exec(source);
  assert.ok(block, "no `const CH = { ... }` block in preload.ts");
  const lines = block[1].split(/\r?\n/).filter((line) => line.trim() !== "" && !line.trim().startsWith("//"));
  const entries = lines.map((line) => {
    const match = /^\s*(\w+):\s*"([^"]+)",?\s*$/.exec(line);
    assert.ok(match, `unparsed line in preload's CH map: ${line.trim()}`);
    return [match[1], match[2]] as const;
  });
  assert.equal(new Set(entries.map(([key]) => key)).size, entries.length, "duplicate key in preload's CH map");
  assert.deepEqual(Object.fromEntries(entries), { ...IPC_CHANNELS });
});
