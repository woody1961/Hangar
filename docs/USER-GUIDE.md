# Hangar user guide

Hangar keeps a searchable record of your Star Citizen hangar on your own computer: every pledge (package), the ships
and items inside it, what you paid, what you upgraded, and what has been melted or gifted. Its main job is to answer
"do I already own this?" before you buy it again.

Everything stays on your computer. Hangar never sees or stores your RSI password.

## Contents

1. [Installing, starting and quitting](#1-installing-starting-and-quitting)
2. [Getting your hangar in](#2-getting-your-hangar-in)
3. [Finding things](#3-finding-things)
4. [Items and packages](#4-items-and-packages)
5. [Upgrades, insurance and prices](#5-upgrades-insurance-and-prices)
6. [Melted and gifted pledges](#6-melted-and-gifted-pledges)
7. [Editing by hand](#7-editing-by-hand)
8. [Settings](#8-settings)
9. [Backups, restore and moving your data](#9-backups-restore-and-moving-your-data)
10. [Exporting](#10-exporting)
11. [Privacy and streaming](#11-privacy-and-streaming)
12. [Keyboard shortcuts](#12-keyboard-shortcuts)
13. [Troubleshooting](#13-troubleshooting)
14. [Good to know](#14-good-to-know)
15. [Glossary](#15-glossary)

## 1. Installing, starting and quitting

| Computer | Install and start |
|---|---|
| Mac | Unzip **Hangar-macOS.zip**, drag **Hangar** to Applications, and open it from there or from Launchpad. The first time, right-click it and choose **Open** (macOS asks because the app isn't notarised). |
| Windows | Double-click **Hangar-Windows.exe**. If SmartScreen warns about an unknown publisher, choose **More info**, then **Run anyway**. You can keep the file anywhere, for example in a folder under Documents, and pin it to the taskbar once it's running. |
| Linux | Build it once with `scripts/build-linux.sh` (see the README), then run `./Hangar-Linux-x64`. |

The Hangar window opens straight away; no browser is needed. Close the window to quit.

The first time, Hangar opens on an empty hangar with a welcome screen. Choose **Import from RSI…** to fill it, or, if
you used the earlier Hangar, **Use an existing Hangar data folder…** to open that data as it is (see
[section 9](#9-backups-restore-and-moving-your-data)).

## 2. Getting your hangar in

Click **Import from RSI** (the blue button at the top, or the one on the welcome screen).

### With Hangar's own RSI window (recommended)

1. Click **Open RSI…**. Hangar opens a separate Chrome, Edge or Brave window on your RSI pledges page. Sign in there if
   asked. Your password goes to RSI's own page only. The window remembers you for next time.
2. When your pledges are visible, come back to Hangar and click **Continue**. Hangar reads every page of your pledges
   and your Hangar log (about a minute) and imports them.

Pictures download in the background afterwards; they appear in the list as they arrive, and a message says when they
are all in. You can close the dialog meanwhile. A picture that can't be downloaded is tried again at the next import.

While Hangar is reading or saving, **Done** is greyed out, and Esc or a click outside won't close the dialog either.

The window uses a browser profile of its own, separate from your everyday browser. **Forget my RSI sign-in** closes it
and deletes the saved sign-in.

### With a bookmark (Safari, Firefox, or no Chrome/Edge/Brave)

Open *Use a bookmark instead*:

1. Click **Copy the "Import to Hangar" bookmark**. In your browser, add a new bookmark (⌘D on a Mac, Ctrl+D on
   Windows and Linux), edit it, and paste into its address (URL) field. You only do this once.
2. Sign in at your RSI pledges page and click the bookmark. It reads every page of your pledges and sends them to
   Hangar, which must be open. The first time, Chrome may ask to allow the site to connect to apps on your device;
   choose Allow.

If your browser blocks the bookmark from reaching Hangar (Safari often does), it copies the data to your clipboard
instead. Open *The bookmark says it couldn't reach Hangar?*, paste it there, and click **Import pasted data**.

### What an import does

- A pledge already in Hangar is **skipped**, never duplicated.
- A package you added by hand is **matched by name** to its RSI pledge and picks up the pledge ID, date, value and
  pictures.
- **New pledges** become new packages with all their items.
- Upgrades, insurance, prices and melted or gifted pledges are worked out from the pledge list and your Hangar log
  (sections 5 and 6).
- Items RSI lists that your package doesn't contain (name reservations, for example) are reported, not added.
- The result lists anything that needs a look, including any ship where Hangar and RSI disagree.
- An import is all or nothing: if it fails part-way, none of it is kept.
- A safety copy of your database is saved first (`before-rsi-import-…`). If an import goes wrong,
  [restore it](#9-backups-restore-and-moving-your-data).

You can import as often as you like; it only adds what is new and brings dates, values, upgrades and status up to date.

### Starting fresh

**Import from RSI → Start fresh…** sets your current data aside in the data folder's `archive` folder (nothing is
deleted), begins empty, and opens RSI so you can import everything again. Pledges that already left your hangar are
added as packages without items, so searches can still say "You previously had…".

To go back, quit Hangar and copy `ships.db` and `images` from the archive folder into the data folder.

## 3. Finding things

### Search

Press `/` (or ⌘K / Ctrl+K) and type. Search looks at name, package, type, manufacturer, purpose, notes and IDs, and
ignores punctuation: `atls` finds "A.T.L.S" and `csv sm` finds "CSV-SM". Old names count too: searching the ship you
upgraded from finds the one it became.

A banner above the results tells you whether you own it:

- green: *You already own…*
- *You previously had… (melted 2022-11-26)* for matches that are no longer in your hangar

The banner ignores column filters, so a filter can never hide the answer.

### The sidebar

Choose **All Items** (one row per ship or item) or **Packages** (one card per pledge). Filter by **Type** or by
**Purpose** (one at a time; items with no purpose are under *No purpose*), and open **Melted**, **Gifted away** or
**Not in hangar** (these appear only when you have some).

### The item table

Click a column title to sort. Click again to reverse, and a third time to return to the default order (purchase date,
newest first). Blank values always sort last.

Under each title is a filter box:

- text columns match what you type;
- Type, Manufacturer, Insurance and Flight Ready offer a list, including *(blank)*;
- Storage, Purchase and Value take numbers: `>100`, `<=50`, `100-200`. A box turns red when it can't understand
  what you typed.

Active filters and sorting appear as chips above the table. Click a chip to remove it, or use *Clear all filters*.

Columns: Name, Type, Manufacturer, Purpose, Insurance, Crew, Storage, Flight Ready, Date, Purchase, Value, Pledge ID
and Ship ID (only ships you have named in RSI have one; their custom name shows under the ship's name). Date,
Purchase, Value and Pledge ID belong to the package. You can hide any column except Name in [Settings](#8-settings).
When the columns don't fit the window, the table scrolls sideways.

### Packages view

Each card shows the pledge, its date, its items and its money. *Sort packages by* offers purchase date, name,
purchase value, upgrade cost, current value and number of items; the button beside it reverses the order. The totals
at the top cover whatever is on screen, so filter first (search, or the Melted category) to total just those.

## 4. Items and packages

A **package** is one pledge: what you bought in a single purchase. An **item** is something in it: a ship, a paint, a
game package... Insurance is not an item of its own; it is a field on each ship.

Click any item to open its editor. You can change every field. To add or change a picture, click the picture box and
choose a file, or paste an image web address and click **Fetch**. Pictures you choose are shrunk to about 480 px.
**Remove picture** takes it away.

If RSI gave an item no picture, Hangar shows a symbol for its kind (a house for hangars, for example) or its first
two letters.

RSI does not publish **Crew**, **Storage**, **Flight Ready** or **Purpose**, so these start empty for you to fill in.
Hangar never overwrites what you type there. **Storage** is a cargo capacity in SCU: type just the number (96, or
0.5). The table shows it as "96 SCU", sorts it numerically, and you can filter it like the money columns.
**Flight Ready** is blank, *Yes* or *In Concept* (announced but not yet flyable). The small ▾ beside Type,
Manufacturer and Purpose offers the values already in your hangar.

## 5. Upgrades, insurance and prices

### Upgrades

Hangar shows the ship you own **now**.

- RSI's current pledge contents are the truth. An upgrade that was later undone is ignored.
- The ship carries its current name, with "↑ upgraded from …" beneath it. The item editor lists every step with its
  date, upgrade ID and price.
- Chains are followed: HoverQuad → X1 Force → Apollo Medivac.
- Searching an old name still finds the ship it became.
- Pledges that share a name (three "Standalone Ship - HoverQuad") are told apart by what is inside them and by
  upgrade history, so each upgrade lands on the right package.
- Hand-written markers such as "(Upgraded)" are removed, because the history replaces them.

### How Hangar knows which ship an upgrade went to

RSI gives IDs to pledges, to upgrades, and to ships you have named, but not to other individual ships. Your Hangar
log names the **pledge** an upgrade was applied to, and the from/to ship names let Hangar follow the chain. Hangar
stores:

- the **pledge ID** of each package (Pledge ID column, the item editor, the CSV);
- the **upgrade ID** of every upgrade step (item editor, CSV `upgrade_ids`);
- for a ship you have **named** in RSI, its own **ship ID** and custom name (Ship ID column, item editor, CSV
  `membership_id` and `custom_name`). Both are searchable.

### Insurance

RSI lists insurance once for a package, and Hangar gives each ship in it that term: *Lifetime*, *120 months*,
*6 months*... When more than one term could apply to a ship, **the longest applies**; an upgrade keeps the original
insurance unless it carries a longer one. The import report lists such packages. If you set a ship's insurance by hand,
an import will not overwrite it.

### Prices

- **Purchase value** is what the pledge was worth when created (from the log).
- **Current value** is RSI's figure today, including upgrades.
- **Upgrades +$…** is the sum of upgrade prices.

Example: *Purchase $25.00 · Upgrades +$245.00 · Value $270.00*.

These are RSI **pledge values** in USD, which is what a melt would return. They are not necessarily the cash you
paid: store credit, discounts and promotions are not visible in the log. You can edit any value by hand.

## 6. Melted and gifted pledges

Pledges that leave your hangar are **kept**, never deleted, and are hidden from normal lists and counts.

- **Melted** (reclaimed for store credit): under *Melted* in the sidebar, with the date and credit.
- **Gifted away**: under *Gifted away*, with the date and who received it.
- **Gift not yet claimed**: still yours, so it stays in the normal lists with a "Gift not yet claimed" badge. When the
  recipient claims it, it moves to *Gifted away*.
- A pledge missing from your hangar that the log cannot explain is marked "No longer in hangar".
- A pledge that comes back (a gift reclaimed, say) becomes owned again.
- A gift you cancel is no longer a gift: the package is simply yours again.

Hangar only marks things as gone when the whole hangar and the whole log were read, so a partial import cannot mark
anything wrongly.

### Gift recipients

The recipient's email address from the log is stored with the package and hidden on screen as dots. Click it to
reveal it and click again to hide it; it is hidden again whenever Hangar starts. In the package editor it stays hidden
and read-only until you press **Show**. It is not shown in the import report or the search banner. You can search for
an address to see everything you gave that person.

The address is kept in your database, so it is also in backups and in the CSV/JSON exports. Keep that in mind before
sharing those files.

## 7. Editing by hand

- **New Package** creates a pledge, then opens the editor for its first item. A package you add starts with today's
  date, so it appears at the top of the default order. Dates are written as YYYY-MM-DD.
- **New Item** adds an item; choose its package in the editor.
- **Edit** on a package card changes its name, dates, values and status (in my hangar, melted, gifted, no longer in
  hangar), and who a gift went to. **Add Item** adds an item to that package.
- **Delete** in either editor asks before it deletes. Deleting a package deletes its items.
- Anything you add by hand is matched to its RSI pledge the next time you import, by name.

## 8. Settings

Click **Settings…** in the sidebar. Changes apply at once and are remembered in `settings.json` in Hangar's folder.
The port and the data folder need a restart; a banner at the top offers **Restart Hangar now**.

### Appearance

| Setting | Choices |
|---|---|
| Theme | System (follows your computer), Light, Dark |
| Font size | Small, Default, Large |
| Thumbnail size | Small (54×33), Medium (108×66, the default), Large (216×133) |
| Row density | Comfortable, Compact |
| Date format | `2026-10-14` (default) or `Oct 14, 2026` |
| Columns shown | Untick the columns you don't use. Name always stays. |

### Privacy

- **Hide dollar amounts** removes the Purchase and Value columns from the item table and hides amounts shown elsewhere
  (package cards, editors, upgrade prices, totals).
- **Hide pledge IDs** removes the Pledge ID and Ship ID columns and hides those numbers elsewhere.

While a column is removed this way, its tick box under *Columns shown* is greyed out, and any filter or sort on it is
cleared.

### Startup

- **Start on** Items or Packages.
- **Port** (default 8742): where Hangar listens for the RSI bookmark on this computer. Needs a restart. A port another
  program is using is refused.

### RSI import

- **Close Hangar's RSI window when an import finishes.** Your sign-in is kept.
- **Browser for the RSI window**: Automatic, or a specific Chrome, Edge or Brave.
- **Download pictures during an import.** Turn off to import faster or save disk space; items then show a symbol.

### Data and backups

See the next section.

### About

The version, and the disclaimer below.

### Settings that environment variables control

`SHIPS_DATA` (data folder), `SHIPS_PORT` (port) and `HANGAR_BROWSER` (browser) override the settings file; when one is
set, Settings greys that item out and says so. `HANGAR_SETTINGS` points Hangar at a different settings file, and
`HANGAR_HOME` at a different Hangar folder altogether.

## 9. Backups, restore and moving your data

Hangar keeps everything in its folder: on a Mac `~/Library/Application Support/Hangar`, on Windows `%AppData%\Hangar`,
on Linux `~/.config/Hangar`. (If the program sits in a folder that already holds Hangar's `settings.json` or `data`
folder, it uses that folder instead.) Inside is `settings.json` and the **data folder**:

| Item | What it is |
|---|---|
| `ships.db` | The database |
| `images/` | Your pictures |
| `backups/` | Copies of the database |
| `archive/` | What *Start fresh* set aside |
| `rsi-browser-profile/` | The saved sign-in of Hangar's RSI window. Private; don't share it. |

Settings → Data and backups shows the data folder in use.

### Using data from the earlier Hangar

On the welcome screen, **Use an existing Hangar data folder…** asks for a folder: pick the earlier Hangar's folder or
its `data` folder. Hangar switches to it (nothing is copied) and restarts. You can do the same at any time from
Settings: type or choose that folder under *Data folder* and click **Move data…**; a folder that already holds a
Hangar database is switched to, not copied over.

### Backups

The backups list in Settings shows the kind, time, contents and size of each one.

- **Daily**: one for each day Hangar starts. *Daily backups to keep* (default 14).
- **Safety copies**: saved before every RSI import and before every restore. *Safety copies to keep* (default 5).
- **Manual**: **Back up now**. These are kept until you remove them.
- **Open backup folder** shows them in your file manager.

A backup holds the database but **not the pictures**. To protect pictures too, back up the whole data folder.

### Restore

Choose **Restore…** on a backup. Hangar checks that the file is healthy, saves your current database as a
"Before a restore" copy, then replaces the database with the chosen one. If you change your mind, restore that copy.

### Moving the data folder

**Move data…** copies the whole folder (database, pictures, backups, archive and RSI sign-in) to a folder you choose.
Click **Choose folder…** to pick it in your computer's own dialog, or type the path. Hangar switches to the new folder
after a restart. Choose an empty folder (or one that already holds a Hangar database), not inside the current data
folder. The old folder is left in place until you click **Delete the old folder…** and confirm, so you can check the
move first.

## 10. Exporting

In the sidebar, under Export:

- **Export as CSV**: one row per ship or item, with package name, `rsi_pledge_id`, `purchase_value`, `value`,
  `upgrade_cost`, `upgrade_ids`, and `custom_name` and `membership_id` for named ships. Opens in any spreadsheet app.
- **Full backup (JSON)**: all the data, not including pictures.

Hangar asks where to save each file. Both contain gift recipients' addresses and (in the JSON) everything in your
database, so be careful where you send them.

## 11. Privacy and streaming

- Hangar keeps everything on your computer and sends nothing anywhere.
- The importer reads only your own pledge and log pages, in your own signed-in browser window, and passes them only to
  Hangar on your computer.
- Your RSI password is typed into RSI's page and is never seen or stored by Hangar. The saved sign-in in
  `rsi-browser-profile/` is your RSI session, so don't share the data folder.
- For the bookmark, Hangar listens on `127.0.0.1` only and accepts imports only from RSI's site or from Hangar itself.
  The data folder is made private to your user on macOS and Linux. Hangar has no login of its own: anyone who can use
  your computer as you can open it.
- Hangar's RSI window uses the browser's local debugging connection while it is open, so on a shared computer close it
  when you're done (Settings can do that after every import).
- For streaming, turn on **Hide dollar amounts** and **Hide pledge IDs** in Settings. Gift recipients are hidden until
  clicked.

## 12. Keyboard shortcuts

| Keys | Action |
|---|---|
| `/` | Jump to search |
| ⌘K / Ctrl+K | Jump to search |
| Esc | Close the dialog that is open; with none open, clear the search |
| Enter | In an editor's text box: save |

## 13. Troubleshooting

**macOS says the app can't be opened or checked.** Right-click Hangar, choose **Open**, then **Open** again. This is
needed once, because the app isn't notarised by Apple.

**Windows SmartScreen warns about an unknown publisher.** Choose **More info**, then **Run anyway**.

**Open RSI… can't find a browser.** Install Chrome, Edge or Brave, or pick one in Settings → RSI import. Otherwise
use the bookmark method.

**The bookmark says it can't reach Hangar.** Hangar must be open. If another program uses Hangar's port, the Import
dialog says so: choose a different port in Settings and restart. Otherwise paste the copied data into *The bookmark
says it couldn't reach Hangar?*.

**The import found no pledges.** You may not be signed in, or RSI changed its page. Check that you can see your
pledges in the RSI window, then try again.

**Pictures are missing after an import.** They download in the background and appear as they arrive. Some RSI items
have no picture; those show a symbol. A picture that failed to download is tried again at the next import.

**An import went wrong.** Settings → Backups → Restore the "Before an RSI import" copy.

**I deleted a package by mistake.** Restore the most recent backup, then import again.

**I don't see my old data.** Use **Use an existing Hangar data folder…** on the welcome screen (section 9).

## 14. Good to know

- Crew, Storage, Flight Ready and Purpose are manual fields; RSI doesn't publish them.
- RSI gives an ID to a ship only once you have named it; every other ship is tracked through its pledge and upgrade
  IDs.
- Values are RSI pledge values, not necessarily what you paid.
- Hangar is unofficial and is not affiliated with Cloud Imperium Games or Roberts Space Industries (see below).

## 15. Glossary

**Package**: one pledge, one purchase.
**Item**: a ship, paint, game package or other thing inside a package.
**Pledge ID**: RSI's number for a pledge.
**Ship ID**: RSI's number for a ship you have given a custom name (its `membership_id`).
**Upgrade ID**: RSI's number for one upgrade application, from your Hangar log.
**Melt**: reclaim a pledge for store credit.
**Pledge value / purchase value / current value**: see [Prices](#prices).
**Safety copy**: a backup Hangar makes before something risky (an import or a restore).

---

Hangar is an unofficial, fan-made tool. It is not affiliated with, endorsed or sponsored by Cloud Imperium Games or
Roberts Space Industries. Star Citizen®, Roberts Space Industries® and Cloud Imperium® are trademarks of Cloud Imperium
Rights LLC and Cloud Imperium Rights Ltd. Hangar is provided as is, with no warranty; keep backups. See
[NOTICE.md](../NOTICE.md).
