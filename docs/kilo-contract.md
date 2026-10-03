# Kilo Code contract

What this plugin reads from Kilo Code, and what it deliberately does not.

## Tested baseline

| | |
| --- | --- |
| Kilo CLI | 7.8.1 (Linux x86_64) |
| Session store | `~/.local/share/kilo/kilo.db`, SQLite, opened `mode=ro` |
| Credential store | `~/.local/share/kilo/auth.json` |
| Model catalog | `~/.cache/kilo/models.json` |
| Herdr integration | `herdr integration install kilo`, reporting `agent_session.kind=id` with a `ses_…` id |

Kilo is an OpenCode fork and kept the same on-disk shapes under its own
directories. Every path here is Kilo's own; nothing resolves into OpenCode's
store, so a machine with both CLIs installed keeps the two apart.

## Context

Read from the `part` rows of type `step-finish` for the session herdr reports.
That row carries the prompt-cache occupancy for one model step:

```
context tokens = tokens.input + tokens.cache.read + tokens.cache.write
```

Output and reasoning tokens are excluded because the next step's input already
contains them — counting them here would double-count the window. This is the
same rule the OpenCode provider applies, and the same row Kilo's own "Token
Usage" panel reads.

The provider and model ids come from the `message` row named by the step's own
`message_id`, because only 88 of Kilo's 9140 step rows carry their own model
block. It has to be that message and not the session's newest assistant message:
after a model switch the newest message can belong to a call that has not
completed a step yet, and pairing this step's tokens with that message's model
would divide one model's context by another model's window. The context window
comes from `~/.cache/kilo/models.json` at
`kilo.models[modelID].limit.context`.

A step whose every counter is zero is a free-model step. It yields no usage at
all, because reading it as a 0-token context would present as an untouched
window.

### Session identity

One session is chosen per pane, in one place, and context, billing mode and pane
activity all read that one. Herdr captures `agent_session` at launch and never
refreshes it, so a cleared or resumed session reports an id that no longer
exists; the pane's cwd is the fallback.

The fallback only attributes a session when it is **unambiguous**. Two live Kilo
panes in one repository share a cwd, so "the newest session in this directory"
is not evidence of which pane asked: once one pane resets its session, the newest
row is the other pane's. Two or more live sessions in scope therefore yield no
reading rather than another pane's context, backend and spend.

"Recorded in this directory" is broader than "could be this pane's session", and
the scope drops the two kinds of row that cannot be:

- `parent_id IS NULL`. A subagent child session runs inside its parent's process
  and no pane ever launches one, so counting it makes a directory with a single
  pane in it look shared.
- `time_updated` within the last 12 hours. A session nobody has written to for a
  working day is history, not a pane. This is liveness evidence: `time_archived
  IS NULL` only says the session was never archived, and on a real store most
  never are. 12 hours spans a working day, so a pane left open overnight still
  resolves while yesterday's finished session does not block today's.

Both narrowings only remove rows that cannot be the answer. Two live top-level
sessions are two panes, and the fallback still yields nothing for them. The
recency floor never applies to a pane's own reported id: a session whose id still
resolves is attributed however old it is.

The scope is the directory and its descendants — the arm that covers a worktree
checked out under the repository. It is bounded by the path separator and carries
`ESCAPE '\'`, so `/repo` never reaches `/repo-other` and a directory containing
`"_"`, `"%"` or `"\"` is matched literally.

### Not used

- `session.model` is populated on only some sessions and is SQL NULL on most; it
  names the session's *last* model, which says nothing about what an earlier
  backend was spent on. It is not read.
- `session.cost` / `session.tokens_*` are lifetime totals across every backend the
  session used (see Identity), so they cannot answer a per-backend question.
- `session_context_epoch` exists in the schema but is empty in 7.8.1, so it is
  not a usable source.
- `session_message` and `session_input` (the v2 tables) are empty in 7.8.1. All
  data lives in the v1 `message`/`part` pair, which is what is read.

## Quota

One window, monthly. Kilo publishes no 5h or 7h bucket for the Kilo Gateway and
no rate limit at all — no response carries one, and none is inferred.

Source: `GET https://api.kilo.ai/api/trpc/kiloPass.getState`, the tRPC procedure
Kilo's own CLI calls for the "Kilo Pass" line in its account panel, authenticated
with the OAuth device login in `auth.json`.

| Field | Used as |
| --- | --- |
| `subscription.currentPeriodBaseCreditsUsd` | allowance |
| `subscription.currentPeriodBonusCreditsUsd` | allowance, added to the base |
| `subscription.currentPeriodUsageUsd` | spend |
| `subscription.nextBillingAt` / `nextRenewalAt` | reset time |
| `subscription.status` | must be `active`, `past_due`, or `trialing` — the CLI's own set |

Bonus credits are added to the allowance rather than reported separately: they
are granted into the same period and expire with it, so they are allowance, not
a top-up outside the window.

Both halves of the ratio are required. A missing spend would read as an
untouched period, which presents as a full allowance.

### Without a subscription

Most accounts have no Kilo Pass and pay from a shared credit balance. Kilo
reports that balance (`GET /api/profile/balance`) with **no limit attached**, so
there is no honest percentage to draw and none is drawn: the pane shows the
balance as a note and no bar. The credit pool's own percentage would degenerate
to a constant 100% on a drained account and would read as "quota exhausted",
which would be false.

`subscription: null` and a status outside the live set both degrade to that
state rather than to an error the user has to read — and both are answers, not
failures, so both **clear** a window this account had. A cancelled plan keeps
reporting the amounts it last had, so the status is checked before the ratio.

A window requires a status Kilo actually reported as live. A response that named
this period's allowance and its spend but **no** status is not metered either:
the contract allows only `active`, `past_due` and `trialing`, and a response that
names none has not said the plan is paying for the session. That is a missing
fact rather than a rejected plan, so it **preserves** the last good window
instead of clearing it.

The two failure kinds are kept apart deliberately. A request that fails —
transport, auth, server, decode — saves nothing, so the last good reading for
that account survives: a blip is not evidence that the plan ended. The same
holds for a response that names a plan but not a usable ratio, and for one that
names no status, neither of which says the account lost its Pass.

The cache stores the outcome, the last good reading and the failed attempt as
three separate fields, because preserving a reading and reporting it are
different acts. A still-fresh failed entry replays **its own** failure — no
window, and the note naming what went wrong — never the snapshot it preserved.
Serving that snapshot would put the previous period's window back on screen
stamped with the failed attempt's time and carrying none of its note, and
repeated failures would copy it forward indefinitely. The snapshot stays on disk
for a later attempt to recover from; an entry written before the two were stored
apart is re-read rather than served.

### Scoped to an organization

Kilo files the selected team in the login's `accountId`, and sends it as
`x-kilocode-organizationid` with the balance request. The balance read is
therefore scoped to that team's wallet.

A Kilo Pass is **personal scope**: Kilo's own CLI shows it for the signed-in
person, never for a team. A login scoped to an organization is an account with no
monthly allowance to meter, so it gets the balance and a note naming the
organization — and never the person's Pass window, which is a different account's
reading entirely. The Pass request carries no organization scope for the same
reason. This is an answer rather than a failure, so it clears a window this scope
had.

### Deliberately not collected

- **Rate limits.** Kilo publishes none, in any header or field. Any RPM/TPM
  figure would be invented.
- **Lifetime spend.** `/api/user` reports `microdollars_used` and
  `total_microdollars_acquired`, and `usageAnalytics.getSummary` reports a
  different lifetime `costMicrodollars` for the same account. The two are not
  reconcilable from any published field, so neither is presented as "spend".
- **The local `/kilocode/provider-usage` route.** Kilo's running server exposes
  it and it needs no credential, but the port is ephemeral and the account's
  `codingPlans.listSubscriptions` is empty, so it returns no items here. Not
  worth a port scan.
- **`KILO_API_URL`.** Not honoured: an override would send the login to a host
  this plugin cannot vouch for.

## Identity

Attribution is by the login, not the harness. Kilo drives OpenCode Go, Anthropic,
OpenRouter and others from one CLI, so the backend recorded on the session
decides:

| Session backend | Credential in Kilo's `auth.json` | Result |
| --- | --- | --- |
| `kilo` | `{"type":"oauth"}` gateway login | the Kilo Pass window |
| `kilo`, with `accountId` set | same login, scoped to a team | no window; the team's balance as a note |
| `kilo` | `{"type":"api"}` gateway key, or none | no window; keeps prior state |
| `opencode-go` | any | routed to the OpenCode collector as "OpenCode Go" |
| anything else | API key | pay-as-you-go, labelled with the backend, with the session's own cost and tokens |

A gateway API key bills the same account but cannot name it, so it is never the
attribution for a reading.

A pay-as-you-go Kilo backend produces an Agent Usage block like any other
harness, read from the store the same way. Its spend is summed **per backend**
from the assistant messages, which carry the backend that served each turn, and
it covers every session touched in the window rather than only the open pane's —
subagent children included, since their spend is drawn on the same credential.
The panel's block is labelled with the backend its pane is on now, so the gateway
spend a session accumulated earlier is not counted under a later label; it
already sits on the Kilo Pass window.

That per-backend scope is also why the session row's own token and cost columns
are never read. Kilo backfills them from its messages, but they are lifetime
totals for **every** backend the session ever used: a session that switched from
the gateway to a vendor key has one set of totals and two backends. Reading them
charges the earlier backend's spend to the later one's label, and presents
gateway spend a second time as a vendor bill. The figures a pay-as-you-go pane
shows are still lifetime rather than windowed — that is what the block shows — and
they are read only once the pane has been classified pay-as-you-go, never for a
plan budget.

The account-wide scan is driven from the session table rather than the message
table, with `CROSS JOIN` to pin that order: a plain `JOIN` lets SQLite choose the
multi-million-row message table as the outer loop, and the panel refreshes on a
15s ticker. Session is a few thousand rows and narrows the read to the sessions
that can hold a message in the window, each read through
`message_session_time_created_id_idx`.

## Credentials

The gateway access token is read for one HTTP request and never persisted. The
cached entry stores only the resolved limits plus a hash of the token **together
with the selected organization**, so a stale cache file cannot leak a login, a
second account's entry is refused rather than displayed, and switching teams
re-reads instead of inheriting the previous scope's numbers. The refresh token is
never read, and no API key value is ever copied out of the store.
