# NodePing API quirks

Places where the NodePing API (`https://api.nodeping.com/api/1/`) behaves in
ways its documentation does not describe, or contradicts it. They were found
in October 2026 and observed against the real API in a test SubAccount, and
the provider works around each one. The commits listed carry the full detail.

This file has two jobs. It explains to maintainers why the client does what it
does, and it is the source for a support request to NodePing.

## Summary

| # | Quirk | What the docs say | What to ask | Risk to other API users |
|---|-------|-------------------|-------------|-------------------------|
| 1 | Errors come back with HTTP 200 | 400 / 403 / 500 | Return the documented codes | High: opt-in or `/api/2` |
| 2 | A missing ID is never a 404, and the answer differs by type | Nothing | 404, or at least one consistent error | High for 404, low for a consistent error |
| 3 | Misleading error when a contact's last address is removed | Nothing | A clearer message | None |
| 4 | `queue` and `secure` change JSON type | `queue` is not documented; `secure` is a string | Document, or keep the types stable | Low |
| 5 | The web UI stores `warningdays: 0` | "optional positive integer" | Store nothing, or document 0 | Low |
| 6 | Headers stored as `null` | "key:value pairs" | Document, or drop the nulls | Low |
| 7 | Older checks hold short-form notifications | Only the object form | Migrate, or document | Low |
| 8 | What an update does with an omitted field | Nothing | Document it | None |
| 9 | An update is sometimes answered with the check as it was before | Returns the check "created or modified" | Answer with the updated check | None |
| 10 | A parameter the check type does not take is dropped without a word | Which parameters each type takes, nearly right | Say so in the answer, and fix the per-type lists | Low |
| 11 | A type change freezes the old type's parameters | Nothing | Document it, or let an update clear them | Low |
| 12 | A check created without `sens` holds none | "Defaults to 2" | Store the default, or document what none means | Low |
| 13 | An invalid check value is refused, ignored or changed | `interval` 0.25, 0.5 or an integer from 1; `sens` and `threshold` from 1 | Refuse every invalid value, naming the field | Low |

## 1. Errors come back with HTTP 200

The API overview says errors return 400 (bad URL or parameter), 403 (token or
permissions) or 500/501 (a bug), each with an `error` key in the body. In
practice most failures return **200** with that body:

| Request | Status | Body |
|---------|--------|------|
| `POST /checks` with a bad target | 200 | `{"error":"target: Invalid URL"}` |
| `PUT /checks/<unknown id>` | 200 | `{"error":"Unable to load check."}` |
| `GET /checks/<deleted id>` | 200 | `{"error":"Error fetching check."}` |
| `DELETE /checks/<deleted id>` | 200 | `{"error":"Unable to find that check"}` |

**Effect:** a client that trusts the status code treats a rejected create as
a success with no ID, and a deleted check as an empty one.

**Provider:** a 2xx response whose body has `error` set is treated as an
error (`982cda9`).

## 2. A missing ID is never a 404, and the answer differs by type

| Request | Status | Body |
|---------|--------|------|
| `GET /checks/<deleted id>` | 200 | `{"error":"Error fetching check."}` |
| `GET /contacts/<deleted id>` | 200 | `{}` |
| `GET /contactgroups/<deleted id>` | 200 | `{}` |
| `DELETE /contacts/<deleted id>` | 200 | `{"error":"Unable to find that contact"}` |
| `DELETE /contactgroups/<deleted id>` | 200 | `{"error":"Unable to find group"}` |

A read of a missing contact or group returns an empty object with no error,
so a client cannot tell it apart from an empty result.

**Provider:** treats these answers as "possibly gone", then lists the type
and reports not-found only if the ID is missing from the list as well
(`1004af5`).

## 3. Misleading error when a contact's last address is removed

A `PUT /contacts/<id>` that would leave the contact with no addresses returns:

```
200 {"error":"Account must have at least one 'owner' contact."}
```

The request has nothing to do with owner contacts. A `POST /contacts` with
`"newaddresses": []` is also refused. The docs do not say that a contact
needs at least one address.

**Provider:** fails the plan before sending anything, and leaves
`newaddresses` out on create when there are none (`c624d2e`).

## 4. `queue` and `secure` change JSON type

- `queue` holds a string on an active check and `false` on a disabled one.
  The field is not documented. Typed as a string, decoding a disabled check
  fails, which breaks listing every check in an account that has one.
- `secure` is documented as `"false"`, `"ssl"` or `"starttls"`, but comes
  back as a bare boolean `false`.

**Provider:** decodes both as untyped values (`4ddd65f`, `9f8f50d`).

## 5. The web UI stores `warningdays: 0`

The docs call `warningdays` an "optional positive integer". Saving a check in
the web UI with the field empty stores `0`, even when the check was opened
and saved unchanged. A client that validates against the docs cannot
represent the stored value.

**Provider:** reads 0 as unset (`f557a0e`).

## 6. Headers stored as `null`

`sendheaders` and `receiveheaders` are documented as key:value pairs. Some
checks hold `null` values, for example `"sendheaders": {"Host": null}`.
Presumably this means "no such header", but the docs do not say so.

**Provider:** drops `null` entries when reading (`9ec0450`).

## 7. Older checks hold short-form notifications

The docs show notifications only in the object form:

```json
[{"<contact>": {"delay": 0, "schedule": "All"}}]
```

Older checks hold a short form, where the value is the schedule and there is
no delay:

```json
[{"<contact>": "All"}]
```

**Provider:** reads the string as the schedule, with a delay of 0 (`bcb6cc2`).

## 8. What an update does with an omitted field

The docs do not say whether `PUT` replaces a record or merges into it. This is
what we observed:

- **Check `tags`:** an update without `tags` keeps them, `[]` clears them,
  and `""` or `null` are ignored.
- **Contact `addresses`:** when the key is present it replaces the whole list,
  together with `newaddresses`. When the key is absent, every address is kept.
  The docs describe only the case where it is present.
- **Contact and contact group `name`:** nothing clears it. An update that
  leaves `name` out, or sends `""`, `null` or `false`, keeps it, and so does
  `0` for a group (not tried for a contact). `" "` is stored for a contact but
  ignored for a group. Every one of these answers 200 without an error.

**Provider:** always sends `tags` on an update, as `[]` when empty
(`f35cf3a`). It sends `addresses` whenever the plan has an address, as `{}`
when none are kept (`c624d2e`). It requires `name` on contacts and contact
groups, and refuses one that is empty or only whitespace at plan time.

## 9. An update is sometimes answered with the check as it was before

`PUT /checks/<id>` sometimes answers 200 with the check as it was **before**
the update: its old `modified` and its old values. The update is applied all
the same, and a `GET` a few seconds later shows it, with a later `modified`.
This happened to 1 in 33 full updates sent more than 4 s after the create, to
5 in about 180 updates sent 2 s apart, and to 19 in 540 in a later run, with
no pattern by spacing. A stale answer is the whole old check, never part of
it; the first `GET` a second later showed the update every time (the longest
lag seen was 2 s). Every fresh answer had a later `modified`, also for an
update that changed nothing.

**Effect:** a client that takes the answer as the result of its update sees
the old values. Terraform fails the apply with "Provider produced inconsistent
result after apply", although NodePing applied the change.

**Provider:** an answer whose `modified` is no later than the check's before
the update, and which does not already hold the planned values, is read again
after 1, 2, 4 and 8 s, until a read shows the update. If none does, the apply
fails with an error that names the check and says to plan again. Contacts and
contact groups gave no stale answer in 80 and 75 updates; they carry no
`modified` to tell one by.

## 10. A parameter the check type does not take is dropped without a word

A create or update of a check accepts every parameter, answers 200, and stores
only those its type takes: an HTTPADV check sent `servername`, which only SSL
checks take, answers without it and never holds it. Nothing in the answer says
so. We created and updated checks of all 33 types with every parameter, in
October 2026; create and update behaved alike. 25 parameters are stored only
by the types that take them, and those match the per-type lists in the docs,
except for:

- `statuscode` on DOHDOT: stored, and listed in the general section ("HTTPADV
  or DOHDOT"), but missing from DOHDOT's own list.
- `contentstring`, `invert`, `follow`, `port`, `verify` and `ipv6`: stored on
  every type, though the docs list them for a few. The docs describe which
  types use them.
- `verifyvolume` and `volumemin` (docs: AUDIO): stored by no type, AUDIO
  included. `autodiag` and `homeloc` (a Premiere feature) read back as
  `false` on every type, on the account we used.

**Effect:** a client that sends such a parameter sees no error, and finds it
missing on the next read.

**Provider:** fails the plan when it would send a parameter the planned check
type does not store, naming the parameter and the types that store it. The
per-type table and the rule are in the check resource's documentation.

## 11. A type change freezes the old type's parameters

When a check's type changes, NodePing keeps the parameters of the old type
and returns them, even those the new type does not take: an HTTP check that
used to be HTTPPARSE still holds its `fields`. No update of the new type
changes or clears them; a change sent with the new type is ignored. An update
that changes the type back to one that takes them changes them in the same
request.

**Effect:** a client cannot clear what a check holds from an earlier type
without changing the type back, and cannot tell from the check which of its
parameters are in use.

**Provider:** a value held this way passes the plan while the configuration
keeps it as it is. Adding or changing it fails the plan, as in #10. Removing it
replaces the check, with a warning in the plan.

## 12. A check created without `sens` holds none

The docs say `sens` "Defaults to 2". A `POST /checks` that leaves `sens` out
creates a check that holds no `sens` at all: its `parameters` held only
`target` and `threshold` when we tried it in October 2026. A check made in the
web UI holds `sens`.

**Effect:** a client that reads such a check sees no `sens`, and cannot tell
from it what the check runs with. The docs suggest 2, but we have not verified
that NodePing runs such a check with 2. The provider read it as unset, so no
configuration could import the check without a plan that wrote `sens`.

**Provider:** reads a missing `sens` as 2 in the check resource and the data
sources alike. That is the documented default and the value the resource plans
when the configuration leaves `sens` out, so an import of such a check plans
clean.

## 13. An invalid check value is refused, ignored or changed

The docs give `interval` as 0.25, 0.5 or an integer from 1, `sens` as a
positive integer and `threshold` as an integer from 1. A check update or
create with another value is refused, with an error that names the field or
with a generic one, or it is ignored, or truncated. A negative `sens` or
`threshold` is stored. We sent the provider's request bodies, which always
carry `type`, in October 2026:

| Value sent | Update | Create |
|------------|--------|--------|
| `interval` -1 | Refused: `Invalid check interval` | The same |
| `interval` 0.25 or 0.5 | Refused on our account: `Invalid check interval`. The docs allow both; they depend on the account's plan | The same |
| `interval` 0, 0.3 or `"abc"` | Ignored: the check keeps its interval | Ignored: the check gets 15 |
| `interval` 2.5 or 1.7 | Truncated to 2 or 1 | The same |
| `sens` 0 | Refused: `Unable to update check.` | Refused: `Unable to create check.` |
| `threshold` 0 | Refused: `Unable to update check.` | Stored as 5, the default |
| `sens` or `threshold` -1 | Stored | Stored |
| `sens` or `threshold` up to 1,000,000 | Stored: no upper bound found | Stored |
| `target` "not a url" (HTTP) | Refused: `target: Invalid URL` | The same |

- A refusal answers 200 with only `error` (#1). Nothing in the request is
  applied, the label included, and `modified` does not move. `Unable to update
  check.` does not name the field.
- An ignored or truncated value answers 200 with the check and no error. The
  rest of the request is applied, and `modified` moves as for any update. The
  answer is not a stale one (#9): the first `GET` agrees with it.
- `target` is checked only when the update carries `type`. A `PUT` of
  `{"target": "not a url"}` alone stores it as sent.

**Effect:** a client that trusts the status and the error takes an ignored or
truncated value as applied. Terraform fails the apply with "Provider produced
inconsistent result after apply", which names the attribute but not the
cause; after a create, the check exists all the same. A negative `sens` or
`threshold` is stored, although the docs ask for 1 or more.

**Provider:** the values NodePing is known to refuse, ignore or truncate are
refused at plan time, before anything is sent: `sens` and `threshold` below 1,
and an `interval` other than 0.25, 0.5 or a whole number of at least 1. Any
other value NodePing accepts but does not apply fails the apply with
Terraform's own "Provider produced inconsistent result after apply", which
names the attribute.

None of these values takes #9's stale-answer path, so none can end in its
advice that applying again is safe. A refused value fails the apply with
NodePing's error before any answer is mapped, and moves no `modified`. An
ignored or truncated value moves `modified` like any other update, so the
answer is not taken for a stale one. Every update in the probe behaved this
way.

## Raising it with NodePing

The API has carried version 1 in its URL since the start, and its changelog
records only additions (latest 2025-08-14). Other clients probably depend on
some of these behaviours, so the request should be split by risk:

1. **Documentation only, no risk:** #2's current answers, #4, #6, #7, #8,
   #11, what #12's missing `sens` means, and #10's per-type lists.
2. **Small fixes:** #3's message, #5 in the web UI, #9's stale answers, #12's
   default stored on create, a note in the answer when #10 drops a
   parameter, and an error naming the field for each of #13's invalid
   values.
3. **Breaking:** real status codes (#1) and 404s (#2). Ask whether they
   would offer these as an opt-in (a request header or query parameter) or in
   an `/api/2`, rather than changing `/api/1`. Note that #1 is also a docs
   mismatch: the overview already promises 400.
