/* vanDyke browser notifications and live in-app updates.
 *
 * Plain ES2020, no build step, idempotent. Every entry point no-ops on a
 * browser without Push or Notification support, and permission is only ever
 * requested from an explicit click — never on load.
 */
(function () {
  "use strict";

  if (window.vandykeNotifications) return;

  const STORAGE_KEY = "vandyke.notifications";
  const SW_PATH = "/sw.js";
  const SW_SCOPE = "/";
  const CONFIG_URL = "/api/v1/push/config";
  const SUBSCRIBE_URL = "/api/v1/push/subscriptions";
  const EVENTS_URL = "/api/v1/events";

  const state = {
    config: null, // {enabled, vapid_public_key, firebase} as served
    subscribed: false, // mirrors pushManager.getSubscription()
  };

  /* Entry ids this tab already reported through the HTMX "entryAdded" trigger.
   * They exist only to stop the same entry toasting twice. */
  const recentlyAdded = Object.create(null);

  function pushSupported() {
    return "Notification" in window && "serviceWorker" in navigator && "PushManager" in window;
  }

  function storageGet() {
    try {
      return window.localStorage.getItem(STORAGE_KEY);
    } catch (err) {
      return null; // storage unavailable (private mode)
    }
  }

  function storageSet(value) {
    try {
      window.localStorage.setItem(STORAGE_KEY, value);
    } catch (err) {
      /* ignore */
    }
  }

  /* VAPID keys travel as base64url; pushManager wants the raw bytes. */
  function urlBase64ToUint8Array(value) {
    const padding = "=".repeat((4 - (value.length % 4)) % 4);
    const base64 = (value + padding).replace(/-/g, "+").replace(/_/g, "/");
    const raw = window.atob(base64);
    const bytes = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i += 1) bytes[i] = raw.charCodeAt(i);
    return bytes;
  }

  async function sendJSON(method, url, body) {
    const response = await fetch(url, {
      method: method,
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify(body),
    });
    if (!response.ok) throw new Error("HTTP " + response.status);
    return response;
  }

  /* Resolves with the permission string; the callback form keeps old Safari
   * working, and a double resolve is harmless. */
  function requestPermission() {
    return new Promise((resolve) => {
      let result;
      try {
        result = window.Notification.requestPermission((value) => resolve(value));
      } catch (err) {
        resolve("default");
        return;
      }
      if (result && typeof result.then === "function") {
        result.then(resolve, () => resolve("default"));
      }
    });
  }

  function usable() {
    return pushSupported() && !!(state.config && state.config.enabled && state.config.vapid_public_key);
  }

  function status() {
    if (!pushSupported()) return "unsupported";
    if (window.Notification.permission === "denied") return "denied";
    if (state.subscribed) return "subscribed";
    if (window.Notification.permission === "granted") return "off";
    return "default";
  }

  /* One place decides what every prompt element looks like. Elements opt in by
   * attribute, so the header strip and the account card stay in step. */
  function paint() {
    const current = status();
    const available = usable();
    const root = document.documentElement;

    if (available) root.removeAttribute("data-notifications-unavailable");
    else root.setAttribute("data-notifications-unavailable", "");

    document.querySelectorAll("[data-notify-strip]").forEach((strip) => {
      strip.hidden = !available;
    });

    document.querySelectorAll("[data-notify-toggle]").forEach((button) => {
      button.hidden = !available || current === "denied";
      button.setAttribute("aria-pressed", current === "subscribed" ? "true" : "false");
      button.setAttribute("data-notify-state", current);
      const label = button.querySelector("[data-notify-label]");
      if (label) label.textContent = current === "subscribed" ? "Notifications on" : "Enable notifications";
    });

    document.querySelectorAll("[data-notify-blocked]").forEach((hint) => {
      hint.hidden = !available || current !== "denied";
    });

    document.querySelectorAll("[data-notify-state-line]").forEach((line) => {
      line.textContent = available ? "web push" : "not configured";
    });
  }

  async function loadConfig() {
    try {
      const response = await fetch(CONFIG_URL, {
        credentials: "same-origin",
        headers: { Accept: "application/json" },
      });
      if (!response.ok) throw new Error("HTTP " + response.status);
      state.config = await response.json();
    } catch (err) {
      state.config = null;
    }
    return state.config;
  }

  async function refreshSubscription() {
    if (!pushSupported()) return false;
    try {
      const registration = await navigator.serviceWorker.getRegistration(SW_PATH);
      if (!registration) {
        state.subscribed = false;
        return false;
      }
      const subscription = await registration.pushManager.getSubscription();
      state.subscribed = !!subscription;
    } catch (err) {
      state.subscribed = false;
    }
    return state.subscribed;
  }

  async function enable() {
    if (!pushSupported()) return "unsupported";
    if (window.Notification.permission === "denied") {
      paint();
      return "denied";
    }
    if (!usable()) return "unavailable";

    const permission = await requestPermission();
    if (permission !== "granted") {
      paint();
      return permission === "denied" ? "denied" : "default";
    }

    try {
      const registration = await navigator.serviceWorker.register(SW_PATH, { scope: SW_SCOPE });
      const subscription = await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(state.config.vapid_public_key),
      });
      const json = typeof subscription.toJSON === "function" ? subscription.toJSON() : {};
      const keys = (json && json.keys) || {};

      await sendJSON("POST", SUBSCRIBE_URL, {
        endpoint: subscription.endpoint,
        keys: { p256dh: keys.p256dh || "", auth: keys.auth || "" },
        user_agent: navigator.userAgent,
      });

      state.subscribed = true;
      storageSet("on");
      paint();
      return "subscribed";
    } catch (err) {
      /* A refused subscribe is usually a key mismatch or a browser without a
       * notification service; leave the switch off and let the user retry. */
      await refreshSubscription();
      paint();
      return "error";
    }
  }

  async function disable() {
    if (!pushSupported()) return "unsupported";

    try {
      const registration = await navigator.serviceWorker.getRegistration(SW_PATH);
      if (registration) {
        const subscription = await registration.pushManager.getSubscription();
        if (subscription) {
          /* The server route is DELETE /api/v1/push/subscriptions and takes the
           * endpoint in the body; the endpoint is the only identifier we send. */
          await sendJSON("DELETE", SUBSCRIBE_URL, { endpoint: subscription.endpoint }).catch(() => {
            /* The server may already have forgotten this registration. */
          });
          await subscription.unsubscribe();
        }
      }
    } catch (err) {
      /* Fall through: forget it locally too, the server prunes dead endpoints. */
    }

    state.subscribed = false;
    storageSet("off");
    paint();
    return "off";
  }

  /* Hand an entry to the page's own toast and refresh code. app.js listens for
   * "entryAdded" on <body>, so the same event is re-dispatched there with the
   * same detail. */
  function announce(entry) {
    if (!document.body) return;
    if (entry && entry.id && recentlyAdded[entry.id]) return; // this tab reported it
    if (document.querySelector("dialog[open]")) return; // never disturb an open dialog
    document.body.dispatchEvent(new CustomEvent("entryAdded", { bubbles: true, detail: entry || {} }));
  }

  function rememberLocal(event) {
    const detail = event.detail || {};
    if (detail.id) recentlyAdded[detail.id] = Date.now();

    const ids = Object.keys(recentlyAdded);
    if (ids.length > 20) ids.forEach((id) => delete recentlyAdded[id]);
  }

  /* One EventSource, reconnected by the browser whenever it drops. */
  function connectEvents() {
    if (!("EventSource" in window)) return;
    const source = new window.EventSource(EVENTS_URL);

    source.addEventListener("entry.created", (event) => {
      let entry = {};
      try {
        entry = JSON.parse(event.data || "{}");
      } catch (err) {
        entry = {};
      }

      document.dispatchEvent(new CustomEvent("vandyke:entry-created", { detail: entry }));

      /* The tab that added the entry also receives an HTMX "entryAdded"
       * trigger. Let that land first, then announce, so one entry means one
       * toast whoever added it. */
      window.setTimeout(() => announce(entry), 250);
    });
  }

  function onWorkerMessage(event) {
    const data = event.data || {};
    if (data.type === "vandyke:navigate" && data.url) {
      window.location.assign(data.url);
      return;
    }
    if (data.type === "vandyke:resubscribe" && storageGet() === "on") {
      enable();
    }
  }

  function onToggleClick(event) {
    const button = event.target && event.target.closest ? event.target.closest("[data-notify-toggle]") : null;
    if (!button || button.getAttribute("aria-busy") === "true") return;

    event.preventDefault();
    button.setAttribute("aria-busy", "true");
    const action = state.subscribed ? disable() : enable();
    action.then(
      () => button.removeAttribute("aria-busy"),
      () => button.removeAttribute("aria-busy")
    );
  }

  window.vandykeNotifications = { enable: enable, disable: disable, status: status };

  function init() {
    connectEvents();

    if (!pushSupported()) {
      document.documentElement.setAttribute("data-notifications-unavailable", "");
      return;
    }

    navigator.serviceWorker.addEventListener("message", onWorkerMessage);
    document.addEventListener("click", onToggleClick);
    if (document.body) document.body.addEventListener("entryAdded", rememberLocal, true);

    loadConfig().then(refreshSubscription).then(paint);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init, { once: true });
  } else {
    init();
  }
})();
