/* OMA overlay for OpenViking Studio (injected into index.html before the app
 * bundle loads).
 *
 * Studio asserts the identity stored in localStorage["ov_console_connection"]
 * (accountId / userId) via X-OpenViking-Account / X-OpenViking-User headers.
 * All tenant data lives under viking://user/<tenant>/, so the Studio tree only
 * shows it when the asserted user matches the tenant.
 *
 * This snippet lets a deep link switch the asserted identity, e.g.:
 *   /studio/playground?ov_user=tn_xxx&ov_account=default&uri=viking://user/memories/
 *
 * Supported query params (all optional):
 *   ov_user      -> connection.userId    (X-OpenViking-User)
 *   ov_account   -> connection.accountId (X-OpenViking-Account)
 *   ov_api_key   -> connection.apiKey    (X-API-Key)
 */
(function () {
  try {
    var params = new URLSearchParams(window.location.search);
    var user = params.get('ov_user');
    var account = params.get('ov_account');
    var apiKey = params.get('ov_api_key');
    if (!user && !account && !apiKey) return;

    var KEY = 'ov_console_connection';
    var conn = {};
    try {
      var raw = window.localStorage.getItem(KEY);
      if (raw) conn = JSON.parse(raw);
    } catch (e) {
      conn = {};
    }
    if (!conn || typeof conn !== 'object') conn = {};

    // A deep link always targets the server serving this page.
    conn.baseUrl = window.location.origin;
    if (account) conn.accountId = account;
    if (user) conn.userId = user;
    if (apiKey) conn.apiKey = apiKey;
    if (!conn.accountId) conn.accountId = 'default';
    if (!conn.userId) conn.userId = 'default';

    window.localStorage.setItem(KEY, JSON.stringify(conn));
    console.info(
      '[oma-studio] identity asserted: account=' + conn.accountId + ' user=' + conn.userId
    );

    // Strip the identity params so a later manual identity switch in the UI is
    // not overwritten on reload; keep other playground params intact.
    var kept = new URLSearchParams(window.location.search);
    ['ov_user', 'ov_account', 'ov_api_key'].forEach(function (k) {
      kept.delete(k);
    });
    var qs = kept.toString();
    var clean = window.location.pathname + (qs ? '?' + qs : '') + window.location.hash;
    window.history.replaceState(null, '', clean);
  } catch (err) {
    console.warn('[oma-studio] identity injection failed:', err);
  }
})();
