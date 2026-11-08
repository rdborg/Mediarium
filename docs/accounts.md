# Accounts and roles

Give the people you live with their own login. They can find and add movies and shows without being able to change how Mediarium is set up.

There are three roles:

- **Admin.** Can do everything. The account made in the setup wizard is an admin.
- **Basic user.** Can find, add and follow titles, but can't see or change settings, indexers, download servers, the VPN, notifications, backups or other accounts. (The API calls this role `member`.)
- **Viewer.** Can look and play, never change anything. See [Viewers](#viewers). (The API calls this role `viewer`.)

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

## Permissions for basic users

When you add or edit a basic user, **Permissions** lets you choose what they may do. Everything is on to start with, which is what a basic user could always do, so you only switch off what you don't want:

- **They can add or ask for:** Movies, TV shows, Music, Books (only the types switched on are listed). A type that's off can't be added or asked for at all.
- **Add titles themselves.** Off: what they add becomes a **request** instead (see below). Good for children's accounts. Without it they also can't follow a book series or an author (that adds books by itself), and picking a release in Search only works for titles already in the library.
- **Pick releases.** Search your indexers and choose what to download.
- **Search now and monitoring.** Start searches, change what is monitored and the tags, follow authors and series.
- **Retry downloads.** Try a failed or stopped download again.
- **Subtitles.** Find and download subtitles.
- **Play, read and listen.** Play videos, preview files, and read and listen in Mediarium Books.

Buttons for things an account may not do are hidden, and the server refuses them anyway ("Your account isn't allowed to do this."). Admins can always do everything.

## Requests

A basic user without **Add titles themselves** still sees Discover and the search box, but their Add button says **Request**. What they pick is kept as a request, exactly as they chose it (quality, formats and so on), and they're told it was sent to an administrator.

- **Activity > Requests** lists them. Admins see everyone's, with **Approve** (the title is added for that person, as they asked, and the library says who asked) and **Decline** (with an optional note the person sees). Everyone else sees their own, and can take back a request that is still waiting. Asking twice for the same title is refused; two titles that only share a name (a 1984 film and its 2021 remake) are separate requests. A request whose kind of media has been switched off waits until it is switched on again. For music, "Only this album" is part of the request, so approving it adds the artist with just that album.
- New requests can reach you by notification: tick **Requests** on a notification under Settings > Connections > Notifications.
- Asking twice for the same title, or for something already in the library, is refused with a message.

### The request portal settings

Under **Settings > Account & Access > Requests and reports**, the **Requests** card holds five things. All of them are off or unlimited until you change them.

**Tell people when their request is answered.** Switch it on and the person who asked gets a message when their request is approved, declined (with your note) and when the title is ready to watch in the library, and when a problem they reported is answered. Messages go:

- by **email**, to the address on their account, through the email server of an email notification you already set up under Settings > Connections > Notifications (the message keeps that notification's server and login and only changes who it goes to). Without an email notification, or without an address on the account, nothing is sent by email;
- to the **phones and browsers** the person allowed notifications on, under **Your profile > Messages about your requests > Also send them to this device**. Push needs Mediarium opened over https (see [notifications.md](./notifications.md)).

The card on someone's profile has a switch, **Send me these messages**, for people who don't want them. It shows only while the setting above is on. "Ready to watch" is noticed within about ten minutes of the first file arriving (for a show, its first episode). Requests approved before you switched messages on are not announced afterwards.

**Limit requests.** Set how many requests each person may make over **the last day** or **the last week** (a sliding window, not a calendar day). Empty or 0 means no limit. Waiting and approved requests count; declined requests and requests taken back do not. Someone who is out of requests is told when a place frees up. Under the default limit, each person who has to ask has a menu: follow the default, **No limit**, or **A number of their own**. People see how many they have left at the top of **Activity > Requests**, for example "3 of 5 requests left this week". Admins are never limited, and neither is anyone who can add titles themselves (they don't make requests).

**4K requests.** Switch on **Let people ask for a 4K copy** and choose the quality profile of that copy (the built-in **4K & over**, say). This uses [second copies](./library.md): you also need a folder for second copies under Settings > Library > Folders, for movies and for shows. The Add dialog of someone who has to ask then offers **Ask for a 4K copy as well**, and a title already in the library has an **Ask for a 4K copy** button on its page. The request shows a **4K** tag. When you approve it, the title is added the usual way if it is not in the library yet, and then a second copy in the 4K quality is added next to it and searched for. A title that is already in the library only gets the copy. A title whose own quality is already the 4K one doesn't need a copy, and one that has a 4K copy can't be asked for again.

**Age limits.** Under the quota table, **Age limits** lists every basic account with a menu for the highest rating it may ask for or add: no limit, up to G and TV-G (all ages), up to PG and TV-PG, up to PG-13 and TV-14, or up to R and TV-MA. Ratings from different countries are put on one short scale (all ages, about 10, about 14, about 17, 18 and over), so PG-13 and TV-14 count the same, and plain ages such as 12 or 16 and ratings such as U, 12A, 15, 18, FSK 16 or AL are placed on it too. Mediarium reads a movie's or show's rating from TMDB when someone asks for it (the answer is kept for a few minutes, nothing new is stored). A title above the limit is refused with a plain message that names its rating, and nothing is saved; the limit applies to accounts that add directly as well as to accounts that have to ask. A title without a rating is allowed, unless you tick **Also refuse titles with no rating** for that person. Titles above the limit still show up in Discover and search; they just can't be asked for or added. Administrators are never limited, and approving a request is not stopped by a limit either.

**One episode.** A person who has to ask can ask for a single episode instead of the whole show: **Ask for one episode** on the page of a show (in the library or not) opens a small form with a season and an episode. The request reads, for example, "Show S02E05", and asking for the same episode twice is refused. When you approve it, a show that is not in the library yet is added with nothing wanted but that episode, and a show that is already there only gets that episode set to wanted. Either way the episode is searched for at once, like the **Search** button of an episode does. An episode that is wanted already or has its file can't be asked for, and a single episode can't be asked for in 4K.

**Removal requests.** On every movie and show in the library, accounts that are not administrators have **Ask to remove**, with an optional reason. The request shows in **Activity > Requests** for administrators as a **Removal** request with the reason, and is told to you through the notifications that have **Requests** ticked. **Remove** asks the same question as the Remove button on the title's page, with the same choice to delete the files from the disk, and the files **stay by default**. **Decline** keeps the title and tells the person (with your note, when messages are on). A title that was already removed some other way is simply marked as approved. Removal requests don't count against a person's request limit, and a second copy can't be asked for by itself: ask for the main title.

**Problem reports.** Switch on **Let people report a problem with a title**. Every account then has a **Report a problem** button on movie and show pages in the library: what is wrong (wrong file, no sound or wrong audio, bad subtitles, bad picture, something else), for a show which episode, and a few words. Reports are listed under **Activity > Reports**, which shows only when reports are on. Admins see everyone's with who reported them, answer with **Resolve** and an optional comment (the person is told, when messages are on), can **Reopen** or remove one, and are told of new ones through notifications that have **Requests** ticked. A person sees their own reports and your answers. The same person can't report the same thing twice while it is open, and has at most 20 open reports.

## What a basic user sees

- Under **Settings** they have **General** (app language and light or dark), their own **Your profile**, **Security** (passkeys, two-step sign-in, API keys), **Linked services** (not for view-only accounts), Statistics and About and credits. Any other settings address lands on General. The profile menu in the top-right corner has **Your profile** and **App language**.
- The **Library** has no Select, Import existing, Remove or quality-profile controls.
- **Activity** shows the queue and history (file names, but not the folders on the server), with a Retry button on failed and stopped downloads. There's no Pause, Resume, Stop, Remove, Clear finished, Blocklist or conflict button, and no Blocklist tab. A download waiting for a decision about an existing file is listed as waiting for an administrator.
- The **Dashboard** leaves out setup warnings, folder locations and links into settings.
- On the Wanted tab of Upcoming they can get subtitles (when subtitles are switched on), but not mark them as not needed.

## Viewers

A viewer can open the library, Discover, the calendar and Activity, and play what is already there, but nothing they do changes anything. It is for someone you want to show the library to, or for a screen on the wall.

- A viewer can't add or request titles, search for releases, retry or pause downloads, change monitoring or tags, or get subtitles. The buttons are hidden, and the server refuses the calls anyway ("This account can only look, not change anything.").
- A viewer doesn't see the buttons for things that change something: asking for a 4K copy, reporting a problem, making a playlist or putting music on one, marking a title as not interested, and the ListenBrainz, e-reader and request message cards on the profile page. They can still open and play the playlists other people shared.
- A viewer can't see settings, other accounts, API keys, the secret calendar address or the Plex watchlist connection, and can't make API keys.
- A viewer can still change their own name, email and password, set up two-step sign-in and passkeys, and keep their place in a book.
- This holds for every route, in one place: the route table refuses any call that changes something before the handler runs. A test walks every route of the server to prove it. An API key made by a viewer follows the role, so it can't change anything either.

Pick **Viewer** when you add an account, or change the role of an existing one under **Edit**. Single sign-on and directory sign-in can make new accounts viewers too (see [Ways to sign in](#ways-to-sign-in)).

## Read-only API keys

When you make an API key under **Settings > Account & Access > Security > API keys**, tick **Read-only**. A read-only key can only be used for `GET` requests; every other request is refused with `403` ("This API key is read-only"), including on the Radarr, Sonarr and Lidarr style endpoints. It is meant for dashboards and monitoring scripts, such as Homarr or a status page, that only need to look. A read-only key of an admin can still read what an admin can read, so keep it as private as any other key. It is marked "read-only" in the key list. You can't change an existing key into a read-only one: make a new one and revoke the old one.

## The account list

Everyone has their own pages under **Settings > Account & Access** (the profile menu has a shortcut): **Your profile** (name, email and Change password side by side), **Security** (passkeys, two-step sign-in and your API keys) and **Linked services** (the Plex watchlist, ListenBrainz and your e-reader). The language of the app is under **Settings > General**. Admins also have **Settings > Account & Access > All accounts** (the **Accounts** list, then **Add an account**), **Requests and reports** and **Ways to sign in** (including sign-in through your reverse proxy). Each account has a card with its name and username, an **Admin** or **Basic user** badge, its email address and when it last signed in ("Signed in 3 days ago", "Never signed in"). Your own card is marked **You**.

**Revoke** stops an API key working straight away. A revoked key stays in the list, so you can see it was there. Press **Delete** next to a revoked key to take it off the list for good. Scripts: `DELETE /api/auth/api-keys/{id}` revokes, and `DELETE /api/auth/api-keys/{id}?remove=true` deletes a key that is already revoked (an active key answers 409).

## Adding a family account

1. Sign in as an admin.
2. Open **Settings > Account & Access > All accounts** and fill in **Add an account**: a username (3 to 32 characters: letters, numbers, dots, dashes and underscores), optionally a name and email address, a password (8 to 72 characters; the eye button shows what you typed), and a role. **Basic user** can find, add and download movies and shows. **Admin** has full control.
3. Tell them the username and password. They can change the password from their profile.

**What is checked.** Every account form points out a problem under the field when you leave it, or when you press the button, and nothing is sent until it's fixed. The server enforces the same rules, so they hold for scripts too: usernames of 3 to 32 characters with only letters, numbers, dots, dashes and underscores; an optional email address that looks like `name@example.com`; a name of up to 100 characters; passwords of 8 to 72 characters. Only a username you change is checked, so an older username with other characters keeps working.

The activity list says who added a title, for example "Night Harbour added to library by Sam".

## Changing, resetting and removing accounts

An admin can do the following for any other account, using the buttons on its card:

- **Edit**: change the name, email address or role. It applies straight away, including to that person's open sessions. Tick **Turn this account off** to stop the account without deleting it: its open sessions end at once, its API keys stop working, and it can't sign in (a right password is told "This account is turned off"). Untick it to bring it back. Nothing it added is touched. The card shows an **Off** badge.
- **Edit**, again: set a new password, for example when a basic user has forgotten theirs, or when someone else may know it. That signs the account out everywhere: its sessions end, its API keys are revoked, and its e-reader password, Kobo address and calendar feed address stop working (the person makes new ones after signing in). Passkeys and a single sign-on link stay, since they need the person's own device or provider account. Leave the field empty to keep the current password. (Changing your own password from your profile also signs out every other browser, but not this one.)
- **Remove**: delete the account, after a confirmation (from the web page only, not with an API key). Its sessions and API keys stop working at once. Titles it added stay in the library.

Every account change an administrator makes (creating an account, changing a role, turning an account off or on, setting a password, removing an account) and every API key made, revoked or deleted is written to the Mediarium log and to the **Activity** page, with who did it and from which address. Only administrators see these lines. They never contain a password or a key.

If something can't be done (the username is taken, or it would leave no admin), Mediarium shows its message as it is.

Two rules stop you from locking yourself out:

- There is always at least one admin who can sign in. The last one can't be deleted, turned off, or made a basic user or viewer.
- An admin can't delete or turn off their own account, or remove their own admin role. Ask another admin, or make someone else an admin first. On your own card, Remove is greyed out and the role can't be changed.

## Two-step sign-in

Each person can add a second step to signing in: after the password, Mediarium asks for the six-digit code from an authenticator app on their phone (Google Authenticator, Microsoft Authenticator, 1Password, Bitwarden, Authy and the like). Someone who learns the password still can't get in without the phone.

It is **off** until each person switches it on for themselves under **Settings > Account & Access > Security > Two-step sign-in**. Mediarium suggests it (on the dashboard and on the profile page) once it can be reached from outside your home: when a public address or sign-in through a proxy is set, or someone signed in from outside your home network in the last 30 days. It never forces it.

Setting it up:

1. Press **Set up two-step sign-in**.
2. In your authenticator app, add an account and scan the QR code (or type in the key shown under it).
3. Enter the six-digit code the app shows and press **Switch on**.
4. Save the ten **recovery codes** that appear (Copy or Download). Each one signs you in once instead of an app code, for when the phone is lost. They are shown only this once; **New recovery codes** (with your password) makes a fresh set and stops the old ones.

Signing in then takes the password and the code. **Can't use your app?** on that screen takes a recovery code instead. A code works once, and the phone's clock should be set automatically: a code is good for about a minute and a half.

If someone loses their phone and their recovery codes, an administrator can press **Two-step off** on their card in the account list (from the web page only, not with an API key). If you are the only administrator, run this on the machine that runs Mediarium (swap 1000:1000 for your PUID:PGID):

```bash
docker exec -it -u 1000:1000 mediarium /app/app reset-two-step <username>
```

API keys and sign-in through a reverse proxy don't use two-step sign-in: a key is its own secret, and the proxy does its own sign-in.

## Signing in through your reverse proxy

If a proxy such as Authelia or Authentik already asks everyone to log in, Mediarium can take the user name from it so nobody signs in twice. It is off by default and set under **Sign in through your reverse proxy** on this page, with the header name and your proxy's address. The names must match the accounts here. See [security.md](./security.md#sign-in-through-your-reverse-proxy).

## Ways to sign in

Besides the username and password of an account made here, there are three more ways in: **single sign-on** with an OpenID Connect provider, **directory sign-in** (LDAP or Active Directory) and **passkeys**. All of them are **off** until an administrator switches them on, under **Settings > Account & Access > All accounts**. Nothing changes for an existing install until then, and the password form stays where it is unless you turn it off yourself (see [Turning password sign-in off](#turning-password-sign-in-off)).

Accounts that come from single sign-on or the directory have **no password here**. Mediarium never stores or sees the password of a directory account beyond the one moment it is checked. So while that way to sign in is switched off (or `MEDIARIUM_DISABLE_SSO` is set), those accounts can't sign in at all; they come back as soon as it is on again. These accounts show a **Single sign-on** or **Directory** badge in the account list. Their name and email are updated each time they sign in, they can't change a password or set up two-step sign-in here (the provider does that), and an admin can't set a password for them.

### Single sign-on

You can use any OpenID Connect provider: Authelia, Authentik, Keycloak, Pocket ID, Google and others. People press **Sign in with <name>** on the sign-in page, sign in at the provider, and come back signed in.

1. **Set the address you open Mediarium at** under **Settings > General > Address of Mediarium** (for example `https://mediarium.example.com`). Mediarium builds the return address from it, never from what a browser sends. Without it single sign-on can't be switched on.
2. **Add Mediarium to your provider** as a client (an "application"): type *confidential*, the authorization code flow, and the redirect address the card shows under **Redirect address to enter at the provider** (it ends in `/api/auth/oidc/callback`). Ask for the scopes `openid profile email`, and `groups` too if you want to use groups. Copy the client ID and the client secret.
3. Open **Settings > Account & Access > Ways to sign in > Single sign-on (OpenID Connect)**. Fill in the name for the button, the provider's address (the *issuer*), the client ID and the secret. Press **Test the provider**: Mediarium reads the provider's setup and says what it found, then tick **Turn on single sign-on** and **Save**.

The issuer has to be written exactly as the provider reports it, including a closing slash if there is one:

| Provider | Issuer |
|---|---|
| Authelia | `https://auth.example.com` |
| Authentik | `https://auth.example.com/application/o/<application-slug>/` (with the slash) |
| Keycloak | `https://keycloak.example.com/realms/<realm>` |
| Pocket ID | `https://id.example.com` |
| Google | `https://accounts.google.com` |

(Check yours against the "OpenID configuration" address of the provider: the `issuer` it lists is what goes here.)

**What is checked on every sign-in.** The sign-in uses the authorization code flow with PKCE (S256). Mediarium makes a random `state` and a `nonce` for each sign-in, each good once and for ten minutes, and ties them to the browser that started the sign-in with a short-lived cookie, so a sign-in someone else started can't be finished in your browser. The ID token must be signed by the provider's own keys (found through its published key set), carry the issuer and audience Mediarium expects, not be expired, and carry the nonce. Anything else is refused and the person sees a short message on the sign-in page, never text from the provider.

**Accounts.**

- **Make an account the first time someone signs in** (off by default): the account gets the user name from the `preferred_username` claim (or the part of the email before the `@`), made to fit the rules. If another account already has that name, a number is added (`sam-2`): a new sign-in never takes over an existing account. **Role for new accounts** is Basic user or Viewer.
- **Administrator group**: people in this group (in the `groups` claim) are admins, checked at every sign-in. Take someone out of the group and their next sign-in makes them a basic user again. The last admin who can sign in is never demoted this way.
- **Viewer group**: people in this group are viewers (they can look, never change anything), checked at every sign-in the same way. It has to be another group than the administrator group; someone in both is an admin. Empty means nobody becomes a viewer this way. See [Groups and roles](#groups-and-roles) for the order and an example.
- **Only this group may sign in** and **Only these email domains** (for example `example.com`) limit who gets in at all, new accounts and returning ones alike. Use the domain list with a provider anyone can register at, such as Google.
- Without **Make an account...**, only people who already have an account can sign in: the sign-in page says "there's no account for you here yet".
- An account is tied to the provider's own id for the person (the issuer and the `sub` claim), never to the name or the email address, which can change hands. Moving to a different provider therefore makes new accounts.

**Linking an existing account.** Two ways:

- A person with a password here can open **Settings > Account & Access > Security** and press **Link to <name>**. After that, **Sign in with <name>** lands on their account, and their password keeps working. **Unlink** takes it away again.
- **Link a first sign-in to an existing account with the same email address** (off by default): when the provider says the email address is verified, and exactly one account here has it, that account is linked. Only an address an administrator set on the account (when making it, or with **Edit**) counts; one a person typed on their own profile never does, because anyone could type someone else's address. Addresses saved by an older version are not known to have been set by an administrator, so set them again with **Edit** if you rely on this. A sign-in with an unverified address is refused. An admin account is never linked this way, because whoever controls an email address at the provider would otherwise control an admin: an admin links their account from their own profile. Only turn this on if your provider checks email addresses.

#### Groups and roles

Each role can follow a group at the provider. At every sign-in Mediarium works out the role in this order:

1. In the **Administrator group**: admin.
2. Otherwise in the **Viewer group**: viewer.
3. Otherwise a new account gets **Role for new accounts**. An account that already exists keeps the role it has, with one exception: someone an administrator group made an admin, or a viewer group made a viewer, and who is no longer in that group, goes back to **Role for new accounts**.

What this means in practice:

- A role you set by hand in Mediarium (**Edit** on the account) is left alone by a group that isn't set up for it. With only an administrator group, a viewer you made by hand stays a viewer. With only a viewer group, a basic user or admin you made by hand stays as they are, unless they are in the viewer group. Setting a role by hand also ends the link to the group: a viewer you promote to basic user stays a basic user, even when they later leave the viewer group.
- Someone you made a viewer by hand who then shows up in the viewer group is looked after by the group from that sign-in on: when they leave the group they go back to the default role.
- With a viewer group but no administrator group, an admin is never made a viewer by the group, because nothing says that admin role came from a group.
- The last admin who can sign in is never demoted, whatever the groups say.
- Leaving both group fields empty changes nothing, as before: roles stay what they are in Mediarium.

Example with three groups at the provider: `mediarium-admins` as the administrator group, `mediarium-viewers` as the viewer group, and `mediarium-users` as **Only this group may sign in**, with **Role for new accounts** on Basic user. Members of `mediarium-admins` and `mediarium-users` are admins, members of `mediarium-viewers` and `mediarium-users` are viewers, and members of only `mediarium-users` are basic users. Make sure your provider sends the `groups` claim (ask for the `groups` scope).

If something goes wrong the sign-in page tells you in a sentence, for example "That sign-in took too long, or was started in another browser" or "Your email address isn't verified". The Mediarium log has one line with the short reason (never a code, token or secret).

### Directory sign-in (LDAP and Active Directory)

People use the normal sign-in form with their directory user name and password. Mediarium looks the person up with a service account, then asks the directory to check the password by signing in as that person. The password is used once and dropped.

Open **Settings > Account & Access > Ways to sign in > Directory sign-in** and fill in:

- **Server address**: `ldaps://ldap.example.com:636` is best. For `ldap://` tick **StartTLS** so the connection is encrypted before any password is sent. Sending passwords without encryption has to be ticked on purpose and is not recommended. The server's certificate is checked. For a directory with its own certificate authority, paste that CA certificate under **Attribute names and the certificate**; there is also a box to skip the check, which you should not use.
- **Base DN**: where people are, for example `ou=people,dc=example,dc=com`.
- **Bind account (DN) and password**: the account that looks people up. Give it read-only access to the people only. The password is kept encrypted and is never shown again.
- **User filter**: how a typed name finds one person; `{username}` stands for what was typed. To limit who may sign in, add conditions to it.

| Directory | User filter | Attributes to change |
|---|---|---|
| OpenLDAP, LLDAP, GLAuth, FreeIPA | `(&(objectClass=person)(uid={username}))` | FreeIPA: id attribute `ipaUniqueID` |
| Active Directory | `(&(objectClass=user)(sAMAccountName={username})(!(userAccountControl:1.2.840.113556.1.4.803:=2)))` | user name `sAMAccountName`, name `displayName`, id `objectGUID` |

Sign in by email as well as name with a filter such as `(&(objectClass=person)(|(uid={username})(mail={username})))`.

- **Administrator group filter** (optional): a filter that finds the person's membership, where `{dn}` stands for the person. People it finds are admins, checked at every sign-in. For OpenLDAP: `(&(objectClass=groupOfNames)(cn=mediarium-admins)(member={dn}))`. For Active Directory, including nested groups: `(&(objectClass=group)(cn=Mediarium Admins)(member:1.2.840.113556.1.4.1941:={dn}))`.
- **Viewer group filter** (optional): the same kind of filter for viewers. People it finds are viewers (they can look, never change anything), checked at every sign-in. For OpenLDAP: `(&(objectClass=groupOfNames)(cn=mediarium-viewers)(member={dn}))`. It has to be a different filter than the administrator group filter, and someone both filters find is an admin. Both filters are searched under **Where to look for groups**.
- **Make an account the first time someone signs in** and **Role for new accounts** work as for single sign-on, and so do the rules for which role wins and for roles you set by hand: see [Groups and roles](#groups-and-roles) above. A directory person who is in neither group gets **Role for new accounts**; one who leaves the viewer group (or the administrator group) goes back to it at the next sign-in. To limit who may sign in at all, put a group condition into the user filter.

Press **Find this person** under **Try it** to check the setup before you switch it on: it connects, signs in the bind account, looks the person up and shows the DN, name, email, the id that will be used and whether they would be an admin or a viewer. Add their password to check that as well. Nothing is saved and nobody is signed in. Switching it on needs a complete setup and a bind account the directory accepts.

How it behaves:

- A name that belongs to an account with a password here signs in with that password and never goes to the directory. A name nobody has here goes to the directory. A directory person can't take over a local account with the same name; they are told the name is taken.
- The account is found by the directory's own id for the person (`entryUUID`, `objectGUID`...), so a rename in the directory is followed and an old name given to someone else doesn't reach the old person's account.
- It has the same limits as a password sign-in: five wrong tries per address per 15 minutes, and 25 per user name from all addresses together. While password sign-in is turned off, a name that has a password here gets the same "Wrong username or password" answer as an unknown one, and the try counts. A wrong password and an unknown user look exactly the same. An empty password is refused before the directory is asked (some directories would otherwise treat it as a good anonymous sign-in).
- What is typed is escaped before it goes into the search, so characters like `*`, `(`, `)` and `\` can't change what is searched for.
- If the directory can't be reached the person is told so ("The directory couldn't be reached") and it doesn't count as a wrong password.
- Every step has a time limit (ten seconds by default).

**Keep in mind:** Mediarium only asks the directory when someone signs in. Taking a person out of the directory (or out of a group in the user filter) stops new sign-ins, but a session they already have lasts until it ends (up to 7 days). To cut it at once, turn their account off in Mediarium (**Edit > Turn this account off**).

### Passkeys

A passkey signs you in with your fingerprint, face or device PIN, with no password to type. It is a key pair kept in your phone, computer or security key; Mediarium keeps only the public half.

- An admin switches passkeys on under **Ways to sign in**. They need the address you open Mediarium at (**Settings > General > Address of Mediarium**), starting with `https://` and with a host name, not an IP address. `http://localhost` works too, for testing. The browser ties every passkey to that host name and only offers it on pages opened at that address: opened at a different address, passkeys don't show up.
- Each person adds passkeys on **Settings > Account & Access > Security > Passkeys**: **Add a passkey**, confirm with your password, then confirm on your device. Add one for each device, name them, rename them, and remove one when you lose a device. An account can have up to 20.
- On the sign-in page **Sign in with a passkey** needs no user name: the browser shows the passkeys it has for Mediarium.
- A passkey always checks the person too (PIN, fingerprint or face), so a passkey sign-in counts as a second step and doesn't ask for an authenticator code.
- A passkey keeps a counter that goes up with every use. If a sign-in comes in with a counter that did not go up, the passkey may have been copied, and the sign-in is refused. Many passkeys that sync between devices always report 0; those are fine.
- Only accounts with a password here can have passkeys. A passkey would outlive the account being turned off at the provider or in the directory.

### Signing in automatically

If most people sign in through single sign-on, the extra click on the sign-in page can go. Under **Settings > Account & Access > All accounts > Ways to sign in**, pick your provider in **Sign in automatically with** (the default is **Off**). Anyone who opens Mediarium without being signed in is then sent straight to the provider, and lands on the page they asked for after signing in. Password sign-in is not changed: it stays on, or off, as you set it.

- **To see the normal sign-in page** (for the password form, or when the provider is down), open Mediarium with `?no-redirect=1` on the end of the address, for example `https://mediarium.example.com/?no-redirect=1`. `?disable-auth-redirect` works too. This only skips the redirect. It doesn't turn the password form on if you turned it off.
- **After you sign out** you get the normal sign-in page once, so you aren't signed straight back in. The next visit redirects again.
- **No redirect when something is wrong.** If the provider sends you back with an error, the sign-in page shows the error and stays. The same goes when single sign-on is switched off or isn't set up (the saved choice is kept and applies again once it works), and when the page already has a message to show, like being signed out for inactivity. If the browser was sent to the provider less than a minute ago it won't be sent again, so a provider that returns without signing anyone in can't cause a loop.
- The first-run setup is never redirected.
- The choice is saved as `auth.auto_signin` (`"oidc"` or empty); scripts use `autoSignIn` in `PUT /api/auth/sign-in`.

### Turning password sign-in off

If everyone signs in another way, an admin can turn the password form off: **Ways to sign in > Turn off password sign-in**. The sign-in page then only shows the other ways. This can't be done by accident:

- It only works while you are signed in **through single sign-on, the directory or a passkey** as an admin, so you know a way in works. Otherwise Mediarium says why and does nothing.
- It can't be switched off with an API key.
- It is on again by itself whenever no other way to sign in is working (single sign-on or the directory switched off, passkeys without their address).
- Sessions that are already open aren't ended. API keys keep working.

### If you are locked out

Set `MEDIARIUM_DISABLE_SSO=1` on the container and restart it. Single sign-on and directory sign-in are off and the password form is on, whatever is saved, for as long as it is set. Nothing is changed in the settings, so when you take the variable away they apply again. Passkeys are not affected, since they are not a way to get locked out of an account with a password. Reset a forgotten password with the command below. Anyone who can set an environment variable on the container can already read the database, so this adds no new way in.

```yaml
environment:
  - MEDIARIUM_DISABLE_SSO=1
```

## A forgotten administrator password

The sign-in page has a **Forgot your password?** line. There's no email reset. Another administrator can set a new password in **Settings > Account & Access > All accounts**, and the command below is for the only administrator.

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

It asks for the new password twice (at least 8 characters) and signs that account out of every open session. The user name can be typed in any letter case, and the password can be piped in, typed twice on two lines (use `-i` instead of `-it` then). It works for any account, admin or basic user.

## For developers

- `POST /api/users` and `PUT /api/users/{id}` take `permissions` (`addDirect`, `movies`, `tv`, `music`, `books`, `releases`, `manage`, `retry`, `subtitles`, `play`, all true or false; any left out stays on). Accounts and `GET /api/auth/me` include them.
- An add from an account that has to ask answers `202` with `{"requested": true, "request": {...}, "message": "..."}`.
- `GET /api/requests` (an admin gets everyone's, anyone else their own, with a `pending` count), `POST /api/requests/{id}/approve` and `POST /api/requests/{id}/decline` with `{"note": "..."}` (admins), `DELETE /api/requests/{id}` (an admin, or the person for their own waiting request).

- Request extras: an add of a show can carry `"episode": {"season": 2, "number": 5}` to ask for (or, for an account that adds directly, add) that one episode only. `POST /api/requests/removal` (any account but an administrator) with `{"kind": "movie" or "tv", "itemId": <library id>, "reason": "..."}` makes a removal request (`action: "remove"` on the request, with its `reason`); `POST /api/requests/{id}/approve` on it takes `{"deleteFiles": true}` to delete the files too (the default keeps them). The age limit of an account is `maxRating` (a rating such as `PG-13` or `TV-14`, or an age such as `12`; empty is no limit) and `blockUnrated` in each entry of `accounts` from `GET /api/requests/settings`, and goes in as `ageLimits` (an object of account id to `{"maxRating": "...", "blockUnrated": false}`) in `PUT /api/requests/settings`. A title above the limit answers `403`.
- The request portal: `GET /api/requests/settings` and `PUT /api/requests/settings` (admins) hold `notifyRequester`, `quotaCount`, `quotaPeriod` (`day` or `week`), `fourK`, `fourKProfileId` and `reports`, and `quotas` (an object of account id to limit: -1 follows the default, 0 is no limit) on the way in; they are kept as one JSON setting, `requests.options`. `GET /api/requests/status` (any account) returns the caller's `quota` (`limit`, `used`, `left`, `period`, `resetsAt`), whether 4K can be asked for (`fourK.movie`, `fourK.tv`) and whether reports are on. An add with `"fourK": true` from an account that has to ask makes a 4K request. A request over the limit answers `429`.
- Problem reports: `GET /api/issues` (admins get all, others their own), `POST /api/issues` (`kind` `movie` or `tv`, `itemId` the library id, `category`, optional `episode` and `message`), `POST /api/issues/{id}/resolve` with `{"comment": "..."}`, `POST /api/issues/{id}/reopen` and `DELETE /api/issues/{id}` (admins).
- Messages to people: `GET /api/auth/request-notices`, `PUT /api/auth/request-notices` (`{"enabled": true}`), `POST /api/auth/request-notices/devices` (`subscription`, `name`) and `DELETE /api/auth/request-notices/devices/{id}`, each for the signed-in account.

- Roles are stored in `users.is_admin` (1 for an admin) and `users.is_viewer` (1 for a viewer, never together with `is_admin`). `users.group_role` is `viewer` when a viewer group gave the account that role and empty otherwise; changing the role by hand empties it. The API reports both `role` (`"admin"`, `"member"` or `"viewer"`) and `isAdmin`. `POST /api/users` and `PUT /api/users/{id}` take `role` and, on update, `disabled`. Accounts also report `disabled` and `source` (`"local"`, `"oidc"` or `"ldap"`).
- `POST /api/auth/api-keys` takes `readOnly: true`; the key list reports it. A read-only key gets `403` on anything but `GET` and `HEAD`.
- Sign-in: `GET /api/auth/methods` (public: what the sign-in page offers), `GET /api/auth/oidc/start` and `GET /api/auth/oidc/callback` (the browser's way through single sign-on), `POST /api/auth/passkey/login/begin` and `.../finish` (passkey sign-in). Signed in: `GET/POST/PUT/DELETE /api/auth/passkeys...`, `POST/DELETE /api/auth/oidc/link`. Admins, in a browser session only for changes: `GET/PUT /api/auth/sign-in`, `GET/PUT /api/auth/oidc`, `POST /api/auth/oidc/test`, `GET/PUT /api/auth/ldap`, `POST /api/auth/ldap/test`. Secrets (client secret, bind password) are never returned.
- Accounts: `GET /api/users`, `POST /api/users`, `PUT /api/users/{id}`, `DELETE /api/users/{id}` (administrators only).
- Which routes a basic user may call is decided in one place, `protectedRoutes` in `internal/api/server.go`: each route is registered on either the `member` or the `admin` group. A test lists every member route by name and fails when that list changes, or when a route is registered outside the two groups.
- What a viewer or a read-only key may do is decided in one place too (`readOnlyAllows` in `internal/api/access.go`), before any handler runs. A test sends a changing request to every route as a viewer and as a read-only key and fails if one gets through.
