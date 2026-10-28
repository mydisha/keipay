# KeiPay console design

The admin console is an operations tool for payments: calm, dense, and easy to
trust. Think of a payment provider's dashboard, not a landing page. This file
is the contract for every screen. When a feature needs something that is not
here, add it here first.

Code lives in `frontend/`. Tokens are in `frontend/src/app/styles.css`, shared
components in `frontend/src/shared/ui`, and every user-facing string in
`frontend/src/shared/lib/strings.ts`.

## Stack

React 19, Vite, TypeScript (strict), Tailwind v4, shadcn/ui (new-york, Radix,
CSS variables), lucide icons, IBM Plex Sans and IBM Plex Mono (self-hosted via
`@fontsource`, no Google Fonts request), react-router 7, TanStack Query and
Table, react-hook-form with zod, sonner, cmdk, openapi-fetch with types
generated from `api/openapi.yaml`. Do not add another UI or state library
without recording the reason in `docs/design.md`.

## Tokens

All colours are CSS variables in `oklch`, defined for light (`:root`) and dark
(`.dark`) and exposed to Tailwind through `@theme inline`. Components use the
Tailwind utilities (`bg-card`, `text-muted-foreground`, `border-danger-border`),
never raw hex, rgb or Tailwind palette colours such as `text-red-600`.

### Colour

| Token | Use |
|---|---|
| `background` / `foreground` | Page background and body text. |
| `card` / `card-foreground` | Tables, cards, inputs. |
| `popover` / `popover-foreground` | Dialogs, menus, tooltips. |
| `primary` / `primary-foreground` | The one main action per view; active filter chip. |
| `secondary`, `accent`, `muted` (+ `-foreground`) | Hover, quiet surfaces, helper text. `muted-foreground` is the lightest text allowed. |
| `destructive` / `destructive-foreground` | Buttons that rotate, revoke or delete. |
| `border`, `input`, `ring` | Dividers; form control borders (3:1 against `card`); focus ring. |
| `link` | Inline links. |
| `overlay` | The scrim behind dialogs and the mobile sheet. |
| `sidebar`, `sidebar-foreground`, `sidebar-accent`, `sidebar-accent-foreground`, `sidebar-border` | The navigation rail and mobile sheet. |

Status tones. Each has a tinted background, a text colour and a border:

| Tone | Tokens | Used for |
|---|---|---|
| success | `success`, `success-foreground`, `success-border` | `PAID`, `DELIVERED`, active (apps, users, API keys) |
| warning | `warning`, `warning-foreground`, `warning-border` | `PENDING`, expired API key |
| danger | `danger`, `danger-foreground`, `danger-border` | `FAILED`, revoked API key, errors, field messages |
| info | `info`, `info-foreground`, `info-border` | `SETTLED` |
| neutral | `neutral`, `neutral-foreground`, `neutral-border` | `EXPIRED` payment, disabled, inactive, anything unknown |

Test mode: `test-mode`, `test-mode-foreground`, `test-mode-border` (amber). Use
them only for the test-mode banner, the switch and the `Test` environment
badge, so "amber" always means test.

Every text/background pair above is at least 4.5:1 in both themes (body text is
about 17:1, status badges 6:1 or more), and control borders and the focus ring
are at least 3:1. Recheck contrast when you change a token.

### Typography

- Sans: IBM Plex Sans 400/500/600. Mono: IBM Plex Mono 400/500 for ids, keys,
  slugs, event names and JSON.
- Base size 14px (`text-sm`, line height 20px). Inputs use 16px below 768px so
  iOS does not zoom.
- Scale: page title `text-xl font-semibold` (one `h1` per page); section title
  `text-base font-semibold` (`h2`); body `text-sm`; helper and table headers
  `text-xs`. Nothing smaller than 12px.
- Numbers that line up (amounts, counts, attempts) use `tabular-nums`.

### Spacing, radius, shadow

- 4px grid through Tailwind spacing. Page gutter 16px on mobile, 24px from
  768px. Sections stack with `gap-6`; controls inside a section with `gap-3`.
- Radius from `--radius` (8px): `rounded-md` for controls, `rounded-lg` for
  cards and tables, `rounded-full` for badges and chips.
- Shadows stay flat: `shadow-xs` on cards and controls, `shadow-md` on menus,
  `shadow-lg` on dialogs and the mobile sheet.

## Components

Import from `@/shared/ui/*`. Prefer these over new markup.

| Component | Use it for |
|---|---|
| `Button` | Every action. Variants: `default` (one per view), `outline`, `ghost`, `secondary`, `destructive`, `link`. Sizes reach 44px below 768px. |
| `Input`, `Textarea`, `Select`, `Checkbox`, `RadioGroup`, `Switch`, `Label` | Form controls. Always with a visible label. `SelectItem` takes an optional one-line `description` (shown in the list, not in the trigger), used for the role picker. A `RadioGroup` is named by its field label (`aria-labelledby`) and each item has its own `Label`; use it for a short fixed choice such as the app environment. |
| `Form` (`FormField`, `FormItem`, `FormLabel`, `FormControl`, `FormDescription`, `FormMessage`) | Every form: react-hook-form + a zod schema that mirrors the contract. Server field errors go on their field; other failures in an `Alert` above the form. |
| `DataTable` | Every list. See the table pattern. |
| `StatusBadge` | Any status. Always text; the tone only reinforces it. |
| `MoneyText` | Every amount. |
| `IdText` | Every id, key, order id or reference. Mono, truncated, full value in `title`, copy button. |
| `DateTimeText` | Every timestamp. Relative text, absolute WIB time in a tooltip. |
| `EmptyState` | No data yet, or a coming-soon feature. Says what goes here and offers the next action. |
| `ErrorState` | A failed load, with a retry button. An API `403 forbidden` renders `ForbiddenState` instead, since retrying can't help. |
| `ForbiddenState` | A page or tab the signed-in user's role can't open: "You don't have access to this page" plus who to ask. Shown in place of the content, never as a redirect. |
| `Skeleton` | Loading placeholders that match the final layout. |
| `Dialog` | Short focused tasks: forms, details. |
| `ConfirmDialog` | Any mutation the user might regret. Pass `confirmText` when the action breaks a live integration at once (see Type-to-confirm). |
| `SecretRevealDialog` | Showing secrets once (a new app's three credentials, a new or rotated API key, a user's temporary password). One row per secret with its label and a copy button, a warning (overridable per use), and a required "I have saved this" checkbox before it closes. Escape and outside clicks do not dismiss it. |
| `JsonViewer` | Raw payloads and before/after snapshots. Collapsible, pretty-printed, copyable; long payloads are cut in the view but copied in full. |
| `Timeline` | Ordered events (payment lifecycle, delivery attempts) with time, title, a text status and an optional body. |
| `PageHeader` | The `h1`, one sentence of context, and page actions. |
| `Alert` | Inline messages inside forms and sections. |
| `Tabs` | Sibling views of one resource (Webhooks: Outbound, Inbound) that share a URL. |
| `LinkTabs` | Sibling views that each have their own URL (Settings: Account, Users, Audit log; an app's Overview, API keys, Webhooks, Routing): links styled as tabs inside a labelled `nav`, `aria-current="page"` on the active one. |
| `EnvironmentBadge` (`features/apps`) | An app's environment as text: `Test` in the amber test-mode tokens, `Live` in neutral card colours. Shown next to every app name (`AppLabel` in payments, webhooks and reconciliation rows). |
| toasts (`sonner`) | Outcome of an action the user just took: "Delivery 7 queued", "Couldn't replay". Never for load errors (use `ErrorState`). |

## Dense table pattern

- `DataTable` with column definitions (`DataColumn`): one column is the card
  title, action columns are `card: "actions"`.
- Filters sit above the table as chips (`filters`: single choice per group,
  clicking the active chip clears it), plus extra controls such as the app
  select in `toolbar`. "Clear filters" appears whenever any filter is set.
  When a group does not start empty (the Apps environment chip starts at the
  Test/Live mode), add an explicit "All" chip so every state is one click
  away.
- The app select (`AppFilterSelect`) filters by the app's public id and lists
  "Name (Test)" / "Name (Live)". It loads the newest 200 apps only.
- Cursor pagination, two forms:
  - Lists that grow and are read top-down (apps, API keys, payments) append
    pages with a "Load more" button: `loadMore={loadMoreList(query).loadMore}`
    over a `useInfiniteQuery` built with `cursorPageOptions`. A failed next
    page keeps the rows and shows a toast; only a failed first page shows
    `ErrorState`.
  - Lists browsed page by page (users, audit log) pass
    `pagination={{ nextCursor, onPageChange }}`. The table keeps the cursor
    stack for Previous/Next, disables them at either end, and returns to page
    1 when a chip changes or `resetKey` changes.
  Neither shows a total. Older offset lists (ledger, deliveries) load one page.
- Status is a `StatusBadge`; ids use `IdText`; amounts use `MoneyText`,
  right-aligned with `tabular-nums` in a right-aligned column; times use
  `DateTimeText`.
- Rows are 40px tall, headers `text-xs text-muted-foreground`. Long text
  truncates with the full value in `title`.
- Below 768px the same rows render as cards: title on top, the other fields as
  label/value pairs, actions at the bottom. Never let a table scroll the page
  sideways.
- States: skeleton rows (or cards) while loading; `ErrorState` with retry on
  failure; an `EmptyState` with guidance and an action when empty, and a
  "Clear filters" action when the emptiness comes from filters.

## Detail page pattern

Used from phase 2 on (app, payment, connection details):

1. A "Back to …" link to the list.
2. Header: the resource name as the page `h1` (or its id in `IdText` when it
   has no name), then its badges as text (environment, status), then key
   facts as a definition list (slug in mono, the public id in `IdText`).
   Write actions sit on the right and are hidden without the permission.
3. When the resource has several views, `LinkTabs` under the header, one
   route per tab (`/apps/:id`, `/apps/:id/keys`, …); the index route is
   Overview. Each tab has an `h2`.
4. Overview: a card with the facts as label/value rows (a missing value says
   "Not set"), then raw data in a `JsonViewer` ("No metadata" when empty).
5. Where events exist: a timeline of what happened, newest last, using
   `Timeline`, and raw payloads in collapsed `JsonViewer`s.

States: a header-shaped skeleton while loading; an unknown or malformed id
(`404 not_found` or `400 invalid_request`) shows "App not found" with a
"Back to apps" action, not an error; other failures show `ErrorState` with
retry, and `403 forbidden` shows `ForbiddenState`. A tab for a later phase is
an `EmptyState` that says what will live there and makes no API call.

Dialogs (such as an audit entry) follow the same order on a smaller scale.

## Apps and API keys

- An app's environment is fixed at creation and always shown as text with
  `EnvironmentBadge`. The create dialog defaults it to the current Test/Live
  mode, suggests the slug from the name until the user edits the slug, and
  says that neither can change later.
- Metadata is edited as JSON text in a `Textarea` (mono). The zod rule
  mirrors the API: a flat object of at most 50 keys (1-40 characters, no
  control characters), values a string of at most 500 characters, a number, a
  boolean or null, at most 8192 bytes. Lengths count code points. Empty text
  means `{}`. The typed text is sent as written so large numbers keep every
  digit.
- Creating an app reveals three secrets once: API key, API secret and webhook
  signing secret. Closing the reveal opens the new app.
- API keys are shown as `{prefix}…{last4}` in mono (`kp_test_…a1b2`, `kp_…0001`
  for migrated keys), never in full after the reveal. Status labels:
  **Active**, **Expired**, **Revoked** (see the tones above). "Last used" and
  "Expires" say "Never" when empty.
- Rotate is offered on active keys only and is a plain confirm, because the
  old key keeps working; its reveal tells the user to revoke the old key
  after deploying the new one. Revoke is offered on any key that is not
  revoked yet.
- The 10-usable-keys limit (`400 validation_failed`) appears as a toast with
  the server's message.
- Expiry is picked with a `datetime-local` input read as WIB (Asia/Jakarta)
  and must be in the future.

## Type-to-confirm

A `ConfirmDialog` with `confirmText` keeps its confirm button disabled until
the exact text (case-sensitive) is typed. Use it when the action breaks a
live integration at once:

| Action | Type |
|---|---|
| Disable an app | the app's slug |
| Revoke an API key | the key's label |
| Disable a user, reset a password | the user's email |

Undoable or non-breaking actions (enable an app or user, rotate a key) use a
plain confirm. The dialog's text says what fails and with which error code
(`tenant_inactive`, `api_key_revoked`).

## States

- Loading: skeletons shaped like the content. Buttons that submit show a
  spinner and a verb ("Saving…") and the form's inputs are disabled.
- Empty: say what will appear and how to get it, with an action button
  ("Create app", "View payments"). Coming-soon features use the same component
  and name the feature.
- Error: `ErrorState` with "Try again". Messages come from the error envelope
  `code`; `internal_error`, `network_error` and `unknown_error` get friendly
  copy and never show internal text.
- Session: the session is an `HttpOnly` cookie the console never reads; nothing
  about it goes in `localStorage` or `sessionStorage`. A `401 session_expired`
  from any console call clears every cached query and sends the user to
  `/login?reason=session_expired`, which says "Your session has expired. Sign
  in again." A `401 session_required` goes to `/login` without the message. A
  `403 password_change_required` goes to `/change-password`.
- Boot: protected pages show a full-page skeleton of the shell until the
  signed-in user (`GET /admin/me`) has loaded, so the console never flashes
  for a signed-out visitor. A failure other than 401 shows a full-page
  `ErrorState` with retry.

## Roles and permissions

- The UI reads what the signed-in user may do from `me.permissions`
  (`usePermission`, `<Can>`, `<RequirePermission>`), never from the role
  name. The server enforces every permission anyway.
- Actions the role can't perform are hidden, not disabled: no Create app,
  Edit, Disable, Enable, Create key, Rotate or Revoke without `apps:write`, no
  Replay without `deliveries:replay`. Pages stay readable.
- Pages and tabs the role can't open are hidden from navigation; opening
  their URL shows `ForbiddenState` in place of the content (no redirect).
- The role is shown as a text label: Owner, Admin, Viewer. Role pickers give
  each role a one-line description of what it allows.

## Auth pages

Setup, sign-in and change password share `AuthLayout`: the brand, an `h1`,
one sentence, and a single card with the form. A skeleton of that layout
shows while the page's guard decides where the visitor belongs.

| Page | Fields | States |
|---|---|---|
| First-run setup `/setup` | Name, Email, Password, Confirm password; Setup token only when the server requires one | Submit spinner with inputs disabled; field errors on their fields (wrong setup token on the token field); "already set up" toast and go to sign-in; rate limit alert in minutes |
| Sign in `/login` | Email, Password | "Incorrect email or password." for any credential failure; "Too many failed attempts. Try again in N minutes."; warning banner when `?reason=session_expired` |
| Change password | Current password, New password, Confirm new password | Wrong current password on its field; a reused password on New password; success toast and back to the console |

- `/login` and `/setup` send a signed-in user to the console and send each
  other's visitors across: `/login` goes to `/setup` while setup is
  required, `/setup` goes to `/login` once it is done.
- New passwords: at least 12 characters and at most 72 bytes, shown as a
  hint under the field; never trimmed.
- Forced password change: after an owner creates a user or resets their
  password, the console is locked until the user sets their own. The page
  says "Set a new password" and that an owner set a temporary password,
  offers "Sign out" instead of "Back to the console", and every other URL
  leads back to it.

## Users

`/settings/users` (owners, `users:manage`) follows the dense table pattern:
name and email (your own row marked "You"), role as text, status badge
(Active or Disabled) with "Password change pending" as secondary text, last
sign-in ("Never" when empty) and created. Add user and Reset password reveal
the temporary password once in `SecretRevealDialog`. Disable and Reset
password are destructive confirms that need the user's email typed exactly;
Enable is a plain confirm. Your own row has no Disable or Reset password.

## Mobile

- Works from 375px wide with a 16px gutter and no horizontal page scroll.
- The sidebar becomes a sheet opened from the menu button in the top bar.
- Touch targets are at least 44px below 768px (buttons, chips, menu items,
  nav links).
- Tables become cards (see above). Dialogs fit the viewport and scroll inside.

## App shell

- Collapsible sidebar (remembered per browser): Dashboard, Apps, Connections,
  Webhooks, Payments, Reconciliation, Settings.
- Top bar: command palette button (⌘K / Ctrl+K), Test/Live switch, theme menu
  (light, dark, system), account menu (name, email and role label at the top;
  account settings, change password, sign out). Sign out calls
  `POST /admin/logout`, clears every cached query and goes to `/login`; if it
  fails the user stays signed in and sees a toast.
- Test mode shows an amber banner on every page. The switch sets the Apps
  list's starting environment filter (and resets it whenever the switch
  changes) and the create-app default. Payments, webhooks and reconciliation
  do not follow it yet, and the banner says so instead of claiming the data is
  filtered.
- The palette navigates to every page (plus Users with `users:manage`),
  switches theme and mode, and says that searching by id or order arrives
  later.

## Dark mode

The `dark` class on `<html>` swaps every token; components need no `dark:`
variants. The theme is light, dark or system (default), stored in
`localStorage` and applied before the first render. Check every new screen in
both themes.

## Formats

All formatting goes through `frontend/src/shared/lib/format.ts`.

- Money: amounts are integers in minor units with an ISO 4217 code. IDR uses
  exponent 0 (whole rupiah), so `150000 IDR` is `Rp150.000`: no space, `.` for
  thousands, `-Rp2.500` for negatives, `Rp0` for zero. Other currencies use
  their ISO exponent through `Intl.NumberFormat("id-ID")` (`1050 USD` is
  `US$10,50`).
- Numbers: `id-ID` grouping (`1.234`).
- Dates: stored and sent in UTC (RFC 3339), shown in Asia/Jakarta with `id-ID`
  formatting and a `WIB` suffix: `2 Jan 2026, 01.30.00 WIB`. Tables show the
  relative time ("5 minutes ago") with the absolute time in a tooltip and in
  `<time dateTime>`.
- Ids: public ids (`app_…`, `pay_…`, `key_…`, `usr_…`) look like
  `pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C` and are always mono with copy. Numeric ids
  from older endpoints (deliveries, ledger entries) show as `#12`.
- Missing values show `—`.

## Copy

- Sentence case, short and specific: "Rotate credentials", not "Rotate API
  Credentials Now".
- English today; every string lives in `strings.ts` so another language can be
  added by swapping that object.
- Say what happened and what to do next: "Couldn't replay the delivery." plus a
  retry, not "Error".
- The UI and the API both say "app". The only leftover of "tenant" is the
  error code `tenant_inactive` (a disabled app), which is quoted as is.

## Accessibility

- Every input has a visible label; icon-only buttons have an `aria-label`.
- Focus is always visible (`focus-visible` ring from the `ring` token).
- Text meets WCAG AA (4.5:1) in both themes; control borders and focus rings
  3:1.
- Status is never colour alone: badges carry text, timeline items carry a
  status label, errors carry a message.
- Dialogs trap focus and have a title and description; destructive confirms
  need typed confirmation.
- One `h1` per page, sections use `h2`.
