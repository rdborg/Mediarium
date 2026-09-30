# Accounts and roles

Give the people you live with their own login. They can find and add movies and shows without being able to change how Mediarium is set up.

There are two roles:

- **Admin.** Can do everything. The account made in the setup wizard is an admin.
- **Basic user.** Can find, add and follow titles, but can't see or change settings, indexers, download servers, the VPN, notifications, backups or other accounts. (The API calls this role `member`.)

Accounts that existed before roles were added became admins, so nobody lost access.

## What a basic user can and can't do

| Area | Basic user | Admin |
|---|---|---|
| Dashboard, Upcoming (calendar and wanted list), Activity | Yes. The dashboard leaves out setup warnings and folder locations on the server | Yes, including setup warnings |
| Discover, browse by genre and year, title search | Yes | Yes |
| Search your indexers for releases and pick one to download | Yes, from the search results. A download link that didn't come from a search here, or from one of your indexers' sites, is refused | Yes |
| Add a movie or show, choosing its quality profile and downloaders while adding | Yes | Yes |
| Start a search now, turn monitoring on or off for a title, season or episode | Yes | Yes |
| Watch the download queue and retry a failed or stopped download | Yes | Yes |
| Pause, resume or stop a download (also Pause all and Resume all) | No | Yes |
| Get subtitles for titles in the library (when subtitles are switched on) | Yes | Yes |
| See a title's files, play a downloaded video and preview its pictures and text files (nfo, subtitles) in the browser | Yes (file names relative to the title's folder, never full server paths) | Yes |
| Their own profile, password and API keys | Yes | Yes |
| Remove a title from the library, or delete its files | No | Yes |
| Select several titles in the Library and change them together (monitor, better versions, quality profile, download source, search now, remove) | No | Yes |
| Change a title's quality profile or downloaders after it was added | No | Yes |
| Remove or clear queue items, blocklist a release, resolve import conflicts, manage the blocklist | No | Yes |
| Create, edit or delete quality profiles (basic users can see them to choose one) | No | Yes |
| Settings, indexers, Usenet servers, VPN, notifications, connectivity checks, Logs and errors, updates and restarts | No | Yes |
| Import an existing library, backup and restore, usage and metrics | No | Yes |
| Dismiss subtitles, run the subtitle sweep, see the OpenSubtitles quota | No | Yes |
| Manage accounts | No | Yes |

When a basic user tries something only an admin can do, Mediarium says "Only an administrator can do this." An API key has the rights of the account that made it. A few things need you signed in on the web page, even with an admin key: making API keys, adding or changing accounts, downloading or restoring a backup and switching on pushed updates. That way a leaked key can't open a second way back in.

The [HTTP API reference](./reference/api.md) marks every route as **public**, **any account** or **admin**.

## What a basic user sees

- Under **Settings** there are two pages: their own profile (name, email, password and API keys) and About and credits. Any other settings address lands on their profile. The profile menu in the top-right corner calls it **Your profile**.
- The **Library** has no Select, Import existing, Remove or quality-profile controls.
- **Activity** shows the queue and history, with a Retry button on failed and stopped downloads. There's no Pause, Resume, Stop, Remove, Clear finished, Blocklist or conflict button, and no Blocklist tab. A download waiting for a decision about an existing file is listed as waiting for an administrator.
- The **Dashboard** leaves out setup warnings, folder locations and links into settings.
- On the Wanted tab of Upcoming they can get subtitles (when subtitles are switched on), but not mark them as not needed.

## The account list

Admins manage accounts in **Settings > Accounts** (the profile menu has a shortcut). The page shows your own details and Change password side by side, then the **Accounts** list, then **Add an account**, and your API keys at the bottom. Each account has a card with its name and username, an **Admin** or **Basic user** badge, its email address and when it last signed in ("Signed in 3 days ago", "Never signed in"). Your own card is marked **You**.

## Adding a family account

1. Sign in as an admin.
2. Open **Settings > Accounts** and fill in **Add an account**: a username (3 to 32 characters: letters, numbers, dots, dashes and underscores), optionally a name and email address, a password (8 to 72 characters; the eye button shows what you typed), and a role. **Basic user** can find, add and download movies and shows. **Admin** has full control.
3. Tell them the username and password. They can change the password from their profile.

**What is checked.** Every account form points out a problem under the field when you leave it, or when you press the button, and nothing is sent until it's fixed. The server enforces the same rules, so they hold for scripts too: usernames of 3 to 32 characters with only letters, numbers, dots, dashes and underscores; an optional email address that looks like `name@example.com`; a name of up to 100 characters; passwords of 8 to 72 characters. Only a username you change is checked, so an older username with other characters keeps working.

The activity list says who added a title, for example "The Matrix added to library by Sam".

## Changing, resetting and removing accounts

An admin can do the following for any other account, using the buttons on its card:

- **Edit**: change the name, email address or role. It applies straight away, including to that person's open sessions.
- **Edit**, again: set a new password, for example when a basic user has forgotten theirs. That signs the account out everywhere. Leave the field empty to keep the current password. (Changing your own password from your profile also signs out every other browser, but not this one.)
- **Remove**: delete the account, after a confirmation. Its sessions and API keys stop working at once. Titles it added stay in the library.

If something can't be done (the username is taken, or it would leave no admin), Mediarium shows its message as it is.

Two rules stop you from locking yourself out:

- There is always at least one admin. The last admin can't be deleted or made a basic user.
- An admin can't delete their own account or remove their own admin role. Ask another admin, or make someone else an admin first. On your own card, Remove is greyed out and the role can't be changed.

## A forgotten administrator password

The sign-in page has a **Forgot your password?** line. There's no email reset. Another administrator can set a new password in **Settings > Accounts**, and the command below is for the only administrator.

If the only admin has forgotten their password, reset it from the command line on the machine running Mediarium. There is deliberately no way to do this over the web.

In Docker (use your own PUID:PGID, so the database files keep the right owner):

```sh
docker exec -it -u 1000:1000 mediarium /app/app reset-password <username>
```

On the `-full` image the program is at `/opt/mediarium/app` instead of `/app/app`.

Running natively, with the same environment variables Mediarium normally runs with:

```sh
mediarium reset-password <username>
```

It asks for the new password twice (at least 8 characters) and signs that account out of every open session. It works for any account, admin or basic user.

## For developers

- Roles are stored in `users.is_admin` (1 for an admin, 0 for a basic user). The API reports both `role` (`"admin"` or `"member"`) and `isAdmin`.
- Accounts: `GET /api/users`, `POST /api/users`, `PUT /api/users/{id}`, `DELETE /api/users/{id}` (administrators only).
- Which routes a basic user may call is decided in one place, `protectedRoutes` in `internal/api/server.go`: each route is registered on either the `member` or the `admin` group. A test lists every member route by name and fails when that list changes, or when a route is registered outside the two groups.
