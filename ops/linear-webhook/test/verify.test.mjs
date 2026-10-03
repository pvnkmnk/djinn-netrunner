/**
 * Offline tests for the Linear webhook receiver.
 *
 * The crypto is the security boundary, so it is tested without deploying: a real
 * HMAC-SHA256 over the exact bytes Linear sends, plus the replay guard, the
 * duplicate-delivery guard, and the drain authorisation.
 *
 * Run:  node --test ops/linear-webhook/test/
 *       (or: node ops/linear-webhook/test/verify.test.mjs)
 *
 * Uses Node's built-in node:test and a fake KV, so there is nothing to install.
 */

import test from "node:test";
import assert from "node:assert/strict";
import crypto from "node:crypto";

import worker from "../src/index.js";

const SECRET = "lin_wh_test_secret_value";
const DRAIN = "drain-token-abc123";

/** Minimal in-memory stand-in for the KV namespace binding. */
function fakeKV(initial = {}) {
  const store = { ...initial };
  return {
    store,
    // Cloudflare's KV honours the type hint: get(key, "json") parses the stored
    // string. A double that ignores it returns a raw string where the Worker
    // expects an object, which fails as a phantom bug in the Worker itself.
    async get(key, type) {
      const raw = store[key];
      if (raw === undefined || raw === null) return null;
      if (type === "json") {
        return typeof raw === "string" ? JSON.parse(raw) : raw;
      }
      return raw;
    },
    async put(key, value) {
      store[key] = value;
    },
  };
}

function env(overrides = {}) {
  return {
    LINEAR_WEBHOOK_SECRET: SECRET,
    DRAIN_TOKEN: DRAIN,
    LINEAR_EVENTS: fakeKV(),
    ...overrides,
  };
}

function sign(body, secret = SECRET) {
  return crypto.createHmac("sha256", secret).update(body, "utf8").digest("hex");
}

function payload(overrides = {}) {
  return JSON.stringify({
    action: "update",
    type: "Issue",
    url: "https://linear.app/djinnet/issue/DJI-1/x",
    actor: { id: "u1", name: "Test Actor" },
    createdAt: "2026-10-03T00:00:00.000Z",
    webhookTimestamp: Date.now(),
    webhookId: "wh1",
    data: { id: "i1", identifier: "DJI-1", state: { name: "Done" } },
    updatedFrom: { state: { name: "Backlog" } },
    ...overrides,
  });
}

function post(body, { secret = SECRET, signature, delivery = "d1", timestamp } = {}) {
  const headers = new Headers({
    "content-type": "application/json",
    "linear-signature": signature ?? sign(body, secret),
    "linear-delivery": delivery,
  });
  if (timestamp !== undefined) headers.set("x-test-timestamp", String(timestamp));
  return new Request("https://worker.example/", {
    method: "POST",
    headers,
    body,
  });
}

test("accepts a correctly signed delivery and queues it", async () => {
  const e = env();
  const body = payload();
  const res = await worker.fetch(post(body), e);
  assert.equal(res.status, 200);
  const out = await res.json();
  assert.equal(out.ok, true);
  assert.equal(out.queued, 1);

  const queue = JSON.parse(e.LINEAR_EVENTS.store.queue);
  assert.equal(queue.length, 1);
  assert.equal(queue[0].type, "Issue");
  assert.equal(queue[0].action, "update");
  // updatedFrom is what makes a state change legible with no follow-up query.
  assert.ok(queue[0].updatedFrom, "updatedFrom must be retained");
});

test("rejects a bad signature with 401 and queues nothing", async () => {
  const e = env();
  const body = payload();
  const res = await worker.fetch(post(body, { signature: "deadbeef" }), e);
  assert.equal(res.status, 401);
  assert.equal(e.LINEAR_EVENTS.store.queue, undefined, "nothing may be queued");
});

test("rejects a signature computed with a different secret", async () => {
  const e = env();
  const body = payload();
  const res = await worker.fetch(post(body, { secret: "wrong-secret" }), e);
  assert.equal(res.status, 401);
  assert.equal(e.LINEAR_EVENTS.store.queue, undefined);
});

test("signature is over raw bytes: re-serialised JSON is rejected", async () => {
  // The guard against "verify the parsed object" — parsed-then-restringified
  // bytes differ, so the signature must not match.
  const original = payload();
  const e = env();
  const res = await worker.fetch(
    post(JSON.stringify(JSON.parse(original), null, 2), { signature: sign(original) }),
    e
  );
  assert.equal(res.status, 401, "whitespace-changed body must fail verification");
});

test("rejects a stale delivery as a replay", async () => {
  const e = env();
  const old = Date.now() - 10 * 60 * 1000; // 10 minutes ago
  const body = payload({ webhookTimestamp: old });
  const res = await worker.fetch(post(body), e);
  assert.equal(res.status, 400);
  assert.equal(e.LINEAR_EVENTS.store.queue, undefined);
});

test("rejects a delivery timestamped far in the future", async () => {
  const e = env();
  const body = payload({ webhookTimestamp: Date.now() + 10 * 60 * 1000 });
  const res = await worker.fetch(post(body), e);
  assert.equal(res.status, 400);
});

test("rejects a delivery with no webhookTimestamp at all", async () => {
  const e = env();
  const body = JSON.stringify({ action: "update", type: "Issue" });
  const res = await worker.fetch(post(body), e);
  assert.equal(res.status, 400);
});

test("a Linear retry is acknowledged but not queued twice", async () => {
  const e = env();
  const body = payload();
  const first = await worker.fetch(post(body, { delivery: "dup-1" }), e);
  assert.equal(first.status, 200);
  const second = await worker.fetch(post(body, { delivery: "dup-1" }), e);
  // Must be 200 so Linear stops retrying, but flagged as a duplicate.
  assert.equal(second.status, 200);
  assert.equal((await second.json()).duplicate, true);
  const queue = JSON.parse(e.LINEAR_EVENTS.store.queue);
  assert.equal(queue.length, 1, "the retry must not be stored a second time");
});

test("an unconfigured receiver fails closed rather than accepting anything", async () => {
  const e = env({ LINEAR_WEBHOOK_SECRET: "" });
  const res = await worker.fetch(post(payload()), e);
  assert.equal(res.status, 500);
  assert.equal(e.LINEAR_EVENTS.store.queue, undefined);
});

test("non-POST to / is rejected", async () => {
  const res = await worker.fetch(new Request("https://worker.example/"), env());
  assert.equal(res.status, 405);
});

test("health needs no auth and reports queue depth", async () => {
  const e = env();
  await worker.fetch(post(payload()), e);
  const res = await worker.fetch(new Request("https://worker.example/health"), e);
  assert.equal(res.status, 200);
  const out = await res.json();
  assert.equal(out.ok, true);
  assert.equal(out.queued, 1);
});

test("draining requires the bearer token", async () => {
  const e = env();
  await worker.fetch(post(payload()), e);

  const anon = await worker.fetch(new Request("https://worker.example/events"), e);
  assert.equal(anon.status, 401);

  const wrong = await worker.fetch(
    new Request("https://worker.example/events", {
      headers: { authorization: "Bearer nope" },
    }),
    e
  );
  assert.equal(wrong.status, 401);

  const ok = await worker.fetch(
    new Request("https://worker.example/events", {
      headers: { authorization: "Bearer " + DRAIN },
    }),
    e
  );
  assert.equal(ok.status, 200);
  const out = await ok.json();
  assert.equal(out.count, 1);
  assert.equal(out.next, 1);
  assert.equal(out.events[0].type, "Issue");
});

test("draining is resumable via since", async () => {
  const e = env();
  await worker.fetch(post(payload({ data: { id: "a" } }), { delivery: "d1" }), e);
  await worker.fetch(post(payload({ data: { id: "b" } }), { delivery: "d2" }), e);

  const first = await (
    await worker.fetch(
      new Request("https://worker.example/events?since=0", {
        headers: { authorization: "Bearer " + DRAIN },
      }),
      e
    )
  ).json();
  assert.equal(first.count, 2);

  const second = await (
    await worker.fetch(
      new Request(`https://worker.example/events?since=${first.next}`, {
        headers: { authorization: "Bearer " + DRAIN },
      }),
      e
    )
  ).json();
  assert.equal(second.count, 0, "already-drained events must not repeat");
});

test("draining fails closed when no drain token is configured", async () => {
  const e = env({ DRAIN_TOKEN: "" });
  const res = await worker.fetch(
    new Request("https://worker.example/events", {
      headers: { authorization: "Bearer anything" },
    }),
    e
  );
  assert.equal(res.status, 401);
});