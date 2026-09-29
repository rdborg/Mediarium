# Accounts and roles

Mediarium can have more than one login, so the people you live with can find and add films and shows themselves without being able to change how Mediarium is set up.

There are two roles:

- **Administrator.** Can do everything. The account made in the setup wizard is an administrator.
- **Member.** Can find, add and follow titles, but cannot see or change settings, indexers, download servers, the VPN, notifications, backups or other accounts.

Every account that existed before roles were added is an administrator after the upgrade, so nobody loses access.

## What a member can and cannot do

| Area | Member | Administrator |
|---|---|---|
| Dashboard, calendar, wanted list, activity | Yes. The dashboard leaves out setup warnings and folder locations on the server | Yes, including setup warnings |
| Discover, browse by genre and year, title search | Yes | Yes |
| Search indexers for releases and pick one to download | Yes, from the search results (a download link that did not come from a search here, or from a configured indexer's site, is refused) | Yes |
| Add a movie or show, choosing its quality profile and downloaders while adding | Yes | Yes |
| Start a search now, turn monitoring on or off for a title, season or episode | Yes | Yes |
| Watch the download queue and retry a failed download | Yes | Yes |
| Get subtitles for titles in the library | Yes | Yes |
| See a title's files, play a downloaded video and preview its pictures and text files (nfo, subtitles) in the browser | Yes (file names relative to the title's folder, never full server paths) | Yes |
| Their own profile, password and API keys | Yes | Yes |
| Remove a title from the library, or delete its files | No | Yes |
| Change a title's quality profile or downloaders after it was added | No | Yes |
| Remove or clear queue items, blocklist a release, resolve import conflicts, manage the blocklist | No | Yes |
| Create, edit or delete quality profiles (members can see them to choose one) | No | Yes |
| Settings, indexers, Usenet servers, VPN, notifications, connectivity checks | No | Yes |
| Import an existing library, backup and restore, usage and metrics | No | Yes |
| Dismiss subtitles, run the subtitle sweep, see the OpenSubtitles quota | No | Yes |
| Manage accounts | No | Yes |

When a member tries something only an administrator can do, Mediarium answers "Only an administrator can do this." An API key works with the rights of the account that made it, so a member's API key has member rights.

The [HTTP API reference](./reference/api.md) marks every route as **public**, **any account** or **admin**.

## What a member sees

Mediarium only shows a member what they can use:

- Under **Settings** there are just two pages: their own profile (name, email, password and API keys) and About & Credits. Opening the address of another settings page takes them to their profile. The profile menu in the top-right corner calls it **Your profile**.
- The **Library** has no Import, Remove or quality-profile controls.
- **Activity** shows the queue and history with a Retry button on failed downloads, but no Remove, Clear finished, Blocklist or conflict buttons, and no Blocklist tab. A download waiting for a decision about an existing file is listed as waiting for an administrator.
- The **Dashboard** leaves out setup warnings, folder locations and links into settings.
- On the Wanted page they can get subtitles, but not mark them as not needed.

## The account list

Administrators manage accounts in **Settings > Profile & Accounts** (the profile menu has a shortcut with the same name). Below your own details, password and API keys is the **Accounts** list: one card per account, side by side, with its name and username, an **Admin** or **Member** badge, its email address and when it last signed in ("Signed in 3 days ago", "Never signed in"). Your own card is marked **You**.

## Adding a family account

1. Sign in as an administrator.
2. Open **Settings > Profile & Accounts** and fill in **Add an account**: a username (at least 3 characters), optionally a name and email address, a password (at least 8 characters; the eye button shows what you typed), and what they can do. Pick **Basic user** for basic users: they can find, add and download movies and shows, but cannot change settings. **Administrator** gives full control, including settings and accounts.
3. Tell them the username and password. They can change the password themselves from their profile once signed in.

Titles a person adds show who added them ("Added by Sam"), in the library and in the activity list.

## Changing, resetting and removing accounts

An administrator can, for any other account, from the buttons on its card in the account list:

- **Edit**: change the name, email address or role (make a member an administrator, or the other way round). The change applies straight away, including to that person's open sessions;
- **Edit**: set a new password, for example when a basic user has forgotten theirs. That signs the account out everywhere, so they sign in again with the new password. Leave the field empty to keep the current password;
- **Remove**: delete the account, after a confirmation. Its sessions and API keys stop working at once. Titles it added stay in the library.

If something cannot be done (the username is taken, or it would leave no administrator), the message from Mediarium is shown as it is.

Two rules keep you from locking yourself out:

- There is always at least one administrator. The last administrator cannot be deleted or made a member.
- An administrator cannot delete their own account or remove their own administrator role. Ask another administrator, or make someone else an administrator first. On your own card, Remove is greyed out and the role cannot be changed; change your own password in the Change password section of the same page.

## A forgotten administrator password

If the only administrator has forgotten their password, reset it from the command line on the machine running Mediarium. There is deliberately no way to do this over the web.

In Docker (use your own PUID:PGID, so the database files keep the right owner):

```sh
docker exec -it -u 1000:1000 mediarium /app/app reset-password <username>
```

Running natively, with the same environment variables Mediarium normally runs with:

```sh
mediarium reset-password <username>
```

It asks for the new password twice (at least 8 characters) and signs that account out of every open session. The same command works for any account, member or administrator.

## For developers

- Roles are stored in `users.is_admin` (1 administrator, 0 member). The API reports both `role` (`"admin"` or `"member"`) and `isAdmin`.
- Accounts: `GET /api/users`, `POST /api/users`, `PUT /api/users/{id}`, `DELETE /api/users/{id}` (administrators only).
- Which routes a member may call is decided in one place, `protectedRoutes` in `internal/api/server.go`: each route is registered on either the `member` or the `admin` group. A test lists every member route by name and fails when that list changes, or when a route is registered outside the two groups.
