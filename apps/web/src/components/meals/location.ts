"use client";

import { useEffect, useState } from "react";

import { FALLBACK_POINT, type Point } from "@/lib/meals-api";

/** Where the customer chose to browse kitchens for, kept in this browser. */
export interface Place {
  kind: "address" | "area";
  /** Saved-address id or area id. */
  id: string;
  label: string;
  label_ar?: string | null;
  point: Point;
}

const KEY = "meals.place";
const EVENT = "meals-place";

function readPlace(): Place | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    const p = JSON.parse(raw) as Place;
    return typeof p?.point?.lat === "number" && typeof p.point.lng === "number"
      ? p
      : null;
  } catch {
    return null;
  }
}

/** Remember a chosen place (or clear it) for every page in this browser. */
export function choosePlace(p: Place | null) {
  try {
    if (p) localStorage.setItem(KEY, JSON.stringify(p));
    else localStorage.removeItem(KEY);
  } catch {
    // Private mode: the choice lasts for this page only.
  }
  window.dispatchEvent(new Event(EVENT));
}

/**
 * The customer's point for kitchen discovery. A chosen saved address or area
 * wins; otherwise a rough device location (a district, not a doorstep: no
 * high-accuracy request and a short timeout), falling back to Dubai Marina.
 * `precise` means we know where the customer is browsing for; `exact` means
 * the point is a real address or device fix, good enough to pin an address.
 */
export function useCustomerPoint() {
  const [place, setPlace] = useState<Place | null>(null);
  const [read, setRead] = useState(false);
  const [gps, setGps] = useState<Point | null>(null);
  const [settled, setSettled] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const sync = () => setPlace(readPlace());
    sync();
    setRead(true);
    window.addEventListener(EVENT, sync);
    window.addEventListener("storage", sync);
    return () => {
      window.removeEventListener(EVENT, sync);
      window.removeEventListener("storage", sync);
    };
  }, []);

  useEffect(() => {
    if (!read || place) return;
    if (!navigator.geolocation) {
      setSettled(true);
      return;
    }
    let live = true;
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        if (!live) return;
        setGps({ lat: pos.coords.latitude, lng: pos.coords.longitude });
        setSettled(true);
      },
      () => {
        if (live) setSettled(true);
      },
      { timeout: 4000, maximumAge: attempt > 0 ? 0 : 300_000 },
    );
    return () => {
      live = false;
    };
  }, [read, place, attempt]);

  const point = place
    ? place.point
    : (gps ?? (settled ? FALLBACK_POINT : null));
  return {
    point,
    place,
    precise: place != null || gps != null,
    exact: place?.kind === "address" || (place == null && gps != null),
    choose: choosePlace,
    /** Drop any chosen place and ask the device again. */
    locate: () => {
      setGps(null);
      setSettled(false);
      setAttempt((n) => n + 1);
      choosePlace(null);
    },
  };
}
