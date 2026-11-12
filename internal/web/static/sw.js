/* vanDyke service worker.
 *
 * Only push notifications live here. The ledger itself is always rendered by
 * the Go server, so there is no cache to warm or invalidate. The file is served
 * from the origin root so its scope covers every page.
 */

const DEFAULT_ICON = "/static/images/apple-touch-icon.png";

/* Take over at once: a subscription created right after register() must be
 * answered by the worker that is already in control of the page. */
self.addEventListener("install", (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

/* The frame is JSON: {title, body, url, tag, icon, badge, data}. Poor JSON
 * still shows something — a silent notification looks like a broken site. */
self.addEventListener("push", (event) => {
  let payload = {};
  if (event.data) {
    try {
      payload = event.data.json();
    } catch (err) {
      payload = {};
    }
  }

  const title = payload.title || "New entry added";
  const body = payload.body || "A new entry was added.";
  const url = payload.url || "/";

  event.waitUntil(
    self.registration.showNotification(title, {
      body: body,
      icon: payload.icon || DEFAULT_ICON,
      badge: payload.badge || DEFAULT_ICON,
      tag: payload.tag || "vandyke-entry",
      renotify: false,
      data: { url: url },
    })
  );
});

/* Focus a tab we already have and let the page navigate itself; otherwise open
 * a new window. Either way the click lands on the entry that was announced. */
self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = (event.notification.data && event.notification.data.url) || "/";

  event.waitUntil(
    (async () => {
      let windows = [];
      try {
        windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
      } catch (err) {
        windows = [];
      }

      const client = windows.find((candidate) => "focus" in candidate);
      if (client) {
        await client.focus();
        client.postMessage({ type: "vandyke:navigate", url: url });
        return;
      }

      if (self.clients.openWindow) {
        await self.clients.openWindow(url);
      }
    })()
  );
});

/* The browser can rotate or drop a subscription on its own. Only a page knows
 * the VAPID key, so ask an open tab to re-subscribe rather than guessing here. */
self.addEventListener("pushsubscriptionchange", (event) => {
  event.waitUntil(
    self.clients
      .matchAll({ type: "window", includeUncontrolled: true })
      .then((windows) => {
        windows.forEach((client) => client.postMessage({ type: "vandyke:resubscribe" }));
      })
      .catch(() => {
        /* No tab is reachable; the next visit repairs the subscription. */
      })
  );
});
