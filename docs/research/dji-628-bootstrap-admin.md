# DJI-628 — Does the bootstrap-admin defect still exist, and is it a security issue?

**Verdict:** still live — and it is a **security** finding, not a product/UX one.

The claim is DJI-555: the bootstrap establishes an admin identity from an email
address alone, without proving control of that address. Verified against the
code as it stands today, the claim is true, and the design document of
2026-10-03 already concedes it.

## Evidence

### The address is the entire credential

`backend/internal/config/config.go:33-36`:

```go
	// BootstrapAdminEmail, when set, is promoted to the admin role at startup
	// and again when that address registers. Empty disables the bootstrap,
	// which is the default and safe to leave unset.
	BootstrapAdminEmail string
```

Read from `BOOTSTRAP_ADMIN_EMAIL` at `config.go:297`. The promotion itself,
`backend/internal/services/bootstrap_admin.go:111` and `:134`:

```go
func BootstrapAdmin(db *gorm.DB, email string) (BootstrapResult, error) {
	...
		if err := tx.Where("LOWER(TRIM(email)) = ?", normalized).First(&user).Error; err != nil {
```

and `:161`:

```go
			Update("role", AdminRole).Error; err != nil {
```

So the promotion is a **string comparison against a stored address**. Nothing else
is consulted — not a token, not a secret, not a mailbox.

### Registration is what triggers it, and registration is open

`backend/internal/api/auth.go:136-142`:

```go
	// An operator who configured BOOTSTRAP_ADMIN_EMAIL before registering is
	// promoted as soon as the account exists. Failure here must not fail the
	// registration - the account is already created and usable, and the next
	// boot retries the same promotion.
	if h.bootstrapAdminEmail != "" &&
		services.NormalizeBootstrapEmail(h.bootstrapAdminEmail) == user.Email {
		result, err := services.BootstrapAdmin(h.db, h.bootstrapAdminEmail)
```

The user row is created on the line above (`auth.go:132`) with the role
hardcoded at `auth.go:129`:

```go
		Role:         "user", // Hardcoded to prevent privilege escalation during registration
```

That comment is the crux of the finding: registration *is* hardened against
self-chosen escalation, and then the bootstrap re-introduces escalation for
exactly one address — decided by whoever gets there first. `/api/auth/register`
is public and needs no invite (rate-limited, per `AGENTS.md`).

### Nothing anywhere proves control of the address

Email is validated for **format only** — `net/mail.ParseAddress`, `auth.go:106`
(`addr, err := mail.ParseAddress(payload.Email)`). There is no verification
flow: no `EmailVerified` field, no token table, no confirmation route (searched
the tree: no such symbol exists). The only SMTP code is
`backend/internal/services/notification_service.go:10` (`"net/smtp"`), used for
job-completion and quota notifications (`FEATURES.md:64`), and it is configured
in the **worker** process (`backend/cmd/worker/main.go:183`), not the web
process that performs the promotion.

Normalization is format handling, not proof — `bootstrap_admin.go:88` folds
case, whitespace and an RFC 5322 display name.

### The documented example is nobody's address

`docs/DEPLOYMENT.md:184`:

```bash
BOOTSTRAP_ADMIN_EMAIL=you@example.com
```

An operator following the runbook literally sets a string that is (a) a
published example and (b) registrable by anyone. The promotion then fires for
whoever registers it first.

### The project already knows

`docs/superpowers/specs/2026-10-03-release-readiness-initiative-design.md:296-300`:

> `BOOTSTRAP_ADMIN_EMAIL` proves an *address* was configured, not that the person
> who registered it is the operator. Registration is open in every environment,
> so an anonymous client knowing the configured address can claim `admin` first.
>
> The ticket's own reasoning deferred this as proportionate risk on the grounds
> that NetRunner is self-hosted and single-machine... **That premise does not
> hold for a public open-source release**, which is why it is surfaced here
> rather than inherited.

The design document's §7 risk register also carries the row
`| Public release raises DJI-555's stakes | 2 | Threat model decided explicitly, not inherited. |`

The tests pin the *intended* behaviour and never probe pre-emption:
`backend/internal/api/bootstrap_admin_register_test.go:20`
(`TestRegister_PromotesTheBootstrapAccount`) proves the configured address is
promoted; `:36` (`TestRegister_DoesNotPromoteAnyoneElse`) proves other
addresses are not. No test asserts that the *legitimate owner* of the address is
the one promoted — which is precisely the property that does not hold.

## Threat model

1. Operator sets `BOOTSTRAP_ADMIN_EMAIL=ops@example.com` in `.env` and starts
   the stack. `main.go:57` runs `services.BootstrapAdmin` in the web process;
   with no account yet, it returns `NoAccount` and waits.
2. An unauthenticated client reaches `POST /api/auth/register`. No invite, no
   domain restriction, no verification.
3. The client registers `ops@example.com`, choosing its own password.
   `auth.go:132` creates the row, `auth.go:140-142` sees the match, and
   `bootstrap_admin.go:161` sets the role to `admin`.
4. The attacker logs in with the password it chose, holding a session on an
   admin account. Everything gated on `user.Role != "admin"` — the whole admin
   surface, `adminRoutes` at `main.go:469` (`PATCH /users/:id/role`), the system
   event socket at `websocket.go:146` — is now reachable.

**What the attacker must know:** one string — an address that in the documented
case is a public placeholder and in the common case is the operator's own
(publicly discoverable) email. They need no password, no token, no mailbox.

**Aggravating detail — the win is sticky, and it denies the operator.**
`bootstrap_admin.go:124-130` short-circuits on the audit marker:

```go
		if prior > 0 {
			result.AlreadyBootstrapped = true
			return nil
		}
```

So once the attacker's registration has written the marker, no later boot
re-promotes the address — the bootstrap is deliberately one-way so that it
cannot undo an operator's demotion (`TestBootstrapAdmin_DoesNotRevertALaterManualDemotion`,
`bootstrap_admin_test.go:130`). The legitimate operator can no longer register
that address either (the duplicate branch, `auth.go:114-118`, answers `201`
without creating a row). The documented recovery path
(`docs/DEPLOYMENT.md:185-190`, "restart the web container and the existing
account is promoted then") is therefore spent, and recovery requires the SQL
edit that DJI-542 existed to remove — or the admin surface the attacker now
controls.

**Not in scope of this finding:** the attacker cannot promote an arbitrary
address; the comparison is exact. The defect is narrower and sharper than
"anyone can become admin" — it is "the configured address is a bearer
credential, and the first person to present it wins."

## Security or product/UX?

**Security** — unauthenticated privilege escalation (account pre-emption on a
public endpoint). The design document independently reaches the same conclusion
at `:296-300` and rejects the self-hosted-only mitigation premise.

**Label that fits:** `security`. The team's `information-disclosure` is not it
(no data is disclosed), and `hardcoded-credentials` is close but wrong (the
address is configured, not compiled in). `vulnerability` is a defensible
alternative if that label is reserved for externally filed reports; this one is
found by reading.

## Disposition

**Fix now.** The design document already supplies the shape and a
recommendation at `:307-317`:

1. **Invite/enrollment secret** — a second env var the registrant must present.
   Recommended there, and it is the smallest change that actually proves
   possession of something the operator holds. It must be enforced on **both**
   promotion triggers (boot and registration).
2. **Email verification for the bootstrap address only** — correct but pulls in
   a verification flow, SMTP in the web process, and a token table.
3. **Record a decision not to fix** — legitimate only as a written threat model
   in `docs/DEPLOYMENT.md`, per the ticket's acceptance criteria.

Hard constraints on any fix, from the same section: it must cover the
registration-time *and* startup promotion paths, and must **not** add a
promotion trigger to the enumeration-safe `201` duplicate branch at
`auth.go:114-118`.

## What would falsify this verdict

- A check on the promotion path — beyond address equality — that requires
  proof of control (a token, an invite secret, or a verified mailbox). None
  exists today; `auth.go:140-141` compares only the normalized address.
- A default that refuses to boot or refuses to promote until the operator
  explicitly acknowledges the risk. `config.go:35` says empty simply
  "disables the bootstrap", which is the opposite.
- Evidence that `BOOTSTRAP_ADMIN_EMAIL` is treated as a secret and is
  unguessable in practice. `docs/DEPLOYMENT.md:184` documents it with a public
  placeholder, which is evidence against that.

## Could not verify

- **Whether any live deployment is actually exposed.** This is a code-level
  verdict. Whether a given instance's configured address is guessable, and
  whether its registration endpoint is reachable from the internet, depends on
  the deployment — outside what I can read. (Independently, `AGENTS.md` notes
  `ALLOW_PRIVATE_TARGETS=true` is mandatory on the compose stacks, which says
  something about the intended network posture, but not about exposure.)
- **I did not execute anything.** I have no terminal in this pass; every claim
  above is a quoted read of the file at the cited line. The one inference I
  draw rather than quote is the sticky-win consequence in step 4 of the threat
  model, which follows from `bootstrap_admin.go:128` plus the duplicate branch
  at `auth.go:114-118`.
- **Whether the pre-emption is reproducible end-to-end against a running
  stack.** The two facts that make it work are each pinned by a test
  (`TestRegister_PromotesTheBootstrapAccount`) but the *ordering* case — an
  attacker registering before the operator — is not covered by any test. A
  one-line integration test (register the configured address as a second party,
  assert `admin`) would settle it and is probably worth adding alongside the
  fix.
