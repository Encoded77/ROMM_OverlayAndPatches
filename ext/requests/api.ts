// Client for the romm-requests service, served on RomM's own origin under
// /ext/requests, so RomM's session cookie authenticates every call.
import { reactive, ref } from "vue";

const BASE = "/ext/requests/api";

export interface Platform {
  slug: string;
  name: string;
  igdb_id: number;
  owned_rom_id?: number;
  /** A library ROM with a similar title on this platform (warning only). */
  resembles?: { rom_id: number; name: string };
}

export interface Game {
  id: number;
  name: string;
  year?: number;
  cover_url?: string;
  summary?: string;
  platforms: Platform[];
}

export type RequestState =
  | "requested"
  | "searching"
  | "needs_choice"
  | "wanted"
  | "downloading"
  | "scanning"
  | "importing"
  | "available"
  | "failed"
  | "cancelled";

export interface Candidate {
  id: string;
  title: string;
  size: number;
  seeders: number;
  indexer: string;
  score: number;
  verdict: "auto" | "candidate" | "reject";
  region?: string;
  format?: string;
  platforms: string[];
  reasons: string[];
}

export interface Live {
  id: number;
  progress: number;
  speed: number;
  eta: number;
  step?: string;
}

export interface GameRequest {
  id: number;
  user_id: number;
  username: string;
  igdb_id: number;
  title: string;
  year?: number;
  cover_url?: string;
  platform_slug: string;
  platform_name: string;
  extras: string[];
  state: RequestState;
  detail?: string;
  rom_id?: number;
  created_at: string;
  updated_at: string;
  candidates: Candidate[];
  chosen?: Candidate;
  searches: number;
  next_search_at?: string;
  placed?: string;
  parent_id?: number;
  extra?: string;
  live?: Live;
}

export const EXTRAS = ["updates", "dlc", "translation", "hacks"] as const;
export type Extra = (typeof EXTRAS)[number];

export const FINISHED: RequestState[] = ["available", "failed", "cancelled"];

export class RequestError extends Error {
  readonly status: number;
  readonly body: Record<string, unknown>;
  constructor(message: string, status: number, body: Record<string, unknown>) {
    super(message);
    this.status = status;
    this.body = body;
  }
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(BASE + path, {
    credentials: "same-origin",
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  });
  if (response.status === 204) return undefined as T;
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new RequestError(body.error ?? `HTTP ${response.status}`, response.status, body);
  }
  return body as T;
}

const post = <T>(path: string, body: unknown = {}) =>
  call<T>(path, { method: "POST", body: JSON.stringify(body) });

export const requestsApi = {
  search: (q: string) => call<Game[]>(`/search?q=${encodeURIComponent(q)}`),
  related: (igdbId: number) => call<Game[]>(`/related?igdb_id=${igdbId}`),
  list: (all = false, history = false) =>
    call<GameRequest[]>(`/requests?all=${all ? 1 : 0}&history=${history ? 1 : 0}`),
  create: (igdbId: number, platformSlug: string, extras: Extra[] = []) =>
    post<GameRequest>("/requests", { igdb_id: igdbId, platform_slug: platformSlug, extras }),
  cancel: (id: number) => post<GameRequest>(`/requests/${id}/cancel`),
  choose: (id: number, candidateId: string) =>
    post<GameRequest>(`/requests/${id}/choose`, { candidate_id: candidateId }),
  retry: (id: number) => post<GameRequest>(`/requests/${id}/retry`),
  searchAgain: (id: number) => post<GameRequest>(`/requests/${id}/search`),
  remove: (id: number) => call<void>(`/requests/${id}`, { method: "DELETE" }),
  settings: () => call<Settings>("/admin/settings"),
  saveSettings: (s: Settings) =>
    call<Settings>("/admin/settings", { method: "PUT", body: JSON.stringify(s) }),
  test: (title: string, platform: string) =>
    post<{ results: Candidate[]; indexer_errors: string[] | null }>("/admin/test", { title, platform }),
};

export interface Rules {
  regions: string[];
  formats: string[];
  deny: string[];
  min_size: number;
  max_size: number;
  set_size: number;
  min_seeders: number;
  convert?: string;
}

export interface Settings {
  auto_download: boolean;
  ttb: { url: string; storage: string; engine: string };
  rules: Record<string, Rules>;
  research_minutes: number[];
  notify: { new_request: boolean; needs_choice: boolean; failed: boolean; available: boolean };
}

// ── Live state, shared by the page and the slots ──────────────────────
//
// One EventSource for the whole app, opened by the first user and closed
// with the last. Request updates replace rows in place; progress events only
// touch `live`, so bars move without refetching anything.

type ServerEvent =
  | { type: "request.updated"; data: GameRequest }
  | { type: "request.deleted"; data: { id: number } }
  | { type: "request.progress"; data: Live };

export const live = reactive({
  connected: false,
  byId: {} as Record<number, GameRequest>,
});
const listeners = new Set<() => void>();
let source: EventSource | null = null;
let users = 0;

export function upsert(request: GameRequest) {
  const previous = live.byId[request.id];
  live.byId[request.id] = { ...request, live: request.live ?? previous?.live };
}

export function connectLive(onResync: () => void) {
  listeners.add(onResync);
  users += 1;
  if (!source) {
    source = new EventSource(`${BASE}/events`, { withCredentials: true });
    source.onopen = () => {
      live.connected = true;
      listeners.forEach((resync) => resync());
    };
    source.onerror = () => (live.connected = false);
    source.onmessage = (message) => {
      const event = JSON.parse(message.data) as ServerEvent;
      if (event.type === "request.updated") {
        upsert(event.data);
      } else if (event.type === "request.deleted") {
        delete live.byId[event.data.id];
      } else if (event.type === "request.progress") {
        const current = live.byId[event.data.id];
        if (current) current.live = event.data;
      }
    };
  }
  return () => {
    listeners.delete(onResync);
    users -= 1;
    if (users === 0 && source) {
      source.close();
      source = null;
      live.connected = false;
    }
  };
}

/** Requests of mine that are still open, by "igdb:platform", for the request buttons. */
export const loadedMine = ref(false);
export async function loadMine() {
  const list = await requestsApi.list();
  list.forEach(upsert);
  loadedMine.value = true;
}

export function formatBytes(bytes: number): string {
  if (!bytes) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / 1024 ** i).toFixed(i > 1 ? 1 : 0)} ${units[i]}`;
}

export function formatEta(seconds: number): string {
  if (!seconds) return "";
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return h ? `${h} h ${m} min` : m ? `${m} min` : `${seconds} s`;
}
