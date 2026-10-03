// Browser check of the overlay on a throwaway RomM: creates the first admin,
// logs in, and asserts that every extension's navbar entry is rendered and
// that its page opens without a page error.
//
//   ROMM_URL=http://host:8081 node checks/nav-check.mjs
//
// Needs playwright-core and a Chromium (PLAYWRIGHT_CORE points at the module).
import { createRequire } from "module";

const pwc = process.env.PLAYWRIGHT_CORE || "playwright-core";
const { chromium } = createRequire(import.meta.url)(pwc);
const base = process.env.ROMM_URL || "http://127.0.0.1:8081";
const user = "overlay-check";
const pass = "overlay-check-password";

// [label shown in the navbar (en_US), path it opens]
const expected = [["Requests", "/requests"]];

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1400, height: 900 } });
const errors = [];

try {
  // RomM's CSRF guard: the cookie comes with any GET, the header must echo it.
  await context.request.get(`${base}/api/heartbeat`);
  const csrf = async () =>
    (await context.cookies(base)).find((c) => c.name === "romm_csrftoken")?.value ?? "";

  const created = await context.request.post(`${base}/api/users`, {
    headers: { "x-csrftoken": await csrf() },
    data: { username: user, password: pass, email: "check@example.com", role: "admin" },
  });
  if (![200, 201, 400, 409].includes(created.status())) {
    throw new Error(`create admin: HTTP ${created.status()} ${await created.text()}`);
  }
  const login = await context.request.post(`${base}/api/login`, {
    headers: { "x-csrftoken": await csrf(), Authorization: "Basic " + Buffer.from(`${user}:${pass}`).toString("base64") },
  });
  if (login.status() !== 200) throw new Error(`login: HTTP ${login.status()}`);

  const page = await context.newPage();
  page.on("pageerror", (error) => errors.push(String(error)));
  await page.goto(`${base}/`, { waitUntil: "networkidle" });

  for (const [label, path] of expected) {
    const link = page.getByRole("link", { name: label }).first();
    await link.waitFor({ timeout: 15000 });
    await link.click();
    await page.waitForURL(`**${path}`, { timeout: 15000 });
    await page.getByRole("heading", { name: label }).waitFor({ timeout: 15000 });
    console.log(`ok: "${label}" in the navbar opens ${path}`);
  }
  if (errors.length) throw new Error(`page errors:\n${errors.join("\n")}`);
  console.log("nav-check passed");
} catch (error) {
  console.error(`nav-check FAILED: ${error.message}`);
  process.exitCode = 1;
} finally {
  await browser.close();
}
