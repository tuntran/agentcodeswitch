// Single place the Wails-generated bindings are imported.
//
// Views go through here so that components stay free of Wails entirely, which is
// what lets vitest render them without the generated bindings existing.
//
// The generated signatures are untyped in places; this module gives them the shapes
// from types.ts.
import * as App from "../wailsjs/go/main/App";
import { EventsOn } from "../wailsjs/runtime/runtime";
import type {
  DoctorView,
  ProfileView,
  PurgeView,
  QuotaView,
} from "./types";

/** Emitted per profile by the background refresh loop. */
export const QUOTA_UPDATED = "quota:updated";

export const listProfiles = (): Promise<ProfileView[]> =>
  App.ListProfiles() as Promise<ProfileView[]>;

export const getQuota = (name: string, force: boolean): Promise<QuotaView> =>
  App.GetQuota(name, force) as Promise<QuotaView>;

export const cachedQuota = (name: string): Promise<QuotaView> =>
  App.CachedQuota(name) as Promise<QuotaView>;

export const subscribeQuota = (): Promise<void> =>
  App.SubscribeQuota() as Promise<void>;

export const onQuotaUpdated = (handler: (quota: QuotaView) => void): void => {
  EventsOn(QUOTA_UPDATED, (quota: QuotaView) => handler(quota));
};

export const createProfileEntry = (
  name: string,
  label: string,
): Promise<ProfileView> =>
  App.CreateProfileEntry(name, label) as Promise<ProfileView>;

/** Takes effect on the next launch. An empty model clears the profile's default. */
export const setProfileModel = (name: string, model: string): Promise<void> =>
  App.SetProfileModel(name, model) as Promise<void>;

/** Takes effect on the next launch. */
export const setProfileContext1M = (
  name: string,
  on: boolean,
): Promise<void> => App.SetProfileContext1M(name, on) as Promise<void>;

export const launchLoginTerminal = (name: string): Promise<void> =>
  App.LaunchLoginTerminal(name) as Promise<void>;

export const launchProfileTerminal = (name: string): Promise<void> =>
  App.LaunchProfileTerminal(name) as Promise<void>;

export const pollLoginDone = (name: string): Promise<boolean> =>
  App.PollLoginDone(name) as Promise<boolean>;

export const cancelAdd = (name: string): Promise<void> =>
  App.CancelAdd(name) as Promise<void>;

export const inspectPurge = (name: string): Promise<PurgeView> =>
  App.InspectPurge(name) as Promise<PurgeView>;

/** Resolves to the path where history was kept, or "" after a purge. */
export const removeProfile = (
  name: string,
  purge: boolean,
  typedName: string,
): Promise<string> =>
  App.RemoveProfile(name, purge, typedName) as Promise<string>;

export const runDoctor = (deep: boolean): Promise<DoctorView> =>
  App.RunDoctor(deep) as Promise<DoctorView>;
