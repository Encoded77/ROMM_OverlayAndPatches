// Which roms the pool launches apply to: the platforms the external emulator pools
// stream. Every one gets Multiplayer (a private screen per player); only the DS also
// gets Shared screen, since only melonDS links several consoles in one process.
const POOL_PLATFORMS = new Set(["nds", "3ds"]);
const SHARED_SCREEN_PLATFORMS = new Set(["nds"]);

export function isPoolRom(rom: { platform_slug?: string | null }): boolean {
  return POOL_PLATFORMS.has(rom.platform_slug ?? "");
}

export function hasSharedScreen(rom: { platform_slug?: string | null }): boolean {
  return SHARED_SCREEN_PLATFORMS.has(rom.platform_slug ?? "");
}
