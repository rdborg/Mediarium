// People whose ideas and bug reports shaped Mediarium, shown on the About
// and credits page and kept in step with docs/community.md and the
// changelog. Newest first within each person; add new people at the end.
import { t } from './i18n.ts'

export interface Helper {
  name: string
  where: 'Reddit' | 'GitHub'
  href: string
  ideas: string[]
}

const reddit = (name: string) => `https://www.reddit.com/user/${name}`
const github = (name: string) => `https://github.com/${name}`

export function helpers(): Helper[] {
  return [
    { name: 'ShuanYu', where: 'Reddit', href: reddit('ShuanYu'), ideas: [t('Different quality profiles for movies and TV, and finding missing episodes one season at a time')] },
    { name: 'troelsdk', where: 'Reddit', href: reddit('troelsdk'), ideas: [t('Finding that the Sonarr-compatible episode list ignored the season asked for')] },
    { name: 'Embyverse', where: 'Reddit', href: reddit('Embyverse'), ideas: [t('Custom formats with TRaSH Guides import'), t('Keeping two qualities of one title')] },
    {
      name: '12_nick_12',
      where: 'Reddit',
      href: reddit('12_nick_12'),
      ideas: [t('Never download season packs'), t('A webhook for media servers without their own support'), t('Connecting apps made for Radarr, Sonarr and Lidarr')],
    },
    { name: 'paulojf', where: 'Reddit', href: reddit('paulojf'), ideas: [t('Real-Debrid and TorBox')] },
    { name: 'evilpig', where: 'Reddit', href: reddit('evilpig'), ideas: [t('Real-Debrid and TorBox')] },
    { name: 'this_for_loona', where: 'Reddit', href: reddit('this_for_loona'), ideas: [t('Sending traffic through a proxy')] },
    { name: 'FilesFromTheVoid', where: 'Reddit', href: reddit('FilesFromTheVoid'), ideas: [t('Keeping quality settings simple, with bad releases skipped out of the box')] },
    { name: 'Wolfspider_79', where: 'Reddit', href: reddit('Wolfspider_79'), ideas: [t('Stop monitoring once a title is complete'), t('A live connection with Seerr')] },
    { name: 'Wiwer', where: 'Reddit', href: reddit('Wiwer'), ideas: [t('Repairing releases posted with random file names'), t('Checking folders can be written before downloading'), t('Searching right away after switching to a higher quality')] },
    { name: 'Dry_Register_5812', where: 'Reddit', href: reddit('Dry_Register_5812'), ideas: [t('Cinema recordings named HDCAMRip slipping past a WEB-DL only profile')] },
    { name: 'goneturbo', where: 'GitHub', href: github('goneturbo'), ideas: [t('WireGuard configs that name the server by host name')] },
    { name: 'Hoopes80', where: 'GitHub', href: github('Hoopes80'), ideas: [t('Clearer errors when importing through Cloudflare'), t('Finding shows with an apostrophe in their name'), t('A Usenet indexer that turns Mediarium away with a Cloudflare page')] },
    { name: 'Big_Dragonfruit9719', where: 'Reddit', href: reddit('Big_Dragonfruit9719'), ideas: [t('Torrents with UDP trackers crashing while the VPN is on')] },
    { name: 'SarSha', where: 'Reddit', href: reddit('SarSha'), ideas: [t('More subtitle sources')] },
    { name: 'userkbmsr-png', where: 'GitHub', href: github('userkbmsr-png'), ideas: [t('Subtitles from subs.ro and other sites, through add-ons')] },
    { name: 'msholly', where: 'GitHub', href: github('msholly'), ideas: [t('Adding a media server by pasting the address from the browser')] },
    { name: 'TryToTilt', where: 'GitHub', href: github('TryToTilt'), ideas: [t('Repairing Usenet releases that come with only PAR2 recovery volumes'), t('A speed schedule, a weekly summary and a page-turn animation in the reader, ideas from their fork')] },
    { name: 'DerGlordon', where: 'Reddit', href: reddit('DerGlordon'), ideas: [t('A custom format condition for the original language of a film or show')] },
    { name: 'xrandallx1', where: 'GitHub', href: github('xrandallx1'), ideas: [t('Silo as a media server'), t('IPTorrents as a built-in site')] },
    { name: 'DFlexy', where: 'GitHub', href: github('DFlexy'), ideas: [t('Mediarium in Brazilian Portuguese'), t('Adding your own indexer definition files'), t('Waiting longer for slow Prowlarr feeds'), t('The import explanations in your language')] },
    { name: 'FACM-PF', where: 'GitHub', href: github('FACM-PF'), ideas: [t('Mediarium in European Portuguese')] },
    { name: 'PauloJf', where: 'GitHub', href: github('PauloJf'), ideas: [t('The WireGuard preshared key and pasting a whole config'), t('qBittorrent-compatible download clients'), t('Sorting and filtering the release lists, trailers in a pop-up, subtitles in the player and more detail on downloads'), t('Kobo sync and an OPDS catalogue for books')] },
    { name: 'joshpennington', where: 'Reddit', href: reddit('joshpennington'), ideas: [t('More than one folder per kind of media')] },
    { name: 'blind0wl', where: 'GitHub', href: github('blind0wl'), ideas: [t('A Download clients tab with what the clients are doing'), t('Choosing when to give up on a stalled torrent')] },
    { name: '12nick12', where: 'GitHub', href: github('12nick12'), ideas: [t('Password-protected downloads'), t('Sending a download to the front of the line, picking several downloads at once and starting newer releases first')] },
    { name: 'jdamner', where: 'GitHub', href: github('jdamner'), ideas: [t('Keeping settings in one file for git or a script')] },
    { name: 'anguyen1776', where: 'GitHub', href: github('anguyen1776'), ideas: [t('Different quality profiles for movies and TV, and finding missing episodes one season at a time')] },
    { name: 'varnakube', where: 'GitHub', href: github('varnakube'), ideas: [t('Picking a book by hand when it is not found during an import'), t("Setting a book's cover by hand")] },
    { name: 'SanketBaraiya', where: 'GitHub', href: github('SanketBaraiya'), ideas: [t('Importing anime that is numbered by TheTVDB seasons, such as One Piece'), t('Knaben as a built-in site')] },
    { name: 'nexus4004', where: 'GitHub', href: github('nexus4004'), ideas: [t("Showing the folder where a title's files go"), t('Pointing a title at a file or folder you renamed'), t('Subtitles in Croatian and Serbian'), t('Rescanning a movie that has no file'), t('Library entries in the left menu'), t('The busy message as a small notice in the corner'), t('Preferring a season pack once a season has ended'), t('Importing your Simkl lists'), t('The total speed and what is left in each download client'), t('The service a subtitle came from, shown in Activity')] },
    { name: 'Jisevind', where: 'GitHub', href: github('Jisevind'), ideas: [t('Keeping the original file names')] },
    { name: 'franciscojnneto', where: 'GitHub', href: github('franciscojnneto'), ideas: [t('Pages for cast and crew')] },
    { name: 'Bayharbourbutcher1', where: 'GitHub', href: github('Bayharbourbutcher1'), ideas: [t('Sending the seeding rules to an outside torrent client')] },
    { name: 'mirisbowring', where: 'GitHub', href: github('mirisbowring'), ideas: [t('Replacing a movie with a smaller version'), t('Searching under another title of a movie or show, such as the German one'), t('Extra categories'), t('The category shown under each book release'), t('Episode by episode meaning one episode at a time'), t('Searching titles with umlauts in both spellings'), t('A search box in Choose a release for your own words')] },
    { name: 'spamoom', where: 'GitHub', href: github('spamoom'), ideas: [t('Sending indexer traffic through the VPN, with the code to do it')] },
    { name: 'acortespr06', where: 'GitHub', href: github('acortespr06'), ideas: [t('Transmission as a download client')] },
    { name: 'MrBanditt', where: 'GitHub', href: github('MrBanditt'), ideas: [t('Ended and continuing shows, file sizes for episodes and seasons, an age column in the release list, a TMDB link on title pages and sorting titles without “The”')] },
    { name: 'barnetfan', where: 'Reddit', href: reddit('barnetfan'), ideas: [t('Telling apart two artists with the same name when scanning your music folder')] },
    { name: 'thegliffy', where: 'GitHub', href: github('thegliffy'), ideas: [t('Audiobooks that are on Audible but not on Open Library'), t('Books with accents in their names'), t('Running the books that were not found again')] },
    {
      name: 'Apostol6',
      where: 'GitHub',
      href: github('Apostol6'),
      ideas: [t('Toloka.to as a built-in site'), t('Prowlarr feed addresses that end in /api'), t('Sites that write dates in their own way'), t('Torrent sites from Prowlarr added as Usenet'), t('Titles and descriptions in your language')],
    },
    { name: 'Marty-SK', where: 'GitHub', href: github('Marty-SK'), ideas: [t('Seeing whether the VPN really passes traffic, with a Test button')] },
    { name: 'memnos', where: 'GitHub', href: github('memnos'), ideas: [t('Naming new movie and show folders and files in the info language, written as a pull request'), t('Sending UDP trackers and DHT through the built-in VPN, written as a pull request')] },
    { name: 'blind0wl', where: 'GitHub', href: github('blind0wl'), ideas: [t('Torrent links from Prowlarr that redirect to a magnet link, written as a pull request')] },
    { name: 'Joshuancsu', where: 'Reddit', href: reddit('Joshuancsu'), ideas: [t('Saying why a torrent was given up')] },
  ]
}
