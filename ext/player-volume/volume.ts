// Volume slider and Mute for every EmulatorJS core, not only the OpenAL ones.
//
// EmulatorJS's setVolume only sets the gain of the OpenAL sources. The nightly
// build RomM loads when netplay is enabled has cores that play through
// RetroArch's rwebaudio driver, which connects each buffer straight to the
// AudioContext's destination: there the slider and Mute did nothing.
//
// installMasterVolume() reroutes every connection to a destination through one
// master GainNode per context. It runs at app start, before the player installs
// upstream's netplay host audio tap (Player.vue), so the tap wraps this connect:
// what netplay sends to guests is captured ahead of the master gain, and the
// host's volume stays local. The player hook drives the master gain from
// EmulatorJS's setVolume whenever OpenAL is not already carrying the volume.
import type { PlayerSession } from "../_core/types";

type NodeMethod = (this: AudioNode, ...args: unknown[]) => unknown;

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const emulator = (): any => (window as any).EJS_emulator;

const masters = new Map<BaseAudioContext, GainNode>();
let level = 1;
let nativeConnect: NodeMethod | null = null;

function masterFor(context: BaseAudioContext): GainNode {
  let master = masters.get(context);
  if (!master) {
    for (const known of masters.keys()) {
      if (known.state === "closed") masters.delete(known);
    }
    master = context.createGain();
    master.gain.value = level;
    Reflect.apply(nativeConnect!, master, [context.destination]);
    masters.set(context, master);
  }
  return master;
}

/** Arguments with a context's destination swapped for its master gain. */
function routed(node: AudioNode, args: unknown[]): unknown[] {
  const { context } = node;
  if (args[0] !== context.destination) return args;
  return [masterFor(context), ...args.slice(1)];
}

export function installMasterVolume() {
  if (nativeConnect) return;
  const proto = AudioNode.prototype as unknown as {
    connect: NodeMethod;
    disconnect: NodeMethod;
  };
  const connect = proto.connect;
  const disconnect = proto.disconnect;
  nativeConnect = connect;
  proto.connect = function (...args) {
    const target = routed(this, args);
    const result = Reflect.apply(connect, this, target);
    // Callers chaining on connect() expect the node they passed back.
    return target === args ? result : args[0];
  };
  proto.disconnect = function (...args) {
    return Reflect.apply(disconnect, this, routed(this, args));
  };
}

function setLevel(volume: number) {
  level = volume;
  for (const [context, master] of masters) {
    if (context.state === "closed") masters.delete(context);
    else master.gain.value = volume;
  }
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function carriedElsewhere(emu: any): boolean {
  // A netplay guest's volume is applied by netplay to the host's stream.
  if (emu.isNetplay && emu.netplay && !emu.netplay.owner) return true;
  const sources = emu.Module?.AL?.currentCtx?.sources;
  if (!sources) return false;
  return (sources.length ?? sources.size ?? Object.keys(sources).length) > 0;
}

export function createPlayerVolume(): PlayerSession {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let patched: any = null;

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const apply = (emu: any, volume: number) =>
    setLevel(carriedElsewhere(emu) ? 1 : volume);

  return {
    gameStarted() {
      const emu = emulator();
      if (!emu?.setVolume || patched === emu) return;
      const setVolume = emu.setVolume;
      emu.setVolume = function (volume: number) {
        const result = setVolume.call(this, volume);
        apply(this, volume);
        return result;
      };
      patched = emu;
      // The saved level was applied before the game reported started.
      apply(emu, emu.muted ? 0 : emu.volume);
    },
    dispose() {
      patched = null;
      setLevel(1);
    },
  };
}
