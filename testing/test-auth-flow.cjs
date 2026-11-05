// Live end-to-end checks for authentication, signup approval, the admin panel,
// account-scoped streaming/downloads, and signed player links.
//
// Real backend calls: /api/auth/*, /api/admin/*, login, registration, password
// changes, deactivation, and deletion. Everything about search results, torrent
// sources, and the streamer/downloader is mocked so the suite never touches a
// tracker or the real Go service.
//
// Usage:
//   ADMIN_PASS=... node test-auth-flow.cjs
//   ADMIN_PASS=... HEADED=1 node test-auth-flow.cjs
//
// Environment:
//   TARGET_URL       App origin (default http://localhost:8090)
//   ADMIN_USER       Admin username (default admin)
//   ADMIN_PASS       Admin password (required)
//   TEST_USER        Fixed first username (default run_<random>)
//   TEST_DELETE_USER Fixed second username (default del_<random>)
//   TEST_PASS        Registration password (default Testpass123!)
//   TEST_RESET_PASS  Admin-reset password (default Resetpass789!)
//   TEST_FINAL_PASS  Self-service password (default Testpass456!)
//   SCREENSHOTS_DIR  Output directory (default ./screenshots beside this file)
//   HEADED           Set to 1 to watch the browser
//   SLOWMO           Milliseconds per Playwright action (default 40, or 150 headed)
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const BASE = (process.env.TARGET_URL || 'http://localhost:8090').replace(/\/+$/, '');
const ADMIN_USER = process.env.ADMIN_USER || 'admin';
const ADMIN_PASS = process.env.ADMIN_PASS;
const HEADED = process.env.HEADED === '1' || process.argv.includes('--headed');
const SLOWMO = Number(process.env.SLOWMO || (HEADED ? 150 : 40));

// Unique per run so repeated runs never collide on a leftover account and the
// final cleanup can identify exactly which users belong to this run.
const RUN_ID = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
const USER = process.env.TEST_USER || `run_${RUN_ID}`;
const USER_PASS = process.env.TEST_PASS || 'Testpass123!';
const USER_RESET = process.env.TEST_RESET_PASS || 'Resetpass789!';
const USER_FINAL = process.env.TEST_FINAL_PASS || 'Testpass456!';
const USER_DELETE = process.env.TEST_DELETE_USER || `del_${RUN_ID}`;
const SHOTS = path.resolve(process.env.SCREENSHOTS_DIR || path.join(__dirname, 'screenshots'));

if (!ADMIN_PASS) throw new Error('ADMIN_PASS is required');
fs.mkdirSync(SHOTS, { recursive: true });

const movie = {
  tmdb_id: 5,
  media_type: 'movie',
  title: 'Test Movie',
  year: 2024,
  poster_url: null,
  overview: null,
};
const magnet = 'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Test.Movie';
const empty = { ok: true, error: null, items: [] };
const torrentItem = {
  title: 'Test.Movie.1080p',
  magnet,
  seeders: 42,
  size: '1.2 GB',
  source: 'Torrentio',
};
const fakeSession = {
  sessionId: 'testsession',
  name: 'Test Movie',
  ready: true,
  files: [{ index: 0, path: 'Test.Movie.mkv', size: 1000000, streamable: true }],
};
const fakeTorrent = {
  hash: 'abc123def456',
  name: 'Test Download',
  files: [{ index: 0, name: 'Test.Download.mkv', size: 1000000 }],
};
const fakeEntry = {
  hash: 'abc123def456',
  name: 'Test Download',
  state: 'downloading',
  progress: 0.5,
  dlspeed: 1024,
  size: 1000000,
  downloaded: 500000,
  eta: 120,
  selectedFiles: 1,
  totalFiles: 1,
};
const fakeDetail = {
  ...fakeEntry,
  files: [
    { index: 0, name: 'Test.Download.mkv', size: 1000000, downloaded: 500000, selected: true },
  ],
};

function log(step) {
  console.log(`\n=== ${step} ===`);
}

// Removes this run's accounts through the real admin API, even when an
// assertion failed earlier. Uses the browser context's request client so the
// admin session cookie and CSRF flow are exercised exactly like the UI's.
async function cleanup(context) {
  if (!context) return true;
  try {
    const login = await context.request.post(`${BASE}/api/auth/login`, {
      data: { username: ADMIN_USER, password: ADMIN_PASS },
    });
    if (!login.ok()) throw new Error(`admin login failed (${login.status()})`);
    const { csrf_token: csrf } = await login.json();

    const list = await context.request.get(`${BASE}/api/admin/users`);
    if (!list.ok()) throw new Error(`user list failed (${list.status()})`);
    const { users } = await list.json();

    const targets = users.filter((u) => u.username === USER || u.username === USER_DELETE);
    for (const user of targets) {
      const del = await context.request.delete(`${BASE}/api/admin/users/${user.id}`, {
        headers: { 'X-CSRF-Token': csrf },
      });
      if (!del.ok()) throw new Error(`delete ${user.username} failed (${del.status()})`);
      console.log(`cleanup: deleted ${user.username}`);
    }
    console.log(`cleanup: ${targets.length} test account(s) removed`);
    return true;
  } catch (err) {
    console.error(`cleanup: could not remove test accounts: ${err.message}`);
    return false;
  }
}

async function main() {
  console.log(`target: ${BASE}`);
  console.log(`users:  ${USER}, ${USER_DELETE}`);
  console.log(`shots:  ${SHOTS}`);

  const browser = await chromium.launch({ headless: !HEADED, slowMo: SLOWMO });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
    baseURL: BASE,
  });
  let cleanupOk = true;
  try {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: BASE });
    // Keep all torrent sources server-side so the mocked /api/* routes are used
    // instead of the browser hitting Torrentio/Comet/Meteor directly.
    await context.addInitScript(() => {
      localStorage.setItem('torrentioMode', 'server');
      localStorage.setItem('cometMode', 'server');
      localStorage.setItem('meteorMode', 'server');
    });
    const page = await context.newPage();
    page.setDefaultTimeout(20000);
    page.on('pageerror', (err) => console.error('PAGE ERROR:', err.message));
    page.on('console', (msg) => {
      if (msg.type() === 'error') console.error('CONSOLE ERROR:', msg.text());
    });

    // Mock only source/search/download data; auth endpoints hit the real backend.
    await page.route('**/api/search**', (r) => r.fulfill({ json: { results: [movie] } }));
    await page.route('**/api/external-ids**', (r) => r.fulfill({ json: { imdb_id: 'tt123' } }));
    await page.route('**/api/torrentio**', (r) =>
      r.fulfill({ json: { ok: true, error: null, items: [torrentItem] } }),
    );
    await page.route('**/api/comet**', (r) => r.fulfill({ json: empty }));
    await page.route('**/api/meteor**', (r) => r.fulfill({ json: empty }));
    await page.route('**/api/forum/search**', (r) => r.fulfill({ json: empty }));
    await page.route('**/api/1337x/search**', (r) => r.fulfill({ json: empty }));
    await page.route('**/download-api/status', (r) => r.fulfill({ json: { enabled: true } }));
    await page.route('**/download-api/disk', (r) =>
      r.fulfill({ json: { totalBytes: 1000000000, freeBytes: 500000000, usedBytes: 500000000 } }),
    );
    await page.route('**/stream-api/torrents', (r) => r.fulfill({ json: [] }));
    await page.route('**/stream-api/sessions', (r) => r.fulfill({ json: fakeSession }));
    await page.route('**/api/auth/stream-token', (r) =>
      r.fulfill({ json: { token: 'signed-test-token', expires_at: '2099-01-01T00:00:00+00:00' } }),
    );
    await page.route('**/download-api/torrents', (r) => {
      if (r.request().method() === 'GET') return r.fulfill({ json: [fakeEntry] });
      return r.fulfill({ json: fakeTorrent });
    });
    await page.route('**/download-api/torrents/abc123def456**', (r) =>
      r.fulfill({ json: fakeDetail }),
    );

    async function shot(name) {
      // Let framer-motion entrance/exit animations settle before capturing.
      await page.waitForTimeout(400);
      await page.screenshot({ path: path.join(SHOTS, `${name}.png`), fullPage: true });
      console.log(`screenshot: ${path.join(SHOTS, `${name}.png`)}`);
    }

    async function openSearchResult() {
      await page.getByLabel('Search query').fill('Test Movie');
      await page.getByRole('button', { name: 'Search', exact: true }).click();
      const titleCard = page.locator('.title-card', { hasText: 'Test Movie' }).first();
      await titleCard.waitFor();
      await titleCard.click();
      const torrentioTab = page.getByRole('tab', { name: /torrentio/i });
      await torrentioTab.waitFor();
      await torrentioTab.click();
      await page.locator('.result-row').first().waitFor();
    }

    async function closeModal() {
      const close = page.locator('.modal [aria-label="Close"]');
      if (await close.count()) await close.first().click();
    }

    async function openLogin() {
      const headerLogin = page.locator('.auth-controls').getByRole('button', {
        name: 'Log in',
        exact: true,
      });
      if (await headerLogin.count()) {
        await headerLogin.click();
        return;
      }
      await page
        .locator('.admin-gate')
        .getByRole('button', { name: 'Log in', exact: true })
        .click();
    }

    async function login(username, password) {
      await openLogin();
      const dialog = page.locator('.auth-modal');
      await dialog.waitFor();
      await dialog.locator('input[autocomplete="username"]').fill(username);
      await dialog.locator('input[autocomplete="current-password"]').fill(password);
      await dialog.locator('button[type="submit"]').click();
      // Wait for the request to finish: success closes the modal, failure shows
      // the role=alert error banner. Without this, a following page.goto() can
      // abort the in-flight login POST.
      await page.waitForFunction(
        () => {
          const modal = document.querySelector('.auth-modal');
          return !modal || !!modal.querySelector('[role="alert"]');
        },
        undefined,
        { timeout: 20000 },
      );
    }

    async function logout() {
      await page.locator('.auth-user-btn').click();
      await page.getByRole('menuitem', { name: /Log out/ }).click();
      await page.locator('.auth-user-btn').waitFor({ state: 'detached' });
    }

    await page.goto(BASE, { waitUntil: 'networkidle' });
    log('Anonymous load');
    await page.getByRole('button', { name: 'Log in', exact: true }).waitFor();
    await page.getByRole('button', { name: 'Sign up', exact: true }).waitFor();
    await shot('01-anonymous-home');

    log('Mobile hero clears the fixed header');
    const mobile = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const mobilePage = await mobile.newPage();
    await mobilePage.goto(BASE, { waitUntil: 'networkidle' });
    const badgeBox = await mobilePage.locator('.eyebrow').boundingBox();
    const headerBox = await mobilePage.locator('header.app-header').boundingBox();
    assert.ok(badgeBox && headerBox, 'hero badge and header must render');
    assert.ok(
      badgeBox.y >= headerBox.y + headerBox.height,
      `hero badge (y=${badgeBox.y}) overlaps the header (bottom=${headerBox.y + headerBox.height})`,
    );
    await mobilePage.screenshot({
      path: path.join(SHOTS, '01b-mobile-home.png'),
      fullPage: true,
    });
    console.log(`screenshot: ${path.join(SHOTS, '01b-mobile-home.png')}`);
    await mobile.close();

    log('Anonymous search + gating');
    await openSearchResult();
    const row = page.locator('.result-row').first();
    assert.equal(await row.getByRole('button', { name: 'Stream', exact: true }).count(), 1);
    assert.equal(await row.getByRole('button', { name: 'Download', exact: true }).count(), 1);
    await row.getByRole('button', { name: 'Stream', exact: true }).click();
    let dialog = page.locator('.auth-modal');
    await dialog.waitFor();
    assert.match(await dialog.innerText(), /Log in to stream or download/);
    await shot('02-stream-login-gate');
    await closeModal();

    await row.getByRole('button', { name: 'Download', exact: true }).click();
    dialog = page.locator('.auth-modal');
    await dialog.waitFor();
    assert.match(await dialog.innerText(), /Log in to stream or download/);
    await closeModal();

    await row.locator('.action-copy').click();
    await page.waitForTimeout(300);
    const copiedMagnet = await page.evaluate(() => navigator.clipboard.readText());
    assert.equal(copiedMagnet, magnet);
    console.log('copy magnet works while anonymous');

    log('Registration + pending approval');
    await page.getByRole('button', { name: 'Sign up', exact: true }).click();
    dialog = page.locator('.auth-modal');
    await dialog.waitFor();
    await dialog.locator('input[autocomplete="username"]').fill(USER);
    await dialog.locator('input[autocomplete="new-password"]').fill(USER_PASS);
    await dialog.locator('button[type="submit"]').click();
    await dialog.locator('.auth-notice').waitFor();
    assert.match(await dialog.innerText(), /admin must approve/i);

    // Register a second account used for the delete path.
    await dialog.getByRole('tab', { name: 'Register' }).click();
    await dialog.locator('input[autocomplete="username"]').fill(USER_DELETE);
    await dialog.locator('input[autocomplete="new-password"]').fill(USER_PASS);
    await dialog.locator('button[type="submit"]').click();
    await dialog.locator('.auth-notice').waitFor();
    await shot('03-registered-pending');

    await dialog.locator('input[autocomplete="username"]').fill(USER);
    await dialog.locator('input[autocomplete="current-password"]').fill(USER_PASS);
    await dialog.locator('button[type="submit"]').click();
    const pendingAlert = dialog.getByRole('alert');
    await pendingAlert.waitFor();
    assert.match(await pendingAlert.innerText(), /awaiting admin approval/i);
    await shot('04-pending-login-message');
    await closeModal();

    log('Admin approval, reset, deactivate/reactivate, delete');
    await login(ADMIN_USER, ADMIN_PASS);
    await page.locator('.auth-user-btn').filter({ hasText: ADMIN_USER }).waitFor();
    await page.goto(`${BASE}/admin`, { waitUntil: 'networkidle' });
    await page.getByRole('heading', { name: 'User management' }).waitFor();
    await page.getByText(USER, { exact: true }).first().waitFor();
    await shot('05-admin-pending');

    // Capture the open account menu (all three items) to check item alignment.
    await page.locator('.auth-user-btn').click();
    await page.getByRole('menuitem', { name: /Admin panel/ }).waitFor();
    await shot('05a-account-menu');
    await page.keyboard.press('Escape');

    await page
      .locator('.admin-user-card', { hasText: USER })
      .getByRole('button', { name: /Approve/ })
      .click();
    await page.getByText(`${USER} approved.`).waitFor();
    await page.locator('.admin-user-card', { hasText: USER }).waitFor({ state: 'detached' });

    let userRow = page.locator('.admin-table tbody tr', { hasText: USER });
    await userRow.getByRole('button', { name: /Reset password/ }).click();
    let resetDialog = page.locator('.modal', { hasText: `Reset password for ${USER}` });
    await resetDialog.waitFor();
    await resetDialog.locator('input[type="password"]').fill(USER_RESET);
    await shot('05b-admin-reset-dialog');
    await resetDialog.getByRole('button', { name: 'Reset password', exact: true }).click();
    await page.getByText(`Password reset for ${USER}.`).waitFor();

    userRow = page.locator('.admin-table tbody tr', { hasText: USER });
    await userRow.getByRole('button', { name: 'Deactivate', exact: true }).click();
    await page.getByText(`${USER} deactivated.`).waitFor();
    await page.locator('.admin-table tbody tr', { hasText: USER }).getByText('disabled').waitFor();
    await page
      .locator('.admin-table tbody tr', { hasText: USER })
      .getByRole('button', { name: /Reactivate/ })
      .click();
    await page.getByText(`${USER} reactivated.`).waitFor();

    let deleteRow = page.locator('.admin-table tbody tr', { hasText: USER_DELETE });
    await deleteRow.getByRole('button', { name: 'Delete', exact: true }).click();
    await deleteRow.getByRole('button', { name: /Confirm delete/ }).click();
    await page.getByText(`${USER_DELETE} deleted.`).waitFor();
    await page
      .locator('.admin-table tbody tr', { hasText: USER_DELETE })
      .waitFor({ state: 'detached' });
    await shot('06-admin-after-actions');

    await logout();

    log('User login with admin-reset password');
    await login(USER, USER_RESET);
    // Wait for the login request to finish (the modal closes on success) before
    // navigating, otherwise the goto aborts the in-flight POST.
    await page.locator('.auth-modal').waitFor({ state: 'detached' });
    // A normal user sees "Not authorized" at /admin, so go back to the app
    // before asserting on the account menu.
    await page.goto(BASE, { waitUntil: 'networkidle' });
    await page.locator('.auth-user-btn').filter({ hasText: USER }).waitFor();
    await shot('07-user-logged-in');

    log('Signed stream URL UI');
    await openSearchResult();
    const userRow2 = page.locator('.result-row').first();
    await userRow2.getByRole('button', { name: 'Stream', exact: true }).click();
    await userRow2.getByRole('button', { name: 'Show links', exact: true }).waitFor();
    await userRow2.getByRole('button', { name: 'Show links', exact: true }).click();
    await page.locator('.stream-panel a[href*="token=signed-test-token"]').first().waitFor();
    const streamHref = await page.locator('.stream-panel a').first().getAttribute('href');
    assert.match(streamHref, /token=signed-test-token/);
    await userRow2.getByRole('button', { name: /Copy stream URL/ }).click();
    await page.waitForTimeout(300);
    const copiedStream = await page.evaluate(() => navigator.clipboard.readText());
    assert.match(copiedStream, /token=signed-test-token/);
    console.log('signed stream URLs present');
    await shot('08-signed-stream-links');

    log('Signed download URL UI');
    await userRow2.getByRole('button', { name: 'Download', exact: true }).click();
    await page.getByText('Test.Download.mkv').waitFor();
    await page.getByRole('button', { name: 'Downloads', exact: true }).click();
    const downloadsModal = page.locator('.stats-modal');
    await downloadsModal.waitFor();
    await downloadsModal.getByText('Test Download', { exact: true }).waitFor();
    await downloadsModal.getByRole('button', { name: 'Show files', exact: true }).click();
    await downloadsModal.getByText('Test.Download.mkv', { exact: true }).waitFor();
    await downloadsModal.locator('a[href*="token=signed-test-token"]').first().waitFor();
    const downloadAnchor = downloadsModal.locator('a[href*="dl=1"]').first();
    const dlHref = await downloadAnchor.getAttribute('href');
    assert.match(dlHref, /token=signed-test-token/);
    assert.match(dlHref, /dl=1/);
    // The "Copy link" label is visually hidden on icon-only buttons, so locate
    // the direct <button> beside the download <a> instead of by accessible name.
    await downloadsModal.locator('.player-links > button.player-btn.icon-only').first().click();
    await page.waitForTimeout(300);
    const copiedDownload = await page.evaluate(() => navigator.clipboard.readText());
    assert.match(copiedDownload, /token=signed-test-token/);
    console.log('signed download URLs present');
    await shot('09-signed-download-links');
    await downloadsModal.locator('[aria-label="Close"]').click();

    log('Self-service password change');
    await page.locator('.auth-user-btn').click();
    await page.getByRole('menuitem', { name: /Change password/ }).click();
    const pwDialog = page.locator('.auth-modal');
    await pwDialog.waitFor();
    await pwDialog.locator('input[autocomplete="current-password"]').fill(USER_RESET);
    await pwDialog.locator('input[autocomplete="new-password"]').fill(USER_FINAL);
    await pwDialog.locator('button[type="submit"]').click();
    await pwDialog.locator('.auth-notice').waitFor();
    assert.match(await pwDialog.innerText(), /Password changed/i);
    await shot('10-password-changed');
    await closeModal();

    log('Deactivation blocks login, reactivation restores it');
    await logout();
    await login(ADMIN_USER, ADMIN_PASS);
    await page.goto(`${BASE}/admin`, { waitUntil: 'networkidle' });
    await page.getByRole('heading', { name: 'User management' }).waitFor();
    userRow = page.locator('.admin-table tbody tr', { hasText: USER });
    await userRow.getByRole('button', { name: 'Deactivate', exact: true }).click();
    await page.getByText(`${USER} deactivated.`).waitFor();
    await logout();
    await page.goto(BASE, { waitUntil: 'networkidle' });
    await login(USER, USER_FINAL);
    const disabledAlert = page.locator('.auth-modal').getByRole('alert');
    await disabledAlert.waitFor();
    assert.match(await disabledAlert.innerText(), /deactivated/i);
    await shot('11-deactivated-login-message');
    await closeModal();
    await login(ADMIN_USER, ADMIN_PASS);
    await page.goto(`${BASE}/admin`, { waitUntil: 'networkidle' });
    await page.getByRole('heading', { name: 'User management' }).waitFor();
    userRow = page.locator('.admin-table tbody tr', { hasText: USER });
    await userRow.getByRole('button', { name: /Reactivate/ }).click();
    await page.getByText(`${USER} reactivated.`).waitFor();

    console.log('\nALL LIVE CHECKS PASSED');
  } finally {
    cleanupOk = await cleanup(context);
    await browser.close();
    if (!cleanupOk && !process.exitCode) process.exitCode = 1;
  }
}

main().catch((err) => {
  console.error('\nLIVE CHECK FAILED:', err);
  process.exitCode = 1;
});
