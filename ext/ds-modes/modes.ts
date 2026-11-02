// Which roms the DS launches apply to: the platforms the melonDS pool streams.
const DS_PLATFORMS = new Set(["nds"]);

export function isDsRom(rom: { platform_slug?: string | null }): boolean {
  return DS_PLATFORMS.has(rom.platform_slug ?? "");
}
