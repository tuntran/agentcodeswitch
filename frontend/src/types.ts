// Mirrors the *View types in views.go. Kept hand-written rather than generated so
// the null-ness is explicit: `fiveHour: WindowView | null` is the type-level version
// of "absence is data, not zero".

/** The five quota states from internal/quota. */
export type QuotaState =
  | "ok"
  | "no_login"
  | "missing_scope"
  | "expired"
  | "unavailable";

export type Severity = "neutral" | "attention" | "";

export interface WindowView {
  utilization: number;
  /** RFC 3339, or "" when unknown. Never treat "" as the epoch. */
  resetsAt: string;
  severity: Severity;
}

/** The last reading that succeeded. Never current, by construction. */
export interface SnapshotView {
  fetchedAt: string;
  fiveHour: WindowView | null;
  sevenDay: WindowView | null;
}

/**
 * A profile's quota.
 *
 * The contract, mirrored from internal/quota:
 *
 *   fiveHour/sevenDay are non-null  <=>  state === "ok"
 *   lastKnown is non-null            =>  state !== "ok"
 *
 * So drawing a bar is exactly `state === "ok"`, and nothing under `lastKnown` can be
 * mistaken for a live figure. When state is "ok" and `stale` is true, the numbers
 * came from cache and `fetchedAt` gives their age.
 */
export interface QuotaView {
  name: string;
  state: QuotaState;
  /** Always set, including when state is "ok". */
  message: string;
  stale: boolean;
  fetchedAt: string;
  retryAfter: string;
  /** null whenever the number is unknown. Do not default this to a zeroed object. */
  fiveHour: WindowView | null;
  sevenDay: WindowView | null;
  lastKnown: SnapshotView | null;
}

export interface ProfileView {
  name: string;
  label: string;
  email: string;
  plan: string;
  /** Without the "[1m]" suffix. "" means Claude Code decides, which is a real state. */
  model: string;
  /** Extended context, on by default. Has no effect while `model` is empty. */
  context1m: boolean;
  keychainHash: string;
  loggedIn: boolean;
  orgId: string;
  orgName: string;
  configDirLiteral: string;
}

export type CheckStatus = "ok" | "warn" | "fail";

export interface CheckView {
  name: string;
  status: CheckStatus;
  detail: string;
  action: string;
}

export interface DoctorProfileView {
  name: string;
  checks: CheckView[];
}

export interface DoctorView {
  deep: boolean;
  ok: boolean;
  profiles: DoctorProfileView[];
  orphans: string[];
  configError: string;
}

export interface PurgeView {
  dir: string;
  jsonlCount: number;
  oldest: string;
  newest: string;
}
