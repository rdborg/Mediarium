// Opening Mediarium Books: in a window of its own, like a separate app. When
// the browser won't open a window (a blocked pop-up, or a phone), the page
// goes there instead.
export function openBookApp(path = '/bookshelf') {
  const w = window.open(path, 'mediarium-books', 'popup=yes,width=1100,height=1000')
  if (!w) {
    window.location.assign(path)
    return
  }
  w.focus()
}

export const readPath = (id: number) => `/bookshelf/read/${id}`
export const listenPath = (id: number) => `/bookshelf/listen/${id}`
