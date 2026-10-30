/* Service worker — GestRip standalone.
 *
 * Shaped like the source's `static/sw.js`: one cache, precache on install,
 * skipWaiting, delete every other cache on activate, clients.claim, and a
 * network-first fetch that falls back to the cache. The update path is the
 * source's whole update path — there is no banner and no reload prompt in the
 * source either; a new worker takes over on the next load.
 *
 * ONE LINE DIVERGES, AND IT IS THE POINT. The source stores every GET it sees
 * (`static/sw.js:35-43`) — every one, with no check on the status: a 302 to the
 * login page and a 500 go into the cache beside a page. On a shop device that means customer
 * names, telephone numbers, the photographs of their pieces and their receipt
 * PDFs, written into a cache that survives signing out and is cleared only when
 * the cache name changes. Here an address must be on the allow-list below
 * before anything is written, and everything else goes to the network and is
 * stored nowhere.
 *
 * The allow-list is the shell: the manifest, the two icons, and whatever the
 * asset prefix serves — which is build output and nothing else, because
 * `Plug.Static` is mounted there and no route of this application matches it.
 * None of them names a customer, and all of them are the same bytes for every
 * visitor. An address carrying a query string is refused as well, so a cache
 * key cannot be multiplied without bound by something appended to a URL.
 *
 * The source's `?al=` navigation bypass (`static/sw.js:32-34`) has no
 * equivalent: it exists for the multi-tenant autologin parameter, and this
 * build has no such parameter. Navigations are not stored here in any case,
 * because a page is never on the allow-list.
 */
const CACHE = "gestrip-v1";
/* Written in by `GestripStandalone.Pwa.service_worker/0` from the one list.
   `addAll` rejects atomically, so an address spelled here by hand and moved
   over there would make this worker fail to install for everybody, silently. */
const PRECACHE = __PRECACHE__;

/* The single decision. Same origin, no query string, and either one of the
   precached shell addresses or something under the asset prefix, which serves
   build output only. */
function storable(url) {
  if (url.origin !== self.location.origin) return false;
  if (url.search) return false;
  return url.pathname.startsWith("/assets/") || PRECACHE.indexOf(url.pathname) !== -1;
}

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(PRECACHE)));
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(keys.filter((key) => key !== CACHE).map((key) => caches.delete(key)))
      )
  );
  self.clients.claim();
});

self.addEventListener("fetch", (event) => {
  if (event.request.method !== "GET") return;

  let url;
  try {
    url = new URL(event.request.url);
  } catch (e) {
    return;
  }

  if (!storable(url)) return;

  event.respondWith(
    fetch(event.request)
      .then((response) => {
        if (response.ok) {
          const copy = response.clone();
          caches
            .open(CACHE)
            .then((cache) => cache.put(event.request, copy))
            .catch(() => {});
        }
        return response;
      })
      .catch(() =>
        caches.match(event.request).then((hit) => hit || Response.error())
      )
  );
});
