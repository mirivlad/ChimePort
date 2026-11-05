'use strict';

(() => {
  const el = (id) => document.getElementById(id);
  const supported = window.isSecureContext && 'serviceWorker' in navigator &&
    'PushManager' in window && 'Notification' in window;
  let registrationPromise;
  let pendingInstall = null;
  let signedIn = false;
  let busy = false;

  const setText = (s) => { if (el('push-status')) el('push-status').textContent = s; };
  const report = (err) => {
    setText('Ошибка: ' + (err && err.message || String(err)));
  };
  const apiPush = async (path, method = 'GET', payload) => {
    const res = await fetch('/api/v1/webpush/' + path, {
      credentials: 'same-origin', method,
      headers: payload ? { 'Content-Type': 'application/json' } : {},
      body: payload ? JSON.stringify(payload) : undefined
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error?.message || 'HTTP ' + res.status);
    return data;
  };
  const payloadOf = (subscription) => {
    const obj = subscription.toJSON();
    return { endpoint: obj.endpoint, keys: { auth: obj.keys.auth, p256dh: obj.keys.p256dh } };
  };
  const keyBytes = (encoded) => {
    const normalized = encoded.replace(/-/g, '+').replace(/_/g, '/');
    const raw = atob(normalized.padEnd(Math.ceil(normalized.length / 4) * 4, '='));
    return Uint8Array.from(raw, (letter) => letter.charCodeAt(0));
  };
  const getRegistration = () => {
    if (!registrationPromise) registrationPromise = navigator.serviceWorker.register('/sw.js', { scope: '/' })
      .then(() => navigator.serviceWorker.ready);
    return registrationPromise;
  };
  const toggleBusy = (value) => {
    busy = value;
    if (el('push-enable')) el('push-enable').disabled = value || !signedIn || !supported || Notification.permission === 'denied';
    if (el('push-disable')) el('push-disable').disabled = value || !signedIn || !supported;
  };

  async function syncPushStatus() {
    if (!signedIn || !supported) return;
    try {
      const registration = await getRegistration();
      const existing = await registration.pushManager.getSubscription();
      if (existing && Notification.permission === 'granted') {
        // Refresh binding to this login session (previous sessions may have expired).
        await apiPush('subscriptions', 'POST', payloadOf(existing));
        const info = await apiPush('subscriptions');
        setText('Web Push включён на этом устройстве. Активных подписок аккаунта: ' + info.count + '.');
        el('push-enable').hidden = true;
        el('push-disable').hidden = false;
      } else {
        setText(Notification.permission === 'denied'
          ? 'Браузер запретил уведомления. Измените разрешение в настройках сайта.'
          : 'Web Push выключен на этом устройстве. Уведомления можно включить вручную.');
        el('push-enable').hidden = false;
        el('push-disable').hidden = true;
      }
    } catch (error) {
      report(error);
    }
    toggleBusy(false);
  }

  async function enablePush() {
    if (!signedIn || !supported || busy) return;
    toggleBusy(true);
    try {
      // Request permission immediately in the click handler; Safari/iOS requires
      // a real user gesture, not a promise continuation after fetch().
      const permissionPromise = Notification.requestPermission();
      const permission = await permissionPromise;
      if (permission !== 'granted') {
        setText('Доступ к уведомлениям не предоставлен.');
        return;
      }
      const registration = await getRegistration();
      const key = (await apiPush('vapid-public-key')).public_key;
      let subscription = await registration.pushManager.getSubscription();
      if (!subscription) subscription = await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: keyBytes(key)
      });
      await apiPush('subscriptions', 'POST', payloadOf(subscription));
      setText('Web Push включён. Новые сообщения будут приходить даже при закрытой вкладке, если ОС разрешает.');
      el('push-enable').hidden = true;
      el('push-disable').hidden = false;
    } catch (error) { report(error); }
    finally { toggleBusy(false); }
  }

  async function disablePush() {
    if (!signedIn || !supported || busy) return;
    toggleBusy(true);
    try {
      const registration = await getRegistration();
      const subscription = await registration.pushManager.getSubscription();
      if (subscription) {
        await apiPush('subscriptions', 'DELETE', { endpoint: subscription.endpoint });
        await subscription.unsubscribe();
      }
      setText('Web Push отключён на этом устройстве.');
      el('push-enable').hidden = false;
      el('push-disable').hidden = true;
    } catch (error) { report(error); }
    finally { toggleBusy(false); }
  }

  function updateInstall() {
    if (el('pwa-install')) el('pwa-install').hidden = !pendingInstall;
  }
  window.addEventListener('beforeinstallprompt', (event) => {
    event.preventDefault();
    pendingInstall = event;
    updateInstall();
  });
  window.addEventListener('appinstalled', () => { pendingInstall = null; updateInstall(); });

  document.addEventListener('DOMContentLoaded', () => {
    if (supported) {
      getRegistration().catch(report);
    } else {
      setText('Web Push недоступен: требуется HTTPS, Service Worker и поддержка Push API. На iOS добавьте ChimePort на домашний экран.');
      el('push-enable').disabled = true;
      el('push-disable').hidden = true;
    }
    el('push-enable').addEventListener('click', enablePush);
    el('push-disable').addEventListener('click', disablePush);
    el('pwa-install').addEventListener('click', async () => {
      if (!pendingInstall) return;
      const prompt = pendingInstall;
      pendingInstall = null;
      updateInstall();
      await prompt.prompt();
    });
    signedIn = !el('app-panel').hidden;
    if (signedIn) syncPushStatus();
    else toggleBusy(false);
  });
  window.addEventListener('chimeport:auth', (event) => {
    signedIn = Boolean(event.detail && event.detail.signedIn);
    if (signedIn) syncPushStatus();
    else {
      if (el('push-status')) setText('Для управления подписками войдите в аккаунт.');
      toggleBusy(false);
    }
  });
})();
