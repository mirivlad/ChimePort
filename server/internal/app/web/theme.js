/* Browser-local theme selection, shared by the console and API examples. */
(() => {
  'use strict';
  const storageKey = 'chimeport.web.theme';
  const knownThemes = new Set(['ocean', 'paper', 'control']);
  const normalize = (value) => knownThemes.has(value) ? value : 'ocean';
  const readPreference = () => {
    try { return window.localStorage.getItem(storageKey); }
    catch (_) { return null; }
  };
  const savePreference = (value) => {
    try { window.localStorage.setItem(storageKey, value); }
    catch (_) { /* private mode or disabled storage: still switch this page */ }
  };
  function applyTheme(value, persist = false) {
    const theme = normalize(value);
    document.documentElement.dataset.theme = theme;
    const picker = document.getElementById('theme-select');
    if (picker) picker.value = theme;
    if (persist) savePreference(theme);
  }

  // Apply before the main stylesheet is parsed to prevent a light-theme flash.
  applyTheme(readPreference());
  document.addEventListener('DOMContentLoaded', () => {
    const picker = document.getElementById('theme-select');
    if (picker) {
      picker.value = document.documentElement.dataset.theme;
      picker.addEventListener('change', () => applyTheme(picker.value, true));
    }
  });
  window.addEventListener('storage', (event) => {
    if (event.key === storageKey) applyTheme(event.newValue);
  });
})();
