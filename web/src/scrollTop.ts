// The page content scrolls inside the app shell's .content area, not the
// window, so going to the top means scrolling that element (the window too,
// for pages shown outside the shell, like sign-in and the wizard).
export function scrollToTop(smooth = false) {
  const behavior: ScrollBehavior = smooth ? 'smooth' : 'auto'
  document.querySelector('.content')?.scrollTo({ top: 0, behavior })
  window.scrollTo({ top: 0, behavior })
}
