/**
 * Linear webhook receiver — verify, then queue. Never lose an event.
 *
 * Why this exists instead of a laptop-hosted listener
 * ----------------------------------------------------
 * Linear requires a public, non-localhost HTTPS endpoint, expects a 200 within
 * 5 seconds, retries only three times (1 min / 1 hour / 6 hours), and may then
 * DISABLE the webhook until a human re-enables it. A receiver on a developer's
 * machine fails precisely when the machine is asleep or off the network, and
 * there is no retry budget left to recover. So the endpoint is always-on, and
 * the machine drains the queue on its own schedule instead of having to be
 * listening at exactly the right moment.
 *
 * Security
 * --------
 * Signature is HMAC-SHA256 over the RAW request body, hex encoded, compared
 * against the `linear-signature` header with a constant-time comparison. The
 * raw bytes matter: re-serialising parsed JSON changes the bytes and the
 * signature will not match.
 *
 * Replay: a valid signature is not enough on its own, so a delivery whose
 * `webhookTimestamp` is more than REPLAY_WINDOW_MS old is refused, and each
 * `Linear-Delivery` id is remembered so a retried delivery is acknowledged but
 * not stored twice.
 *
 * Storage
 * -------
 * Events are appended to a KV list under a single key, newest last, and the
 * queue is trimmed to MAX_QUEUE. `since` (an index or a timestamp) lets a
 * client drain only what it has not seen. KV is eventually consistent, which is
 * acceptable here: this is a notification queue for a human-scale workflow, not
 * a ledger.
 *
 * Endpoints
 *   POST /            Linear pushes here. 200 on accept, 401 on bad signature,
 *                     400 on replay/oversize, 500 to ask Linear to retry.
 *   GET  /events?since=N   drain the queue (requires DRAIN_TOKEN).
 *   GET  /health           liveness plus queue depth, no auth needed.
 */

const REPLAY_WINDOW_MS = 60 * 1000; // Linear's own recommendation is 60s.
const MAX_QUEUE = 500;
const QUEUE_KEY = "queue";
const SEEN_KEY = "seen";
const MAX_BODY_BYTES = 256 * 1024;

/** Constant-time string compare that does not leak length via early exit. */
function timingSafeEqualHex(a, b) {
  if (typeof a !== "string" || typeof b !== "string") return false;
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) {
    diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  }
  return diff === 0;
}

async function hmacHex(secret, rawBody) {
  const key = await crypto.subtle.importKey(
    "raw",
    new TextEncoder().encode(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"]
  );
  const sig = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(rawBody));
  return Array.from(new Uint8Array(sig))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

function json(body, status) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "content-type": "application/json; charset=utf-8",
      "cache-control": "no-store",
    },
  });
}

/** Append and trim in one round trip; KV has no atomic list append. */
async function enqueue(env, entry) {
  const raw = await env.LINEAR_EVENTS.get(QUEUE_KEY, "json");
  const queue = Array.isArray(raw) ? raw : [];
  queue.push(entry);
  const trimmed = queue.slice(-MAX_QUEUE);
  await env.LINEAR_EVENTS.put(QUEUE_KEY, JSON.stringify(trimmed));
  return trimmed.length;
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);

    if (url.pathname === "/health") {
      const raw = await env.LINEAR_EVENTS.get(QUEUE_KEY, "json");
      const queue = Array.isArray(raw) ? raw : [];
      return json({
        ok: true,
        queued: queue.length,
        oldest: queue.length ? queue[0].receivedAt : null,
        newest: queue.length ? queue[queue.length - 1].receivedAt : null,
      });
    }

    if (url.pathname === "/events") {
      // Draining exposes the whole queue, so it is gated. The token is compared
      // the same constant-time way as the signature.
      const supplied = request.headers.get("authorization") || "";
      const expected = "Bearer " + (env.DRAIN_TOKEN || "");
      if (!env.DRAIN_TOKEN || !timingSafeEqualHex(supplied, expected)) {
        return json({ error: "unauthorized" }, 401);
      }
      const since = Number(url.searchParams.get("since") || "0");
      const raw = await env.LINEAR_EVENTS.get(QUEUE_KEY, "json");
      const queue = Array.isArray(raw) ? raw : [];
      const pending = queue.slice(Math.max(0, since));
      return json({
        count: pending.length,
        total: queue.length,
        // The client stores this and passes it back as `since` next time.
        next: since + pending.length,
        events: pending,
      });
    }

    if (request.method !== "POST") {
      return json({ error: "method not allowed" }, 405);
    }

    // Read as text: the signature is over these exact bytes.
    const rawBody = await request.text();
    if (rawBody.length > MAX_BODY_BYTES) {
      return json({ error: "payload too large" }, 413);
    }

    const secret = env.LINEAR_WEBHOOK_SECRET;
    if (!secret) {
      // Misconfiguration must not silently accept everything.
      return json({ error: "receiver not configured" }, 500);
    }

    const signature = request.headers.get("linear-signature");
    const expected = await hmacHex(secret, rawBody);
    if (!timingSafeEqualHex(signature, expected)) {
      return json({ error: "bad signature" }, 401);
    }

    let event;
    try {
      event = JSON.parse(rawBody);
    } catch {
      return json({ error: "invalid json" }, 400);
    }

    // Replay guard: a correctly signed but stale delivery is refused.
    const ts = Number(event.webhookTimestamp || 0);
    const age = Date.now() - ts;
    if (!ts || age > REPLAY_WINDOW_MS || age < -REPLAY_WINDOW_MS) {
      return json(
        { error: "stale or missing webhookTimestamp", ageMs: age },
        400
      );
    }

    // A retried delivery (Linear's 3 retries) must not be stored twice.
    const delivery = request.headers.get("linear-delivery");
    if (delivery) {
      const seen = (await env.LINEAR_EVENTS.get(SEEN_KEY, "json")) || {};
      if (seen[delivery]) {
        // Already have it — acknowledge so Linear stops retrying.
        return json({ ok: true, duplicate: true, delivery }, 200);
      }
      seen[delivery] = ts;
      const keys = Object.keys(seen);
      if (keys.length > MAX_QUEUE) {
        for (const k of keys.slice(0, keys.length - MAX_QUEUE)) delete seen[k];
      }
      await env.LINEAR_EVENTS.put(SEEN_KEY, JSON.stringify(seen));
    }

    const depth = await enqueue(env, {
      delivery: delivery || null,
      action: event.action,
      type: event.type,
      url: event.url,
      actor: event.actor ? event.actor.name || event.actor.id : null,
      createdAt: event.createdAt,
      receivedAt: new Date().toISOString(),
      // `updatedFrom` is what makes a state change legible without a follow-up
      // query, so keep it rather than discarding it.
      updatedFrom: event.updatedFrom || null,
      data: event.data || null,
    });

    // 200 fast: Linear gives 5 seconds and only three retries.
    return json({ ok: true, queued: depth }, 200);
  },
};