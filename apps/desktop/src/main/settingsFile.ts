import fs from "node:fs/promises";
import path from "node:path";

export interface SettingsPaths {
  /** Where settings are read from first and always written back to. */
  primary: string;
  /** Copy an older build left in the daemon's directory, or null when there is none. */
  legacy: string | null;
}

function errorCode(error: unknown): string {
  const code = (error as { code?: string } | null)?.code;
  return code ?? (error instanceof Error ? error.name : "unknown");
}

export function isMissingFile(error: unknown): boolean {
  return errorCode(error) === "ENOENT";
}

async function readLegacySettings(legacyPath: string | null): Promise<Record<string, unknown>> {
  if (!legacyPath) {
    return {};
  }
  try {
    return JSON.parse(await fs.readFile(legacyPath, "utf8")) as Record<string, unknown>;
  } catch (err) {
    if (!isMissingFile(err)) {
      console.warn("Could not migrate the settings left in the old state directory:", errorCode(err));
    }
    return {};
  }
}

export async function readSettings(paths: SettingsPaths): Promise<Record<string, unknown>> {
  let raw: string;
  try {
    raw = await fs.readFile(paths.primary, "utf8");
  } catch (err) {
    // Anything but ENOENT is a file we could not read, and the caller's
    // write-back would replace every setting in it with a default.
    if (!isMissingFile(err)) {
      throw err;
    }
    return readLegacySettings(paths.legacy);
  }

  try {
    return JSON.parse(raw) as Record<string, unknown>;
  } catch (err) {
    // Corrupt, not missing: keep it rather than let the write-back erase it.
    console.error("settings.json is corrupt; preserving it as .corrupt and starting fresh:", errorCode(err));
    await fs.rename(paths.primary, `${paths.primary}.corrupt-${Date.now()}`).catch(() => {});
    return {};
  }
}

export async function writeSettings(primary: string, settings: Record<string, unknown>): Promise<void> {
  await fs.mkdir(path.dirname(primary), { recursive: true, mode: 0o700 });
  const tmpPath = `${primary}.tmp-${process.pid}-${Date.now()}`;
  // Write-then-rename: a crash or power loss mid-write leaves the old file
  // intact instead of a truncated one, since rename is atomic on both OSes.
  await fs.writeFile(tmpPath, JSON.stringify(settings, null, 2));
  await fs.rename(tmpPath, primary);
}
