# Library lifecycle

Library lifecycle takes care of what nobody watches any more, without surprises. It is **off** until an administrator switches it on under **Settings > Media types** (the setup wizard and the What's new window offer it too). While it is off nothing runs and every `/api/lifecycle` answer is empty.

It uses the same three rules as the [cleanup rules](./media-servers.md#cleanup-rules) and the same watch data, so **What's been watched** has to be on and every media server has to have answered in the last two days. Switching a rule on switches reading what's been watched on for you. While Library lifecycle is on it replaces the daily cleanup: the cleanup rules keep their settings but stop running by themselves.

## How it works

1. **Set the rules.** The card under the module cards on Settings > Media types has the three rules (movies watched a while ago, movies nobody watched, episodes watched a while ago), **Days on the shelf first** (14 at first, 1 to 365) and **Always keep titles tagged**.
2. **Dry run.** The **Dry run** button saves the rules and lists the titles they would put on the shelf. Nothing is changed and nothing is removed.
3. **Act for real.** The switch can only be turned on after a dry run with the rules as they are now. Change a rule and it asks for a new dry run. It asks again before it switches on. While it is on, the rules are locked.
4. **The Leaving soon shelf.** Titles that match a rule are put on the shelf (the first ones right away, then once a day) with the days they have left. The shelf is on **Home**, for every signed-in account, and each title has a **Keep** button.
5. **Removal.** When a title has waited out its days and still matches, its files are removed the way the cleanup removes them: into the recycle bin when it is on (Settings > System), otherwise deleted. The title stays in the library as missing and stops being looked for. Every removal is written to Activity ("Library lifecycle removed ...").

A title that stops matching (watched again, or downloaded again) leaves the shelf by itself. A title put on the shelf by one run is never removed by that same run: the days start when it goes on the shelf. At most 50 titles are removed in one run.

## Keep

**Keep** on the shelf exempts a title from then on: a movie by itself, and for an episode its whole show. Anyone signed in can press it, like they can already tag or unmonitor a title. The administrator sees the kept titles in the card and can press **Stop keeping**.

Tags work as before: a movie or show with one of the **Always keep titles tagged** tags is never put on the shelf.

## Switching it off

Switching **Act for real** off empties the shelf, so the days start again next time. Switching the module off hides the shelf and stops the daily run; the rules and the kept titles are remembered.

## For scripts

| Route | Who | What |
|---|---|---|
| `GET /api/lifecycle/shelf` | any account | the titles on the shelf, soonest first, with `daysLeft` |
| `POST /api/lifecycle/keep` | any account | `{"kind": "movie" \| "episode", "id": 3}` |
| `GET /api/lifecycle` | administrators | rules, last run, kept titles |
| `PUT /api/lifecycle` | administrators | save the rules; `actForReal: true` needs a dry run with the same rules |
| `POST /api/lifecycle/dry-run` | administrators | what the saved rules would put on the shelf |
| `POST /api/lifecycle/unkeep` | administrators | `{"kind": "movie" \| "series", "id": 3}` |

The settings are `modules.lifecycle`, `lifecycle.rules` and `lifecycle.status` (see the [settings keys](./reference/settings-keys.md)).
