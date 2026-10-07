#!/usr/bin/env python3
"""Linear CLI — the one way this repo reads and writes Linear.

Why this exists
---------------
Linear's GraphQL API has no `patch` argument, on `projectUpdate` or anything
else (366 mutations, none named patch/append). The MCP connector advertises one
and rejects it in every shape. So the only way to edit a project body was to
resend ~44KB in full, which is a blind write: nothing can see what changed, and
one formatting slip silently rewrites the whole record.

The discovery that fixes it: `ProjectUpdateInput.content` is a field SEPARATE
from `description`, documented as "The project content as markdown", while
`description` is the ~127-char one-line summary. Editing a body is therefore a
read-modify-write of one field, not a resend. Verified on a throwaway project:
`description` came back byte-identical, mentions survived, and the stored text
came back as ordinary markdown links.

What this buys
--------------
  * `project-body` edits surgically, and prints a diff before writing.
  * `comment`/`state`/`issue` are one cheap call instead of an MCP round trip
    that may collapse an array argument into an object.
  * Rate-limit headers are honoured, so a burst throttles itself instead of
    discovering the ceiling by failing.
  * GraphQL returns HTTP 200 with a populated `errors` array on partial
    success. Every helper here refuses to treat a non-empty `errors` as
    success — that bug shape already cost this repo one misread probe.

Credentials
-----------
The token is read from the path in LINEAR_TOKEN_FILE (default
`~/.linear_token`, mode 0600) or the LINEAR_API_KEY environment variable. It
is deliberately NOT read from the repo's `.env`: both app services declare
`env_file: .env`, so a key there is injected into the web and worker
containers.

Usage
-----
  linear.py project-body --project P-DJI-28 --file body.md [--dry-run]
  linear.py project-body --project P-DJI-28 --stdout
  linear.py comment DJI-596 --file note.md
  linear.py state DJI-596 Done
  linear.py issue --title "..." [--description-file f] [--project P-DJI-28]
  linear.py issue-list --project P-DJI-28 [--state Backlog] [--json]
  linear.py rate-limit
"""

import argparse
import difflib
import io
import json
import os
import sys
import time
import urllib.error
import urllib.request

sys.stdout.reconfigure(encoding="utf-8", errors="replace")

ENDPOINT = "https://api.linear.app/graphql"


def die(msg, code=1):
    sys.stderr.write("linear: %s\n" % msg)
    sys.exit(code)


def token():
    path = os.environ.get("LINEAR_TOKEN_FILE") or os.path.expanduser("~/.linear_token")
    if os.path.exists(path):
        with io.open(path, encoding="utf-8") as fh:
            tok = fh.read().strip()
        if tok:
            return tok
    env = os.environ.get("LINEAR_API_KEY")
    if env and env.strip():
        return env.strip()
    die("no Linear token: set LINEAR_API_KEY or write one to %s" % path)


class Api:
    """Thin GraphQL client that refuses to call a partial success a success."""

    def __init__(self, tok, verbose=False):
        self.token = tok
        self.verbose = verbose
        self.remaining = None
        self.limit = None

    def gql(self, query, variables=None, retries=4):
        payload = json.dumps({"query": query, "variables": variables or {}})
        for attempt in range(retries):
            req = urllib.request.Request(
                ENDPOINT,
                data=payload.encode("utf-8"),
                headers={
                    "Content-Type": "application/json",
                    "Authorization": self.token,
                    "User-Agent": "netrunner-linear-cli",
                },
            )
            try:
                with urllib.request.urlopen(req, timeout=60) as resp:
                    body = json.loads(resp.read().decode("utf-8"))
                    self.limit = resp.headers.get("X-RateLimit-Requests-Limit")
                    self.remaining = resp.headers.get("X-RateLimit-Requests-Remaining")
            except urllib.error.HTTPError as e:
                raw = e.read().decode("utf-8", "replace")
                if e.code in (429, 400) and "RATELIMITED" in raw:
                    wait = 5 * (2 ** attempt)
                    if self.verbose:
                        sys.stderr.write("linear: rate limited, sleeping %ds\n" % wait)
                    time.sleep(wait)
                    continue
                if e.code >= 500 and attempt + 1 < retries:
                    time.sleep(2 * (attempt + 1))
                    continue
                die("HTTP %d: %s" % (e.code, raw[:400]))
            except urllib.error.URLError as e:
                if attempt + 1 < retries:
                    time.sleep(2 * (attempt + 1))
                    continue
                die("network error: %s" % e)

            errs = body.get("errors")
            if errs:
                # HTTP 200 + errors is a real Linear behaviour; treating it as
                # success is how a probe reads a failed field as "empty".
                msg = errs[0].get("message", "")
                code = (errs[0].get("extensions") or {}).get("code", "")
                die("GraphQL error%s: %s" % (" [%s]" % code if code else "", msg[:400]))
            return body.get("data")
        die("giving up after %d attempts (rate limited)" % retries)

    def throttle(self):
        """Back off proactively when the budget is nearly spent."""
        try:
            left = int(self.remaining)
        except (TypeError, ValueError):
            return
        if left <= 5:
            time.sleep(min(60, (6 - left) * 5))


def resolve_project(api, ref):
    """Accept an identifier (P-DJI-28), a uuid, or a name fragment; return a uuid.

    `project(id:)` takes the human identifier as well as the uuid, so the
    common case is one call. ProjectFilter has no `identifier` field -- only
    id/name/slugId -- so a name fragment needs the separate search below.
    """
    data = api.gql(
        """query($id:String!){ project(id:$id){ id identifier name
             teams{ nodes{ id key } } } }""",
        {"id": ref},
    )
    p = data.get("project")
    if p:
        return p
    data = api.gql(
        """query($q:String!){ projects(first:20, filter:{name:{contains:$q}}){
             nodes{ id identifier name teams{ nodes{ id key } } } } }""",
        {"q": ref},
    )
    nodes = (data.get("projects") or {}).get("nodes") or []
    if not nodes:
        die("no project matches %r" % ref)
    if len(nodes) > 1:
        sys.stderr.write(
            "linear: %r matches %d projects; using %s\n"
            % (ref, len(nodes), nodes[0]["identifier"])
        )
    return nodes[0]


def read_project_body(api, ref):
    data = api.gql(
        """query($id:String!){ project(id:$id){
             id identifier name description content
             documentContent{ content } } }""",
        {"id": ref},
    )
    p = data.get("project")
    if not p:
        die("project %r not found" % ref)
    # documentContent.content is the authoritative stored text; content mirrors
    # it. Prefer the document layer so we edit exactly what will be replaced.
    dc = (p.get("documentContent") or {}).get("content")
    body = dc if dc is not None else (p.get("content") or "")
    return p, body


def show_diff(old, new, label):
    if old == new:
        sys.stderr.write("linear: %s unchanged; nothing to write\n" % label)
        return False
    diff = difflib.unified_diff(
        old.splitlines(keepends=True),
        new.splitlines(keepends=True),
        fromfile="linear/%s (current)" % label,
        tofile="linear/%s (proposed)" % label,
    )
    sys.stdout.write("".join(diff))
    return True


def emit_body(text):
    """Write a project body to stdout byte-exactly.

    sys.stdout is a TEXT stream: on Windows it translates every LF to CRLF on
    the way out, so a `--stdout > body.md` capture carried CR on all 215
    lines of a real body and did not match the bytes Linear holds. The binary
    layer does not translate, so write through it.

    Scope, stated honestly: feeding such a capture back to --file did NOT
    rewrite the body, because that side reads with io.open(encoding="utf-8")
    and universal newlines repair CRLF on the way in. The capture was still
    wrong -- anything comparing bytes against it (a diff, a hash, another
    tool, a commit) saw a different document -- and it becomes destructive
    the moment --file is hardened the same way.
    """
    buf = getattr(sys.stdout, "buffer", None)
    if buf is None:
        # Only reachable when stdout is an in-memory text sink. A real process
        # always has the binary layer, which is where the fix does its work.
        sys.stdout.write(text)
        return
    sys.stdout.flush()
    buf.write(text.encode("utf-8"))
    buf.flush()


def cmd_project_body(api, args):
    p, current = read_project_body(api, args.project)
    if args.stdout:
        emit_body(current)
        return 0
    if not args.file:
        die("project-body needs --file or --stdout")

    with io.open(args.file, encoding="utf-8") as fh:
        proposed = fh.read()

    if not show_diff(current, proposed, "project-body"):
        return 0
    if args.dry_run:
        sys.stderr.write("linear: --dry-run, not writing\n")
        return 0

    api.throttle()
    # `content` only. Never send `description` — it is the one-line summary and
    # an earlier bug lived precisely in conflating the two.
    api.gql(
        """mutation($id:String!,$c:String!){ projectUpdate(id:$id, input:{content:$c}){
             success project{ identifier } } }""",
        {"id": p["id"], "c": proposed},
    )
    _, after = read_project_body(api, p["identifier"])
    if after != proposed:
        sys.stderr.write(
            "linear: WARNING Linear normalised the body on write; stored text "
            "differs from what was sent (this is expected for tables/prose). "
            "Re-read is authoritative.\n"
        )
    sys.stderr.write("linear: wrote project-body of %s (%d chars)\n" % (p["identifier"], len(after)))
    return 0


def cmd_comment(api, args):
    body = args.body
    if args.file:
        with io.open(args.file, encoding="utf-8") as fh:
            body = fh.read()
    if not body:
        die("comment needs --body or --file")
    api.throttle()
    data = api.gql(
        """mutation($issue:String!,$body:String!){ commentCreate(input:{
             issueId:$issue, body:$body }){ success comment{ id url } } }""",
        {"issue": args.issue, "body": body},
    )
    res = data.get("commentCreate") or {}
    if not res.get("success"):
        die("commentCreate reported failure")
    sys.stderr.write("linear: commented on %s -> %s\n" % (args.issue, (res.get("comment") or {}).get("url")))
    return 0


def cmd_state(api, args):
    name = args.state
    data = api.gql(
        '{ workflowStates(first:40){ nodes{ id name type } } }'
    )
    states = (data.get("workflowStates") or {}).get("nodes") or []
    match = [s for s in states if s["name"].lower() == name.lower()]
    if not match:
        die("no workflow state named %r (have: %s)"
            % (name, ", ".join(s["name"] for s in states)))
    api.throttle()
    data = api.gql(
        """mutation($id:String!,$s:String!){ issueUpdate(id:$id, input:{stateId:$s}){
             success issue{ identifier state{ name } } } }""",
        {"id": args.issue, "s": match[0]["id"]},
    )
    iss = (data.get("issueUpdate") or {}).get("issue") or {}
    sys.stderr.write("linear: %s -> %s\n" % (iss.get("identifier"), (iss.get("state") or {}).get("name")))
    return 0


def cmd_issue(api, args):
    desc = args.description
    if args.description_file:
        with io.open(args.description_file, encoding="utf-8") as fh:
            desc = fh.read()
    variables = {"title": args.title, "description": desc}
    inp = "title: $title, description: $description"
    decls = ""
    if args.project:
        # IssueCreateInput takes a scalar `teamId: String!`, NOT a `teamIds`
        # array. Confirmed by introspecting the live schema: 36 input fields,
        # exactly one team field (`teamId`, NON_NULL<String>), and no
        # `teamIds` at all. Sending the array is an HTTP 400 naming both.
        proj = resolve_project(api, args.project)
        teams = (proj.get("teams") or {}).get("nodes") or []
        if not teams:
            die("project %r has no team" % args.project)
        variables["team"] = teams[0]["id"]
        # --project used to resolve the team and then silently drop the
        # project: issueCreate returned success and the issue landed with
        # project == null. `projectId` is what actually attaches it.
        variables["projectId"] = proj["id"]
        inp += ", teamId: $team, projectId: $projectId"
        decls = "$team:String!,$projectId:String"
    api.throttle()
    data = api.gql(
        "mutation($title:String!,$description:String,%s){ issueCreate(input:{%s}){ success issue{ identifier url } } }"
        % (
            decls,
            inp,
        ),
        variables,
    )
    iss = (data.get("issueCreate") or {}).get("issue") or {}
    sys.stdout.write("%s\t%s\n" % (iss.get("identifier"), iss.get("url")))
    return 0


def cmd_issue_list(api, args):
    # Filtering lives in the `filter:` argument -- Query.issues has no `project`
    # or `state` field argument. `project.id` is ID-typed, so $pid must be
    # declared ID, not String; Linear rejects the mismatch at validation time.
    conds = []
    variables = {}
    decls = []
    if args.project:
        decls.append("$pid:ID")
        variables["pid"] = resolve_project(api, args.project)["id"]
        conds.append("project:{id:{eq:$pid}}")
    if args.state:
        decls.append("$state:String")
        variables["state"] = args.state
        conds.append("state:{name:{eq:$state}}")
    filt = ", filter:{%s}" % ",".join(conds) if conds else ""
    # Explicit `first` keeps complexity low: default pagination is 50 and the
    # cost multiplies across child fields.
    data = api.gql(
        "query(%s){ issues(first:%d%s, orderBy: updatedAt){ "
        "nodes{ identifier title state{ name } updatedAt url } } }"
        % (",".join(decls), min(max(args.limit, 1), 50), filt),
        variables,
    )
    nodes = (data.get("issues") or {}).get("nodes") or []
    if args.json:
        sys.stdout.write(json.dumps(nodes, indent=2, ensure_ascii=False) + "\n")
        return 0
    for n in nodes:
        sys.stdout.write("%-10s %-12s %s\n" % (n["identifier"], n["state"]["name"], n["title"][:70]))
    sys.stderr.write("linear: %d issue(s)\n" % len(nodes))
    return 0


def cmd_webhook_list(api, args):
    """List org webhooks. The signing secret is readable here, so a UI copy of
    it is never required."""
    data = api.gql(
        """{ webhooks(first:20){ nodes{ id label url enabled team{ name }
             resourceTypes } } }"""
    )
    nodes = (data.get("webhooks") or {}).get("nodes") or []
    if args.secrets:
        for n in nodes:
            n["secret"] = _webhook_secret(api, n["id"])
    if args.json:
        sys.stdout.write(json.dumps(nodes, indent=2, ensure_ascii=False) + "\n")
        return 0
    for n in nodes:
        sys.stderr.write(
            "linear: %s %-22s %-16s %s%s\n"
            % ("on " if n["enabled"] else "off", n["label"] or "-",
               (n.get("team") or {}).get("name", "all teams"),
               ",".join(n.get("resourceTypes") or []),
               ("  secret=" + n["secret"]) if n.get("secret") else "")
        )
        sys.stderr.write("      %s\n" % n["url"])
    return 0


def _webhook_secret(api, webhook_id):
    data = api.gql(
        """query($id:String!){ webhook(id:$id){ id label url secret enabled } }""",
        {"id": webhook_id},
    )
    w = data.get("webhook") or {}
    return w.get("secret")


def cmd_webhook_register(api, args):
    """Create the webhook for the receiver and print its signing secret.

    Deliberately reports the URL it registered: Linear retries a failing endpoint
    three times and then may disable it, so what is registered must be visible.
    """
    if not args.url.startswith("https://"):
        die("--url must be https:// (Linear rejects plain http and localhost)")
    if "localhost" in args.url or "127.0.0.1" in args.url:
        die("Linear will not deliver to a localhost URL")
    types = args.types or ["Issue", "Comment", "Project"]
    data = api.gql(
        """mutation($url:String!,$types:[String!]!,$all:Boolean){
             webhookCreate(input:{url:$url, resourceTypes:$types, allPublicTeams:$all}){
               success webhook{ id label url enabled } } }""",
        {"url": args.url, "types": types, "all": True},
    )
    res = data.get("webhookCreate") or {}
    if not res.get("success"):
        die("webhookCreate reported failure")
    hook = res.get("webhook") or {}
    sys.stderr.write("linear: registered %s -> %s\n" % (hook.get("id"), hook.get("url")))
    secret = _webhook_secret(api, hook["id"])
    if args.print_secret and secret:
        sys.stdout.write(secret + "\n")
        sys.stderr.write(
            "linear: signing secret above -- store it with "
            "`wrangler secret put LINEAR_WEBHOOK_SECRET`\n"
        )
    else:
        sys.stderr.write("linear: re-read it with: linear.py webhook-list --secrets\n")
    return 0


def cmd_events(api, args):
    """Drain the receiver queue. PUSH beats polling Linear, which Linear's own
    docs discourage; this reads OUR queue, not their API."""
    if not args.url.startswith("https://"):
        die("--url must be https://")
    token = _drain_token()
    req = urllib.request.Request(
        args.url.rstrip("/") + "/events?since=%d" % max(0, args.since),
        headers={
            "Authorization": "Bearer " + token,
            # Cloudflare answers Python's default urllib agent with
            # "error code: 1010" on a workers.dev route, which reads as an auth
            # failure but is a bot-fingerprint block. Any explicit UA passes.
            "User-Agent": "netrunner-linear-cli/1.0",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            body = json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        die("receiver HTTP %d: %s" % (e.code, e.read().decode("utf-8", "replace")[:300]))
    except urllib.error.URLError as e:
        die("cannot reach receiver at %s: %s" % (args.url, e))

    events = body.get("events") or []
    if args.json:
        sys.stdout.write(json.dumps(body, indent=2, ensure_ascii=False) + "\n")
        return 0
    for ev in events:
        line = "%s  %-8s %-14s %s" % (
            (ev.get("receivedAt") or "")[:19], ev.get("action", "?"),
            ev.get("type", "?"), ev.get("url", ""),
        )
        sys.stdout.write(line + "\n")
        # A state change is the case that matters most, so show the transition.
        uf = ev.get("updatedFrom")
        if uf:
            for key, before in uf.items():
                after = ((ev.get("data") or {}).get(key))
                if isinstance(before, dict):
                    before = before.get("name")
                if isinstance(after, dict):
                    after = after.get("name")
                if before != after:
                    sys.stderr.write("      %s: %s -> %s\n" % (key, before, after))
    sys.stderr.write(
        "linear: %d event(s); pass --since %d next time\n"
        % (len(events), body.get("next", args.since))
    )
    return 0


def _drain_token():
    path = os.environ.get("LINEAR_DRAIN_TOKEN_FILE") or os.path.expanduser(
        "~/.linear_drain_token"
    )
    if os.path.exists(path):
        with io.open(path, encoding="utf-8") as fh:
            tok = fh.read().strip()
        if tok:
            return tok
    tok = os.environ.get("LINEAR_DRAIN_TOKEN")
    if tok and tok.strip():
        return tok.strip()
    die("no drain token: set LINEAR_DRAIN_TOKEN or write one to %s" % path)


def cmd_rate_limit(api, args):
    api.gql("{ viewer{ id name email } }")
    sys.stdout.write(
        "requests remaining: %s / %s\n" % (api.remaining or "?", api.limit or "?")
    )
    return 0


def main(argv=None):
    ap = argparse.ArgumentParser(prog="linear.py", description=__doc__)
    ap.add_argument("--verbose", action="store_true")
    sub = ap.add_subparsers(dest="cmd", required=True)

    pb = sub.add_parser("project-body", help="read/compare/replace a project body")
    pb.add_argument("--project", required=True)
    pb.add_argument("--file")
    pb.add_argument("--stdout", action="store_true")
    pb.add_argument("--dry-run", action="store_true")
    pb.set_defaults(fn=cmd_project_body)

    cm = sub.add_parser("comment", help="add a comment to an issue")
    cm.add_argument("issue")
    cm.add_argument("--body")
    cm.add_argument("--file")
    cm.set_defaults(fn=cmd_comment)

    st = sub.add_parser("state", help="move an issue to a workflow state by name")
    st.add_argument("issue")
    st.add_argument("state")
    st.set_defaults(fn=cmd_state)

    is_ = sub.add_parser("issue", help="create an issue")
    is_.add_argument("--title", required=True)
    is_.add_argument("--description")
    is_.add_argument("--description-file")
    is_.add_argument("--project")
    is_.set_defaults(fn=cmd_issue)

    il = sub.add_parser("issue-list", help="list issues, newest-updated first")
    il.add_argument("--project")
    il.add_argument("--state")
    il.add_argument("--limit", type=int, default=25)
    il.add_argument("--json", action="store_true")
    il.set_defaults(fn=cmd_issue_list)


    wh = sub.add_parser("webhook-list", help="list org webhooks")
    wh.add_argument("--secrets", action="store_true",
                    help="also print each signing secret (readable via the API)")
    wh.add_argument("--json", action="store_true")
    wh.set_defaults(fn=cmd_webhook_list)

    wr = sub.add_parser("webhook-register", help="create a webhook for the receiver")
    wr.add_argument("--url", required=True, help="public https URL of the Worker")
    wr.add_argument("--types", nargs="*",
                    help="resource types (default: Issue Comment Project)")
    wr.add_argument("--print-secret", action="store_true",
                    help="print the signing secret to stdout")
    wr.set_defaults(fn=cmd_webhook_register)

    ev = sub.add_parser("events", help="drain the webhook receiver queue")
    ev.add_argument("--url", required=True, help="base URL of the Worker")
    ev.add_argument("--since", type=int, default=0,
                    help="index returned by the previous drain")
    ev.add_argument("--json", action="store_true")
    ev.set_defaults(fn=cmd_events)

    rl = sub.add_parser("rate-limit", help="show remaining request budget")
    rl.set_defaults(fn=cmd_rate_limit)

    args = ap.parse_args(argv)
    api = Api(token(), verbose=args.verbose)
    return args.fn(api, args)


if __name__ == "__main__":
    sys.exit(main())