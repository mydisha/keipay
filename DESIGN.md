# KeiPay console design

This file is the contract for every screen. When a feature needs something
that is not here, add it here first.

## Direction

- **What it is:** an operations console for payments. It is calm, dense and
  easy to trust, like a payment provider's dashboard, not a landing page. The
  pages are clean and tidy, with the same structure everywhere.
- **Visual language:** the Kei ledger family. Ivory
  paper and a pine-black ink for text and buttons. Pine is a thin signal for
  links, focus, the active page and the outcome that matters in a chart.
  Lists and figures are split by 1px rules like ledger columns, with at most
  one surface per region and never a card inside a card.
- **Identity motifs:** ruled hairlines between rows and figures, and payment
  references set in mono like the lines of a receipt.
- **Logo:** the name "KeiPay" as a text wordmark.
- **Missing values** are written out for what they mean ("Not set", "Never",
  "Not yet", "Unknown", "None"), never a dash.
- **Pace:** console and auth pages are quiet. People come to read numbers
  and act, so layouts are predictable, motion is only hover and focus
  feedback, and an auth page has one task with nothing competing with its
  form.

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

The palette is the Kei ledger palette: neutrals are ivory (hue 95) in
light and charcoal with a pine cast (hue 165) in dark, never pure white or
black.

| Token | Use |
|---|---|
| `background` / `foreground` | The paper and the ink. |
| `card` / `card-foreground` | Surfaces: tables, cards, inputs. |
| `popover` / `popover-foreground` | Dialogs, menus, tooltips. |
| `primary` / `primary-foreground` | Every button: ink with paper text in light, pale mint with ink text in dark. The screen's main action gets the strongest position, not another colour. |
| `brand`, `brand-soft` | The pine signal: the active filter chip, the active tab underline, the successful series in charts. Never a fill larger than a chip, never decoration. |
| `secondary`, `accent`, `muted` (+ `-foreground`) | Hover, quiet surfaces, helper text. `muted-foreground` is the lightest text allowed. |
| `destructive` / `destructive-foreground` | Buttons that rotate, revoke or delete. |
| `border`, `input`, `ring` | Hairlines; form control borders (3:1 against `card`); the pine focus ring. |
| `link` | Inline links, in pine. |
| `overlay` | The scrim behind dialogs and the mobile sheet. |
| `sidebar`, `sidebar-foreground`, `sidebar-accent`, `sidebar-accent-foreground`, `sidebar-border` | The desk: the navigation rail, the mobile sheet and the background of the auth pages. |
| `sidebar-primary` | The icon of the active navigation item. |
| `chart-1` … `chart-5` | Chart series, in this order: pine (successful payments), brass, slate (payments created), terracotta, plum. Never used outside charts. |

Status tones are ledger stamps. Each has a soft tint, an ink and a border:

| Tone | Tokens | Used for |
|---|---|---|
| success | `success`, `success-foreground`, `success-border` | `PAID`, `DELIVERED`, active (apps, users, API keys, connections), connection test `Passed` |
| warning | `warning`, `warning-foreground`, `warning-border` | `PENDING`, expired API key, "no default connection" notice |
| danger | `danger`, `danger-foreground`, `danger-border` | `FAILED`, revoked API key, connection test `Failed`, errors, field messages |
| info | `info`, `info-foreground`, `info-border` | `SETTLED`, the `Legacy URL` badge of the imported connection |
| neutral | `neutral`, `neutral-foreground`, `neutral-border` | `EXPIRED` payment, disabled, inactive, connection `Not tested`, anything unknown |

Test mode: `test-mode`, `test-mode-foreground`, `test-mode-border` (amber). Use
them only for the test-mode banner and the `Test` environment badge. The
switch in the top bar turns its label and track `warning-foreground` (the
darker amber, 3:1 against the bar) while test mode is on, so "amber" always
means test.

Every text/background pair is at least 4.5:1 in both themes and every
control border, focus ring and chart colour at least 3:1 against the card.
Measured with the WCAG 2.x formula on the current tokens (light / dark):

| Pair | Light | Dark |
|---|---|---|
| Body text on card | 17.7 | 16.1 |
| Muted text on card | 6.7 | 8.1 |
| Button label on button | 17.2 | 15.3 |
| Links on card | 7.8 | 10.6 |
| Pine on its soft tint (active chip) | 7.5 | 8.4 |
| Active nav icon on the sidebar | 7.2 | 11.6 |
| Status stamps, lowest | 5.5 | 6.4 |
| Test mode | 8.9 | 10.0 |
| Input border on card | 3.9 | 4.5 |
| Focus ring on card | 5.2 | 8.6 |
| Chart series on card, lowest | 3.7 | 6.9 |

Recheck contrast when you change a token.

### Typography

- Sans: IBM Plex Sans 400/500/600: an industrial grotesk
  that reads like printed statements. Mono: IBM Plex Mono 400/500 for ids,
  keys, slugs, event names and JSON, never for headings or prose.
- Base size 14px (`text-sm`, line height 20px). Inputs use 16px below 768px so
  iOS does not zoom.
- Scale: page title 24px, 28px from 768px, weight 500 with tight tracking (one
  `h1` per page); section and card titles 15px weight 500; a figure 24px to
  28px weight 500 `tabular-nums`; body `text-sm`; helper text, eyebrows and
  table headers `text-xs`. Nothing smaller than 12px.
- Sentence case everywhere. No uppercase labels with wide tracking: an
  eyebrow is plain muted text.
- Numbers that line up (amounts, counts, attempts) use `tabular-nums`.

### Spacing, radius, shadow

- 4px grid through Tailwind spacing. Page gutter 16px on mobile, 24px from
  768px, 32px from 1024px; 32px above the content (40px from 768px). Regions
  of a page stack 32px apart; controls inside a section 12px; side-by-side
  surfaces 24px.
- Radius from `--radius` (8px) grows with the object: `rounded-sm` (4px) for
  status stamps, chips and keyboard hints, `rounded-md` (6px) for buttons,
  inputs and menu items, `rounded-lg` (8px) for popovers, `rounded-xl` (12px)
  for surfaces (cards, tables, dialogs, empty states). Only avatars and
  switches are round.
- Card padding comes from the card itself: header and footer `px-4 py-3`
  (`px-5 py-4` from 768px), content `p-4` (`p-5` from 768px), and `p-4`
  everywhere on a compact card. Pass `className="p-0"` to `CardContent` when
  a table or a list fills the card. Never set padding on `Card`.
- Two elevations only: `shadow-xs` is a 1px paper lip on surfaces and
  controls, `shadow-md`/`shadow-lg` lift menus, dialogs and the mobile sheet.
  No gradients, glows, glass or grid backgrounds anywhere.

## Components

Import from `@/shared/ui/*`. Prefer these over new markup.

| Component | Use it for |
|---|---|
| `Button` | Every action. Variants: `default` (one per view), `outline`, `ghost`, `secondary`, `destructive`, `link`. Sizes reach 44px below 768px. |
| `PasswordInput` | Passwords and other secrets: masked, with a show/hide toggle whose accessible name can be overridden (`Show value: Secret API key`). |
| `Input`, `Textarea`, `Select`, `Checkbox`, `RadioGroup`, `Switch`, `Label` | Form controls. Always with a visible label. `SelectItem` takes an optional one-line `description` (shown in the list, not in the trigger), used for the role picker. A `RadioGroup` is named by its field label (`aria-labelledby`) and each item has its own `Label`; use it for a short fixed choice such as the app environment. |
| `Form` (`FormField`, `FormItem`, `FormLabel`, `FormControl`, `FormDescription`, `FormMessage`) | Every form: react-hook-form + a zod schema that mirrors the contract. Server field errors go on their field; other failures in an `Alert` above the form. |
| `DataTable` | Every list. See the table pattern. |
| `StatusBadge` | Any status, as a ledger stamp: the label on its soft tint, 4px corners, no dot. Always text; the tone only reinforces it. Optional `label` and `tone` override the shared label and tone for a status only one feature knows (an inbound webhook's result and signature). Only real states, never "New" or "Beta". |
| `Chip` | A filter or a single choice (`aria-pressed`): a dashed outline while off, the pine signal while on. |
| `Brand` | The text wordmark "KeiPay" (just "K" in the collapsed sidebar, with the full name for screen readers). |
| `MoneyText` | Every amount. |
| `IdText` | Every id, key, order id or reference. Mono, truncated, full value in `title`, copy button. |
| `DateTimeText` | Every timestamp. Relative text, absolute WIB time in a tooltip. |
| `EmptyState` | No data yet, or a coming-soon feature: a short heading, one sentence on what appears here and why it is empty, and the one action that fills it. No icon. `variant="inline"` inside a card or panel that already is a surface. |
| `ErrorState` | A failed load, with a retry button. An API `403 forbidden` renders `ForbiddenState` instead, since retrying can't help. |
| `ForbiddenState` | A page or tab the signed-in user's role can't open: "You don't have access to this page" plus who to ask. Shown in place of the content, never as a redirect. |
| `Skeleton` | Loading placeholders that match the final layout. |
| `Dialog` | Short focused tasks: forms, details. |
| `SidePanel` | A record's detail or a longer form that slides in from the right over its list (delivery, inbound webhook, webhook endpoint): full width on phones, 36rem from 640px; the body scrolls, the header and an optional `footer` (form buttons) stay put; `locked` blocks closing while a request is in flight. The list stays behind it and the open record is in the URL. |
| `ConfirmDialog` | Any mutation the user might regret. Pass `confirmText` when the action breaks a live integration at once (see Type-to-confirm). Optional `children` adds inputs above the type-to-confirm field (the grace period when rotating an endpoint secret); `canConfirm={false}` keeps confirm disabled while those inputs are invalid. |
| `SecretRevealDialog` | Showing secrets once (a new app's API key and secret, a new or rotated webhook endpoint secret, a new or rotated API key, a user's temporary password). One row per secret with its label and a copy button, a warning (overridable per use), and a required "I have saved this" checkbox before it closes. Escape and outside clicks do not dismiss it. |
| `JsonViewer` | Raw payloads and before/after snapshots. Collapsible, pretty-printed, copyable; long payloads are cut in the view but copied in full. |
| `Timeline` | Ordered events (payment lifecycle, delivery attempts) with time, title, a text status and an optional body. |
| `PageHeader` | The `h1`, one sentence of context, page actions, an optional eyebrow above the title, and `children` for the row under it (range chips, a note). |
| `Card` (`CardHeader`, `CardTitle`, `CardDescription`, `CardAction`, `CardContent`, `CardFooter`) | Every surface, at most one per region. Split what is inside with hairlines (`divide-y`, `border-t`), never a card or bordered box inside a card. `CardHeader` carries the title (`h2`, or `h3` with `as`), an optional sentence only when it adds a fact, and an action on the right; `CardContent` the body; `CardFooter` a closing row such as a form's buttons. `size="compact"` for a small block of facts beside a header (a run's figures). |
| `Section` / `SectionHeader` | The only way to write an `h2` (or `h3`) with its sentence and actions: `<section aria-labelledby={id}>` with `gap-3` to its content. Pass the heading id other code relies on (`audit-log-heading`). |
| `DetailHeader` | The header of every detail page: `BackLink`, an optional eyebrow, the title (`titleMono` for an id), badges, one sentence, inline facts, and the write actions on the right. `DetailHeaderSkeleton` while loading. |
| `BackLink` | "Back to …" above a detail page or a wizard. |
| `FactList` / `Fact` | Label/value pairs as a definition list: `rows` (label column, detail cards), `inline` (under a title), `tiles` (a grid of small facts). `mono` for ids and slugs; a missing value says "Not set". |
| `StatStrip` / `Stat` | Figures that belong together as one surface split by hairlines, not separate cards: label, value in tabular figures, then the change as text and one line of context. A value that is a word for missing data ("Not yet") is set with `empty` so it never reads as a figure. `StatStripSkeleton` while loading. |
| `Chart` (`ChartContainer`, `ChartTooltip`, `ChartLegend`, `ChartLegendContent`) | recharts wrapped so series colours only come from `chart-1` … `chart-5`. A chart writes its own tooltip content for what it shows. Every chart has an `aria-label` and an `sr-only` table of its rows. |
| `Alert` | Inline messages inside forms and sections. |
| `Tabs` | Sibling views of one resource (Webhooks: Outbound, Inbound) that share a URL. Underline style; 24px to the content. |
| `LinkTabs` | Sibling views that each have their own URL (Settings: Account, Users, Audit log; an app's Overview, API keys, Webhooks, Routing): links styled as the same underline tabs inside a labelled `nav`, `aria-current="page"` on the active one. |
| `EnvironmentBadge` (`features/apps`) | An app's or a connection's environment as text: `Test` in the amber test-mode tokens, `Live` in neutral card colours. Shown next to every app name (`AppLabel` in payments, webhooks and reconciliation rows) and every connection name. |
| `SchemaFields` (`features/connections`) | Every provider form field, rendered from the `SchemaField` list the server sends. See Connections. |
| `WizardStepper` (`features/connections`) | The numbered steps of a multi-step page. See Wizard pattern. |
| toasts (`sonner`) | Outcome of an action the user just took: "Delivery queued for one more attempt", "Couldn't replay". Never for load errors (use `ErrorState`). Popover paper with `shadow-lift`, IBM Plex, a 16px lucide icon tinted by type (success, danger, warning; info in muted ink), the close button inside the top right corner. Bottom right; full width with a 16px gutter on a phone. |

## Dense table pattern

- `DataTable` with column definitions (`DataColumn`): one column is the card
  title, action columns are `card: "actions"`.
- Filters sit above the table as chips (`filters`: single choice per group,
  clicking the active chip clears it), plus extra controls such as the app
  select in `toolbar`. "Clear filters" appears whenever any filter is set.
  When a group does not start empty (the Apps and Webhooks environment
  chips start at the Test/Live mode), add an explicit "All" chip so every
  state is one click away.
- The app select (`AppFilterSelect`) filters by the app's public id and lists
  "Name (Test)" / "Name (Live)". It loads the newest 200 apps only.
- Cursor pagination, two forms:
  - Lists that grow and are read top-down (apps, API keys, payments,
    webhook deliveries, inbound webhooks) append pages with a "Load more" button: `loadMore={loadMoreList(query).loadMore}`
    over a `useInfiniteQuery` built with `cursorPageOptions`. A failed next
    page keeps the rows and shows a toast; only a failed first page shows
    `ErrorState`.
  - Lists browsed page by page (users, audit log) pass
    `pagination={{ nextCursor, onPageChange }}`. The table keeps the cursor
    stack for Previous/Next, disables them at either end, and returns to page
    1 when a chip changes or `resetKey` changes.
  Neither shows a total. The older offset list (ledger) loads one page.
- Status is a `StatusBadge`; ids use `IdText`; amounts use `MoneyText`,
  right-aligned with `tabular-nums` in a right-aligned column; times use
  `DateTimeText`.
- The table is one surface. Rows are 44px tall and ruled by hairlines, with
  no zebra or tinted header; headers are `text-xs text-muted-foreground`.
  Long text truncates with the full value in `title`, and id columns stop
  at 208px so a list fits a 1280px screen without scrolling.
- Below 768px the same rows render as cards inside one surface split by
  hairlines: title on top, the other fields as label/value pairs, actions at
  the bottom. Never let a table scroll the page sideways; a wide table
  scrolls inside its own surface.
- States: skeleton rows (or cards) while loading; `ErrorState` with retry on
  failure; an `EmptyState` with guidance and an action when empty, and a
  "Clear filters" action when the emptiness comes from filters.

## Detail page pattern

Used by every resource detail page (app, connection, reconciliation run, and
payment once it has one):

1. A `BackLink` to the list.
2. `DetailHeader`: the resource name as the page `h1` (or its id in mono when
   it has no name), then its badges as text (environment, status), then key
   facts as an inline `FactList` (slug in mono, the public id in `IdText`).
   Write actions sit on the right and are hidden without the permission.
3. When the resource has several views, `LinkTabs` under the header, one
   route per tab (`/apps/:id`, `/apps/:id/keys`, …); the index route is
   Overview. Each tab has an `h2`.
4. Overview: a `Section` per topic, each a `Card` with the facts as a
   `FactList` of rows (a missing value says "Not set"), then raw data in a
   `JsonViewer` ("No metadata" when empty).
5. Where events exist: a timeline of what happened, newest last, using
   `Timeline`, and raw payloads in collapsed `JsonViewer`s.

States: a header-shaped skeleton while loading; an unknown or malformed id
(`404 not_found` or `400 invalid_request`) shows "App not found" with a
"Back to apps" action, not an error; other failures show `ErrorState` with
retry, and `403 forbidden` shows `ForbiddenState`. A tab for a feature that
is not built yet is an `EmptyState` that says what will live there and makes
no API call.

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
- Creating an app reveals two secrets once: API key and API secret. Webhook
  signing secrets belong to each webhook endpoint (an app's Webhooks tab) and
  are revealed when the endpoint is created or its secret rotated. Closing the
  reveal opens the new app.
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
| Disable a connection | the connection's name |
| Rotate a webhook endpoint secret, delete an endpoint | the endpoint URL's host |

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

## Wizard pattern

Used when a task has steps that each need room (adding a connection). A
wizard is a page, never a dialog, so it works at 375px and survives a reload.

- `WizardStepper` above one card: numbered steps with text labels in an
  `ol` inside a `nav` named "Steps"; the current step has
  `aria-current="step"`, finished steps show a check, and each item carries
  "Step N of M:" for screen readers. Steps stack vertically below 768px.
- The card has the step's `h2`, one sentence, the content, and the actions
  bottom-right: the main action last (`Next`, `Create and test`,
  `Continue`), `Back` before it, and `Skip for now` (ghost) on optional steps.
- Steps before the resource exists share one form, so Back keeps what was
  typed, and the server's field errors send the user back to the step that
  has the field. Once the resource exists, the URL carries it and the step
  (`?connection=con_…&step=3`), so a reload resumes there; the steps after
  creation are optional and every one of them can be done later on the
  detail page.

## Connections

- Every credential and settings form is rendered by `SchemaFields` from the
  provider's schema (`GET /admin/providers`); the console has no code per
  gateway. `string` is an `Input` (a secret field is a `PasswordInput` with
  `autocomplete="off"` and no spellcheck), `integer` a numeric text input sent
  as a number (blank means "not set"), `boolean` a `Switch`, and `enum` a
  `Select` with a `Default` choice when the field is optional. Labels,
  descriptions and placeholders come from the schema; required fields carry a
  `*` and `aria-required`.
- Client rules mirror the server (required, length in code points, pattern
  with `pattern_hint` as the message and never the regex, minimum, maximum,
  options). Server errors `credentials.<key>` and `settings.<key>` land on
  their field. A field type the console doesn't know shows "This provider
  needs a newer console." and disables submit.
- Credentials are write-only. The detail page shows each field's label and
  `•••• last4` (or `Stored` when the value is too short to hint), never a
  value. Replacing them uses the same fields in replace mode: empty fields
  keep the stored value, the hint is the placeholder, at least one value is
  required, and only the filled keys are sent.
- Settings sit under a collapsible "Advanced settings" in the wizard and in
  the Details form on the detail page, which sends only what changed.
- The callback URL is mono with a copy button, followed by the provider's
  numbered instructions and dashboard link. The imported connection also
  says it receives callbacks at `/webhooks/xendit` (legacy).
- The last test is a status badge (`Passed`, `Failed`, `Not tested`) with
  the relative time; a failure shows its message. A failed test never blocks
  the wizard; it says checkouts may fail until the test passes.
- An app's default connection is chosen on its Overview tab from the active
  connections of the app's environment, plus `None`. Viewers see a link or
  "Not set". Without one, a warning says payments fail until it is set.
- Disabling a connection that an app uses as its default is refused: a
  dialog lists those apps as links and how many there are.

## Roles and permissions

- The UI reads what the signed-in user may do from `me.permissions`
  (`usePermission`, `<Can>`, `<RequirePermission>`), never from the role
  name. The server enforces every permission anyway.
- Actions the role can't perform are hidden, not disabled: no Create app,
  Edit, Disable, Enable, Create key, Rotate or Revoke without `apps:write`, no
  Add connection, Test connection, Replace credentials, Disable or Enable
  without `connections:write`, no Replay without `deliveries:replay`. Pages
  stay readable; forms a viewer can't submit render disabled.
- Pages and tabs the role can't open are hidden from navigation; opening
  their URL shows `ForbiddenState` in place of the content (no redirect).
- The role is shown as a text label: Owner, Admin, Viewer. Role pickers give
  each role a one-line description of what it allows.

## Auth pages

Setup, sign-in and change password share `AuthLayout`: one sheet of paper on
the desk (`sidebar` tone, 12px inset from `sm` up; the whole screen on a
phone). Along its top edge sit the wordmark and the theme menu; in the middle,
384px wide, the centred `h1`, one sentence and the form. Nothing else
competes with the form: no brand panel, tagline, illustration or background
pattern. A skeleton of that layout shows while the page's guard decides where
the visitor belongs.

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
- The sidebar becomes a sheet opened from the "Menu" button in the top bar.
  Below 640px the switch is labelled "Test" (its name stays "Test mode") so
  the top bar fits a 360px phone.
- Touch targets are at least 44px below 768px (buttons, chips, menu items,
  nav links).
- Tables become cards (see above). Dialogs fit the viewport and scroll inside.
- Every grid that holds a chart, a table or long mono text declares its
  columns with `grid-cols-1` or `minmax(0, …)` at every breakpoint, so its
  content can shrink. Check new pages at 360px, 768px and 1280px with real
  data, not only the empty state.

## App shell

- Collapsible sidebar on the desk tone (remembered per browser) with the
  wordmark at the top, the working pages (Dashboard, Apps, Connections,
  Webhooks, Payments, Reconciliation) in one list, and Settings apart at the
  bottom. No group labels. The active link sits on a darker desk tint in ink
  with its icon in pine. Collapsed, every link has a tooltip.
- Top bar: command palette button (⌘K / Ctrl+K), the Test/Live switch (its
  label and track turn amber while test mode is on; no box around it), theme menu (light,
  dark, system), account menu behind the initials (name, email and role label
  at the top; account settings, change password, sign out). Sign out calls
  `POST /admin/logout`, clears every cached query and goes to `/login`; if it
  fails the user stays signed in and sees a toast.
- Test mode shows one amber line under the top bar on every page: "Test mode",
  a hairline, then which screens follow the switch and that Payments still
  show every environment (only the Payments sentence on a phone). The switch sets the
  starting environment filter of the Apps and Connections lists (and resets
  it whenever the switch changes) and the environment default of the
  create-app dialog and the add-connection wizard. The Webhooks Outbound and
  Inbound lists start at the mode too (`environment` chip with an explicit
  "All"; choosing All puts `environment=all` in the URL, no `environment`
  means "follow the switch"). The dashboard and reconciliation follow it too.
  Payments do not follow it yet, and the banner says so instead of claiming
  that data is filtered.
- The palette navigates to every page (plus Users with `users:manage`),
  switches theme and mode, and opens a pasted webhook record id: `dlv_` and
  `inb_` open that delivery or inbound webhook on the Webhooks page, `evt_`
  shows the event's deliveries, `whe_` opens the endpoint's app Webhooks tab
  (each only with the permission to read it). It says that searching
  payments and orders arrives later.

## Dashboard

The screen answers two questions: is money coming in through the gateways,
and is anything broken that someone has to fix. It is built around those,
not around a row of stat cards.

`/` reads `GET /admin/dashboard` once per range and environment (30 seconds
stale time). The environment is the Test/Live switch; the range is
`Last 7 days` or `Last 30 days` as chips in the page header, kept in the URL
(`?range=30d`). The header's one sentence names the scope and the comparison
once ("Test apps and connections over the last 7 days. Changes compare with
the 7 days before."). The page is a `RequirePermission` on `payments:read`.

1. One payments surface. On top a `StatStrip`: gross volume of successful
   payments, payments created, success rate (successful over successful,
   expired and failed) and delivery success (delivered over delivered and
   failed). A figure shows its change only when there is a real one ("+12,5%",
   percentage points for the rate); nothing when the previous period was
   empty or equal. Delivery success has no previous period, so it says how
   many finished deliveries were delivered. Under a hairline, payments
   created (slate, `chart-3`) and successful (pine, `chart-1`) per WIB day,
   with the gross per currency in the tooltip and an `sr-only` table of the
   days. Amounts are never added across currencies: gross volume is in the
   currency picked with the chips above the chart (IDR first), shown only
   when the range has more than one; counts and the success rate cover every
   currency and come from `payments.totals`, so the console never computes a
   rate the server also defines. No sparklines.
2. Two ruled lists side by side from 1024px. Needs attention: failed
   deliveries (`/webhooks?status=FAILED`), rejected callbacks
   (`/webhooks?tab=inbound`) and unresolved reconciliation items
   (`/reconciliation?tab=runs`), each a link in the danger ink, or one
   sentence saying nothing needs fixing; the latest reconciliation run is
   the last line. Connections: up to five, each one line with its name, the
   environment, the provider and the last test, with "View all" in the
   header.
3. Recent payments: the newest eight as a `DataTable` with "View all
   payments".

Tests that render a chart call `stubElementSize` from `src/test/layout.ts`
first, because jsdom measures every element as 0×0 and recharts draws
nothing at that size.

States: a skeleton shaped like the page; `ErrorState` with retry for the
whole page (a `403` shows `ForbiddenState`); each card has its own empty
text, so an empty range never looks like an error.

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
- Ids: public ids (`app_…`, `pay_…`, `key_…`, `usr_…`, `con_…`, `whe_…`,
  `evt_…`, `dlv_…`, `inb_…`) look like `pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C` and
  are always mono with copy. Numeric ids from older endpoints (ledger
  entries) show as `#12`.
- Missing values say what they mean, never a dash: "Not set" for an optional
  field left empty, "Never" for something that has not happened, "Not yet"
  for a step still to come, "Unknown" for a value the gateway did not send,
  and "None" where nothing exists. `DateTimeText` takes a `fallback` for this.

## Copy

- Sentence case, short and specific: "Rotate credentials", not "Rotate API
  Credentials Now".
- English today; every string lives in `strings.ts` so another language can be
  added by swapping that object.
- Say what happened and what to do next: "Couldn't replay the delivery." plus a
  retry, not "Error".
- The UI and the API both say "app". The only leftover of "tenant" is the
  error code `tenant_inactive` (a disabled app), which is quoted as is.
- No em dashes, emoji, buzzwords ("seamless", "powerful", "next-generation")
  or generic calls to action ("Get started", "Learn more").
- A heading's sentence must add a fact. Cut any line that only restates the
  heading, such as "The account you are signed in with." under "Account".
- No number appears unless it comes from the server.

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
