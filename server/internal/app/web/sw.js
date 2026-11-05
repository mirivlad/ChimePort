/* ChimePort service worker: app shell only; NEVER cache API responses or user data. */
'use strict';
const CACHE = 'chimeport-shell-v1';
const SHELL = [
  '/', '/style.css', '/themes.css', '/theme.js', '/app.js', '/push.js',
  '/chimeport-icon.svg', '/manifest.webmanifest',
  '/icons/icon-192.png', '/icons/icon-512.png'
];

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    for (const key of await caches.keys()) {
      if (key.startsWith('chimeport-shell-') && key !== CACHE) await caches.delete(key);
    }
    await self.clients.claim();
  })());
});

self.addEventListener('fetch', (event) => {
  const request = event.request;
  const url = new URL(request.url);
  if (request.method !== 'GET' || url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return;
  if (request.mode === 'navigate' && (url.pathname === '/' || url.pathname === '/examples')) {
    event.respondWith(fetch(request).catch(async () => {
      return (await caches.match(url.pathname)) || (await caches.match('/'));
    }));
    return;
  }
  if (SHELL.includes(url.pathname)) {
    event.respondWith((async () => {
      try {
        const response = await fetch(request);
        if (response.ok && response.type === 'basic') {
          const cache = await caches.open(CACHE);
          await cache.put(url.pathname, response.clone());
        }
        return response;
      } catch (_) {
        return caches.match(url.pathname);
      }
    })());
  }
});

self.addEventListener('push', (event) => {
  event.waitUntil((async () => {
    let notice = {};
    try { notice = event.data ? event.data.json() : {}; } catch (_) { /* Show safe fallback. */ }
    const id = /^\d+$/.test(String(notice.id || '')) ? String(notice.id) : '';
    const title = String(notice.title || 'Новое уведомление ChimePort').slice(0, 200);
    const channel = String(notice.channel || '').slice(0, 80);
    const body = String(notice.body || '').slice(0, 600);
    const text = channel ? channel + (body ? ' · ' + body : '') : body;
    // A real notification is always shown, including when a tab is focused.
    // Safari/WebKit revokes access when push is used for silent background work.
    await self.registration.showNotification(title, {
      body: text,
      icon: '/icons/icon-192.png',
      badge: '/icons/icon-192.png',
      tag: id ? 'chimeport-message-' + id : undefined,
      data: { url: id ? '/?message=' + id : '/' },
    });
  })());
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const path = event.notification.data && event.notification.data.url || '/';
  event.waitUntil((async () => {
    const target = new URL(path, self.location.origin);
    if (target.origin !== self.location.origin || target.pathname !== '/') return;
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    for (const client of windows) {
      if (new URL(client.url).origin === self.location.origin) {
        await client.navigate(target.href);
        return client.focus();
      }
    }
    return self.clients.openWindow(target.href);
  })());
});
