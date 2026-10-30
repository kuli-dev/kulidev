// KuliDev service worker: only shows notifications (Android requires a worker for
// them) and focuses the workbench when one is tapped. It caches nothing.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()));
self.addEventListener("notificationclick", (e) => {
  e.notification.close();
  e.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((cs) =>
      cs.length ? cs[0].focus() : self.clients.openWindow("./#/pulse"),
    ),
  );
});
