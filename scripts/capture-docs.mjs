// Captures the documentation screenshots (English) against a local demo instance,
// seeding a bit of demo data (companies, sites, a custom role, users) so the
// roles/scope views have content. Run from web/: node ../scripts/capture-docs.mjs <base-url> <out-dir>
import { createRequire } from "module";
const require = createRequire(process.cwd() + "/");
const { chromium } = require("playwright");
const BASE = process.argv[2] || "http://127.0.0.1:18080";
const OUT = process.argv[3] || "docs/screenshots";

const b = await chromium.launch();
const ctx = await b.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
const p = await ctx.newPage();
const wait = (ms) => p.waitForTimeout(ms);
const shot = async (name) => { await p.screenshot({ path: `${OUT}/${name}.png` }); console.log("shot", name); };

async function login() {
  await p.goto(BASE + "/");
  await p.waitForSelector("input");
  const i = await p.$$("input");
  await i[0].fill("admin");
  await i[1].fill("demo1234");
  await p.click("button[type=submit]");
  await wait(1800);
}

// Sprache auf Englisch stellen (LangSwitch zeigt die jeweils andere Sprache als Label).
async function setEN() {
  for (let k = 0; k < 2; k++) {
    const btn = await p.$(".lang-switch");
    if (!btn) return;
    if ((await btn.innerText()).trim() === "EN") { await btn.click(); await wait(500); } else return;
  }
}

// Demo-Daten anlegen (über die authentifizierte Session, gleiche Origin).
async function seed() {
  return await p.evaluate(async () => {
    const api = (m, u, body) => fetch("/api/v1" + u, {
      method: m, headers: { "Content-Type": "application/json" },
      body: body ? JSON.stringify(body) : undefined, credentials: "same-origin",
    }).then((r) => r.json());
    const devs = await api("GET", "/devices");
    const devId = devs[0] && devs[0].id;
    const acme = await api("POST", "/clients", { name: "ACME Corp" });
    const berlin = await api("POST", "/sites", { client_id: acme.id, name: "Berlin HQ" });
    await api("POST", "/sites", { client_id: acme.id, name: "Munich Office" });
    const globex = await api("POST", "/clients", { name: "Globex Industries" });
    await api("POST", "/sites", { client_id: globex.id, name: "Head Office" });
    if (devId) await api("PUT", "/devices/" + devId + "/site", { site_id: berlin.id });
    const support = await api("POST", "/roles", { name: "Support", permissions: ["page.dashboard", "page.devices", "devices.operate"] });
    await api("POST", "/roles", { name: "Auditor (read-only)", permissions: ["page.dashboard", "page.devices"] });
    await api("POST", "/users", { username: "support1", password: "Passw0rd!x", role: "technician" });
    await api("POST", "/users", { username: "acme-viewer", password: "Passw0rd!x", role: "viewer", custom_role_id: support.id });
    return { devId };
  });
}

async function openDevice() {
  await p.goto(BASE + "/devices"); await wait(1500);
  const row = await p.$("table.selectable tbody tr");
  if (row) { await row.click(); await wait(1500); }
}

async function openSettings(areaLabel) {
  const dd = await p.$(".nav-dropdown button");
  if (!dd) return false;
  await dd.click(); await wait(300);
  const item = await p.$(`.nav-dropdown-item:has-text("${areaLabel}")`);
  if (!item) return false;
  await item.click(); await wait(1200);
  return true;
}

await login();
await setEN();
await seed();
await wait(500);

await p.goto(BASE + "/dashboard"); await wait(1500); await shot("dashboard");
await p.goto(BASE + "/devices"); await wait(1500); await shot("devices");

await openDevice(); await shot("device-detail");
try {
  await p.click('button.tab-group:has-text("Overview")'); await wait(300);
  await p.click('button.tab:has-text("Utilization")');
  await p.waitForSelector(".metric-fill", { timeout: 30000 }); await wait(3000);
  await shot("live-utilization");
} catch (e) { console.log("live:", e.message); }
try {
  await p.click('button.tab-group:has-text("Access")'); await wait(300);
  await p.click('button.tab:has-text("Remote control")'); await wait(1200);
  await shot("remote-control");
} catch (e) { console.log("remote:", e.message); }

try { await p.goto(BASE + "/policies"); await wait(1200); await shot("policies"); } catch (e) { console.log("policies:", e.message); }

try {
  if (await openSettings("Users & roles")) {
    await shot("roles");
    const sc = await p.$('button:has-text("Scope")');
    if (sc) {
      await sc.click(); await wait(800);
      await p.evaluate(() => document.querySelector(".scope-editor")?.scrollIntoView({ block: "center" }));
      await wait(400);
      await shot("data-scope");
    }
  }
} catch (e) { console.log("settings:", e.message); }

await b.close();
console.log("done");
