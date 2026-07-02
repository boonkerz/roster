// Captures README screenshots against a local demo instance.
// Usage (run from the web/ directory so playwright resolves): node ../scripts/capture.mjs <base-url> <out-dir>
import { createRequire } from "module";
// Resolve playwright from the current working directory (web/node_modules),
// not from this file's location (scripts/), which has no node_modules.
const require = createRequire(process.cwd() + "/");
const { chromium } = require("playwright");
const BASE = process.argv[2] || "http://127.0.0.1:18080";
const OUT = process.argv[3] || "docs/screenshots";

const b = await chromium.launch();
const ctx = await b.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
const p = await ctx.newPage();
const wait = (ms) => p.waitForTimeout(ms);

async function login() {
  await p.goto(BASE + "/");
  await p.waitForSelector("input");
  const i = await p.$$("input");
  await i[0].fill(process.env.DEMO_USER || "admin");
  await i[1].fill(process.env.DEMO_PASS || "demo1234");
  await p.click("button[type=submit]");
  await wait(1500);
}
async function shot(name) { await p.screenshot({ path: `${OUT}/${name}.png` }); console.log("shot", name); }
async function setLang(want) {
  for (let i = 0; i < 2; i++) {
    const btn = await p.$(".lang-switch");
    if (!btn) return;
    const cur = (await btn.innerText()).trim() === "EN" ? "de" : "en";
    if (cur === want) return;
    await btn.click(); await wait(400);
  }
}
async function openDevice() {
  await p.goto(BASE + "/devices"); await wait(1500);
  const row = await p.$("table.selectable tbody tr");
  if (row) { await row.click(); await wait(1500); }
}

await login();
for (const lang of ["de", "en"]) {
  await setLang(lang);
  await p.goto(BASE + "/dashboard"); await wait(1200); await shot(`dashboard-${lang}`);
  await openDevice(); await shot(`devices-${lang}`);
  try {
    await p.click('button.tab-group:has-text("Übersicht"),button.tab-group:has-text("Overview")'); await wait(300);
    await p.click('button.tab:has-text("Auslastung"),button.tab:has-text("Utilization")');
    // wait until the live metrics have actually loaded (bars rendered), then a
    // couple more seconds so a second sample fills the network rate.
    await p.waitForSelector(".metric-fill", { timeout: 30000 });
    await wait(3000);
    await shot(`live-${lang}`);
  } catch (e) { console.log("live capture:", e.message); }
  try {
    await p.click('button.tab-group:has-text("System")'); await wait(300);
    await p.click('button.tab:has-text("Dienste"),button.tab:has-text("Services")');
    await p.waitForSelector("table tbody tr", { timeout: 30000 });
    await wait(800);
    await shot(`services-${lang}`);
  } catch (e) { console.log("services capture:", e.message); }
}
await b.close();
console.log("done");
