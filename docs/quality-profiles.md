# Quality profiles

A quality profile decides which releases Mediarium may download for a movie or show, and when it stops looking for something better. Every title uses the **default profile** unless you give it one of its own. Change the default in Settings (or `PUT /api/settings` with `defaultProfileId`), and a single title's profile from its page.

A profile has:

- **Allowed qualities**: the quality tiers it accepts, such as `WEBDL-1080p` or `Bluray-2160p`. A release in any other tier is skipped.
- **Cutoff**: once a title has a file at or above this tier, Mediarium stops searching for upgrades.
- **Upgrades**: when on, a title that has a file below the cutoff keeps being searched for a better allowed release. When off, the first acceptable release is kept.
- Optional release restrictions: words a release name must contain, must not contain, and preferred words with a score.
- Optional **fallback profiles**: other profiles to try, in order, when nothing found is acceptable to this one (see below).

## The built-in presets

Mediarium comes with five presets, loosely based on the [TRaSH Guides](https://trash-guides.info/) recommendations. All five have upgrades on. They are listed from lowest to best, with Any last.

| Preset | Allowed qualities | Cutoff | Good for |
|---|---|---|---|
| **Cinema recordings** | CAM/TeleSync only | CAM/TeleSync | a film that is still only in cinemas |
| **720p** | HDTV-720p, WEBDL-720p, Bluray-720p | Bluray-720p | small screens, little storage or slow connections |
| **1080p** (default) | HDTV-1080p, WEBDL-1080p, Bluray-1080p | Bluray-1080p | most people |
| **4K & over** | WEBDL-2160p, Bluray-2160p, Remux-2160p | Bluray-2160p | 4K screens with plenty of storage |
| **Any** | every recognised tier, from SDTV and DVD up to Remux-2160p | Bluray-1080p | getting *something* for rare or old titles |

Some choices worth knowing:

- **Each resolution preset takes only its own resolution.** 1080p never settles for 720p and 4K & over never settles for 1080p. A title with no release at that resolution waits until one appears. Use **Any** (or your own profile) for titles that may never get one.
- **1080p** does not include **Remux-1080p**: a remux is an untouched copy of the disc, often 20 to 40 GB per movie, several times the size of a Bluray encode that looks the same on most screens. If you want remuxes, make your own profile.
- **4K & over**'s cutoff is Bluray-2160p, so a 4K remux is downloaded only when it is the best release found, not hunted as an upgrade.
- **Any** stops upgrading at Bluray-1080p, so a title is not chased all the way to a 4K remux. Releases whose name carries no recognisable quality are not accepted.
- **Cinema recordings and screeners** (CAM, HDCAM, TeleSync/TS/HDTS, Telecine/TC, screeners, R5) are their own quality, **CAM/TeleSync**, even when the name also says 1080p. Only the **Cinema recordings** preset accepts them, and it accepts nothing else; a new film with only such copies waits for a proper release. You can tick CAM/TeleSync in your own profile if you really want them.
- **Cinema recordings** accepts only CAM/TeleSync copies. It is most useful as a fallback (see below): a film that is still only in cinemas gets a recording now, and a proper release replaces it as soon as one appears.

The presets are ordinary profiles: you can edit, rename or delete them (except the one that is currently the default), and make your own from scratch.

## Fallback profiles

A profile can name other profiles to fall back on, in order. Every profile starts with none: you opt in per profile (`fallback` in `GET/POST /api/quality-profiles` and `PUT /api/quality-profiles/{id}`, an ordered list of profile ids; ids that do not exist, and the profile's own id, are ignored, and an update that leaves `fallback` out keeps the saved list). Deleting a profile removes it from every fallback list.

When an automatic search (the scheduled search, the new-releases check, **Search now**, the search after adding a title, the retry after a bad release) finds releases for a title with **nothing downloaded yet**:

1. The results are judged against the title's own profile first. If any is acceptable, the best of those is grabbed, as always.
2. Only if none is acceptable, the same results are judged against each fallback profile in order, and the best release of the first fallback that accepts anything is grabbed. Each fallback uses its own allowed qualities and release restrictions; a fallback's own fallbacks are not followed.

The title keeps its own profile. A file that came from a fallback (its quality is outside the title's profile but allowed by one of its fallbacks) is always searched for a replacement, even when upgrades are off, and any release the title's own profile accepts replaces it, whatever the cutoff. Upgrades of a file the own profile already accepts never use a fallback.

For example, a **1080p** profile with **Cinema recordings** as its fallback downloads a TeleSync copy of a film that is still only in cinemas, then replaces it with the first 1080p release.

The activity list says when a fallback was used ("nothing acceptable to the "1080p" profile, so ... was grabbed with the fallback profile "Cinema recordings""). In the release list of a title's interactive search, each result has `acceptedBy` (`{profileId, profileName, fallback}`, left out when no profile accepts it), and a release only a fallback accepts is explained as "allowed only as a fallback (...)".

## Upgrading from an older version

Earlier versions shipped three presets: "Up to 1080p", "Ultra-HD (up to 2160p)" and "Any". The first time a newer version starts:

- An old preset you never changed is updated in place to its new equivalent: "Up to 1080p" becomes **1080p**, "Ultra-HD (up to 2160p)" becomes **4K & over**, and "Any" gets the new "Any" settings. Titles using it, and the default profile setting, keep pointing at it.
- An old preset you edited is left exactly as you made it.
- Any of the current presets that is still missing by name is created (so if you edited "Up to 1080p", you get a new "1080p" beside it).

This happens once. If you later delete a preset, it is not brought back.

**Cinema recordings** was added later. An install that already had the other presets gets it created once, on the first start of the version that added it; the presets you already have, edited, untouched or deleted, stay as they are.

Versions released before the presets became strict had **1080p** fall back to 720p and **4K & over** fall back to 1080p. If you never edited those two, they are updated to the strict versions automatically; if you edited them, they stay as you made them.
