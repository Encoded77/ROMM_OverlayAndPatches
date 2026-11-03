// Browser check of the overlay on a throwaway RomM: creates the first admin,
// logs in, and asserts that every extension's navbar entry is rendered and
// that its page opens without a page error. An extension that needs a service
// outside RomM starts off: the check asserts its entry is absent, switches it
// on in Administration > Extensions, and then checks it like the others.
//
//   ROMM_URL=http://host:8081 [SCREENSHOTS=dir] node checks/nav-check.mjs
//
// Needs playwright-core and a Chromium (PLAYWRIGHT_CORE points at the module).
import { createRequire } from "module";

const pwc = process.env.PLAYWRIGHT_CORE || "playwright-core";
const { chromium } = createRequire(import.meta.url)(pwc);
const base = process.env.ROMM_URL || "http://127.0.0.1:8081";
const user = "overlay-check";
const pass = "overlay-check-password";

// [label shown in the navbar (en_US), path it opens]
// The throwaway RomM holds no games, so Random multiplayer stays on its page.
const expected = [["Random multiplayer", "/random-multi"]];
// [navbar label, path, extension id, its switch in Administration > Extensions]
const switchedOff = [["Requests", "/requests", "requests", "Game requests"]];
const shots = process.env.SCREENSHOTS;

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
  const shot = async (name) => shots && page.screenshot({ path: `${shots}/${name}.png` });
  const opens = async (label, path) => {
    const link = page.getByRole("link", { name: label }).first();
    await link.waitFor({ timeout: 15000 });
    await link.click();
    await page.waitForURL(`**${path}`, { timeout: 15000 });
    await page.getByRole("heading", { name: label }).waitFor({ timeout: 15000 });
    console.log(`ok: "${label}" in the navbar opens ${path}`);
  };
  await page.goto(`${base}/`, { waitUntil: "networkidle" });

  for (const [label, path] of expected) await opens(label, path);
  for (const [label] of switchedOff) {
    if (await page.getByRole("link", { name: label }).count()) {
      throw new Error(`"${label}" is in the navbar before an admin switched it on`);
    }
    console.log(`ok: "${label}" starts off`);
  }

  await page.goto(`${base}/administration?tab=extensions`, { waitUntil: "networkidle" });
  for (const [, , id, title] of switchedOff) {
    const toggle = page.getByRole("switch", { name: title });
    await toggle.waitFor({ timeout: 15000 });
    if ((await toggle.getAttribute("aria-checked")) !== "false") {
      throw new Error(`the "${title}" switch is not off on a fresh config`);
    }
    await shot("admin-extensions");
    const saved = page.waitForResponse(
      (r) => r.url().endsWith(`/api/config/overlay/extensions/${id}`) && r.request().method() === "PUT",
    );
    await toggle.click();
    const response = await saved;
    if (!response.ok()) throw new Error(`saving "${title}": HTTP ${response.status()}`);
    await page.waitForFunction(
      (name) => document.querySelector(`[role=switch][aria-label="${name}"]`)?.getAttribute("aria-checked") === "true",
      title,
    );
    await page.getByRole("button", { name: "Reload" }).waitFor({ timeout: 15000 });
    await shot("admin-extensions-reload");
    console.log(`ok: "${title}" switched on in Administration > Extensions`);
  }
  const config = await (await context.request.get(`${base}/api/config`)).json();
  for (const [, , id] of switchedOff) {
    if (config.OVERLAY_EXTENSIONS?.[id] !== true) {
      throw new Error(`config.yml does not hold overlay.extensions.${id}: true`);
    }
  }

  await page.goto(`${base}/`, { waitUntil: "networkidle" });
  for (const [label, path] of switchedOff) await opens(label, path);
  if (errors.length) throw new Error(`page errors:\n${errors.join("\n")}`);
  console.log("nav-check passed");
} catch (error) {
  console.error(`nav-check FAILED: ${error.message}`);
  process.exitCode = 1;
} finally {
  await browser.close();
}
