// TodayZ mocks UI. Plain DOM, textContent only (captured data is untrusted).
"use strict";

const POLL_MS = 2000;

const KINDS = {
  sms: {
    title: "SMS",
    desc: "MSG91 OTP and flow messages, rendered from the template.",
    cols: ["Time", "To", "OTP", "Template", "Message"],
    row: (m) => [
      time(m.at),
      phone(m.to),
      otp(m.otp),
      stack(m.template_id || "default", m.api),
      wrap(m.body),
    ],
  },
  whatsapp: {
    title: "WhatsApp",
    desc: "MSG91 and Gupshup WhatsApp sends.",
    cols: ["Time", "To", "Template", "Message", "Provider"],
    row: (m) => [
      time(m.at),
      phone(m.to),
      stack(m.template || m.kind, m.language),
      wrap(m.body),
      text(m.provider),
    ],
  },
  push: {
    title: "Push",
    desc: "Web Push deliveries. Title and body come from the X-TZ-Mock-Plaintext header.",
    cols: ["Time", "Subscription", "Status", "Notification", "Headers", "Payload"],
    row: (m) => [
      time(m.at),
      mono(m.subscription),
      status(m.status, m.status === 201),
      notification(m),
      stack("TTL " + (m.ttl || "–"), [m.urgency, m.topic && "topic " + m.topic].filter(Boolean).join(" · ")),
      stack(m.payload_bytes + " B", [m.content_encoding, m.vapid ? "VAPID" : "no VAPID"].filter(Boolean).join(" · ")),
    ],
  },
  turnstile: {
    title: "Turnstile",
    desc: "Cloudflare siteverify calls. XXXX.DUMMY.TOKEN.FAIL and an empty token fail.",
    cols: ["Time", "Result", "Token", "Secret", "Error codes", "Remote IP"],
    row: (m) => [
      time(m.at),
      status(m.success ? "success" : "failed", m.success),
      mono(m.token || "(empty)"),
      text(m.secret_kind),
      text((m.error_codes || []).join(", ") || "–"),
      text(m.remoteip || "–"),
    ],
  },
  stt: {
    title: "Speech-to-text",
    desc: "Sarvam transcriptions, matched by the sha256 of the audio.",
    cols: ["Time", "Status", "Transcript", "Language", "Audio", "Fixture"],
    row: (m) => [
      time(m.at),
      status(m.status, m.status === 200),
      wrap(m.error || m.transcript),
      stack(m.language_code || "–", m.language_requested ? "asked " + m.language_requested : m.model),
      stack(m.bytes + " B", (m.sha256 || "").slice(0, 12) + "…"),
      text(m.fixture || "–"),
    ],
  },
  llm: {
    title: "LLM",
    desc: "Anthropic Messages calls, answered from fixtures (or recorded in live mode).",
    cols: ["Time", "Feature", "Status", "Response", "Prompt", "Fixture", "Tokens"],
    row: (m) => [
      time(m.at),
      stack(m.feature || "(none)", m.model),
      status(m.status + (m.stop_reason ? " · " + m.stop_reason : ""), m.status === 200),
      pre(m.error || m.response),
      wrap(m.prompt),
      stack(m.recorded || m.fixture || "–", m.mode + (m.stream ? " · stream" : "")),
      stack(m.input_tokens + " in", m.output_tokens + " out"),
    ],
  },
};

let kind = "sms";
let live = true;
let timer = null;
let lastJSON = "";

// --- cells ---

function el(tag, cls, txt) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (txt !== undefined && txt !== null) e.textContent = String(txt);
  return e;
}

function td(cls, ...children) {
  const c = el("td", cls);
  for (const ch of children) if (ch) c.append(ch);
  return c;
}

function text(v) { return td("", el("span", "", v ?? "–")); }
function mono(v) { return td("tz-mock-mono", el("span", "", v ?? "–")); }
function wrap(v) { return td("tz-mock-wrap", el("span", "", v || "–")); }
function pre(v) { return td("tz-mock-pre", el("span", "", v || "–")); }

function stack(main, sub) {
  const c = td("", el("span", "tz-mock-title", main ?? "–"));
  if (sub) c.append(el("span", "tz-mock-sub2", sub));
  return c;
}

function time(iso) {
  const d = new Date(iso);
  const c = td("tz-mock-time", el("span", "", d.toLocaleTimeString([], { hour12: false })));
  if (d.toDateString() !== new Date().toDateString()) c.append(el("span", "tz-mock-sub2", d.toLocaleDateString()));
  c.title = iso;
  return c;
}

// +919000000001 -> +91 90000 00001 (shown in full: this page is local only).
function formatPhone(p) {
  const m = /^\+91(\d{5})(\d{5})$/.exec(p || "");
  return m ? "+91 " + m[1] + " " + m[2] : p || "–";
}

function phone(p) { return td("tz-mock-phone", el("span", "", formatPhone(p))); }

function otp(code) {
  if (!code) return td("tz-mock-dim", el("span", "", "–"));
  const b = el("button", "tz-mock-otp", code);
  b.type = "button";
  b.title = "Copy code";
  b.addEventListener("click", async () => {
    try { await navigator.clipboard.writeText(code); } catch { /* clipboard may be blocked */ }
    b.dataset.copied = "true";
    setTimeout(() => { delete b.dataset.copied; }, 900);
  });
  return td("", b);
}

function status(label, ok) {
  const cls = ok === true ? "tz-pill tz-pill-completed" : ok === false ? "tz-pill tz-pill-rejected" : "tz-pill tz-pill-pending";
  return td("", el("span", cls, label));
}

function notification(m) {
  if (!m.title && !m.body) return td("tz-mock-dim tz-mock-wrap", el("span", "", m.plaintext ? m.plaintext : "(encrypted, no plaintext header)"));
  const c = td("tz-mock-wrap", el("span", "tz-mock-title", m.title || ""));
  if (m.body) c.append(el("span", "", m.body));
  return c;
}

// --- rendering ---

function renderHead() {
  const k = KINDS[kind];
  document.getElementById("kind-title").textContent = k.title;
  document.getElementById("kind-desc").textContent = k.desc;
  const tr = el("tr");
  for (const c of k.cols) tr.append(el("th", "", c));
  document.getElementById("thead").replaceChildren(tr);
  for (const b of document.querySelectorAll("[data-kind]")) {
    b.setAttribute("aria-pressed", String(b.dataset.kind === kind));
  }
}

function renderRows(items) {
  const k = KINDS[kind];
  if (items === null) {
    // Tab switch: drop the previous tab's rows at once, never show them under new headers.
    document.getElementById("tbody").replaceChildren();
    document.getElementById("empty").hidden = true;
    document.getElementById("foot-right").textContent = "";
    return;
  }
  const rows = items.map((m) => {
    const tr = el("tr");
    tr.dataset.id = m.id;
    tr.append(...k.row(m));
    return tr;
  });
  document.getElementById("tbody").replaceChildren(...rows);
  document.getElementById("empty").hidden = items.length > 0;
  document.getElementById("foot-right").textContent = items.length + (items.length === 1 ? " entry" : " entries");
}

async function load(force) {
  const want = kind; // a reply for a tab the user has since left is dropped
  try {
    const [list, sum] = await Promise.all([
      fetch("/_mock/" + want + "?limit=200", { cache: "no-store" }).then((r) => r.json()),
      fetch("/_mock/summary", { cache: "no-store" }).then((r) => r.json()),
    ]);
    for (const [k, n] of Object.entries(sum.counts || {})) {
      const c = document.querySelector('[data-count="' + k + '"]');
      if (c) c.textContent = n;
    }
    const mode = document.getElementById("llm-mode");
    mode.textContent = "LLM: " + sum.llm_mode;
    mode.className = sum.llm_mode === "live" ? "tz-pill tz-pill-pending" : "tz-pill";
    if (want !== kind) return;
    const json = JSON.stringify(list.items);
    if (force || json !== lastJSON) {
      lastJSON = json;
      renderRows(list.items || []);
    }
    document.getElementById("foot-left").textContent = live
      ? "Newest first. Refreshes every 2 seconds."
      : "Newest first. Paused.";
  } catch {
    document.getElementById("foot-left").textContent = "Cannot reach the mocks server.";
  }
}

function schedule() {
  clearInterval(timer);
  timer = live ? setInterval(() => load(false), POLL_MS) : null;
  const pill = document.getElementById("live-pill");
  pill.textContent = live ? "Live" : "Paused";
  pill.className = live ? "tz-pill tz-pill-completed" : "tz-pill tz-pill-pending";
  const t = document.getElementById("toggle-live");
  t.setAttribute("aria-pressed", String(!live));
  t.querySelector("span").textContent = live ? "Pause" : "Resume";
  t.querySelector("use").setAttribute("href", live ? "#i-pause" : "#i-play");
}

function select(k) {
  if (typeof k !== "string" || !Object.hasOwn(KINDS, k)) k = "sms"; // "#constructor" is not a kind
  kind = k;
  if (location.hash !== "#" + k) history.replaceState(null, "", "#" + k);
  lastJSON = "";
  renderHead();
  renderRows(null);
  load(true);
}

document.querySelectorAll("[data-kind]").forEach((b) => b.addEventListener("click", () => select(b.dataset.kind)));
document.getElementById("refresh").addEventListener("click", () => load(true));
document.getElementById("toggle-live").addEventListener("click", () => { live = !live; schedule(); });
document.getElementById("clear").addEventListener("click", async () => {
  await fetch("/_mock/" + kind, { method: "DELETE" });
  load(true);
});
window.addEventListener("hashchange", () => select(location.hash.slice(1)));

select(location.hash.slice(1));
schedule();
