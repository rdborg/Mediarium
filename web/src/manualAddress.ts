// The address pasted in "Add a download by hand", made ready to check and send.

// cleanAddress trims the address and, for a magnet link, writes each space as
// %20. Many sites and apps copy magnet links with plain spaces in the name
// (&dn=Some Movie (2023) 1080p), which a magnet link can't hold; the hash is
// what matters, so the link is otherwise left as it was pasted.
export function cleanAddress(address: string): string {
  const a = address.trim()
  return /^magnet:/i.test(a) ? a.replace(/ /g, '%20') : a
}
