import type { ReactNode } from 'react'
import HelpLink from './HelpLink'
import InfoTip from './InfoTip'
import './InfoTip.css'

// A label with a tip (or a link to the guide) after it:
//
//   <TipLabel text={t('What it does and what changes when you set it.')}>{t('Label')}</TipLabel>
//   <TipLabel help="quality#custom-formats">{t('Custom formats')}</TipLabel>
//
// The last word and the button are kept together, so the button never wraps
// onto a line of its own, in a narrow column or on a phone. Give it the label
// as plain text where you can; any other content is kept on one line.
// `text` is the sentence of an InfoTip (with an optional `learnMore` guide
// topic), `help` makes a plain "?" link to a guide topic instead.
export default function TipLabel({ children, text, learnMore, help, label }: { children: ReactNode; text?: string; learnMore?: string; help?: string; label?: string }) {
  const button = help ? <HelpLink topic={help} label={label} /> : <InfoTip text={text ?? ''} learnMore={learnMore} label={label} />
  if (typeof children === 'string') {
    const m = /^([\s\S]*?)(\S+)\s*$/.exec(children)
    if (m) {
      // Chinese and Japanese have no spaces: a whole sentence is one "word", and keeping it on one line would
      // push the page wider than a phone. Only its last character stays with the button.
      const letters = Array.from(m[2])
      const long = letters.length > 16
      if (m[1] || long) {
        return (
          <span className="has-tip">
            {m[1]}
            {long && letters.slice(0, -1).join('')}
            <span className="tip-last">
              {long ? letters[letters.length - 1] : m[2]}
              {button}
            </span>
          </span>
        )
      }
    }
  }
  return (
    <span className="has-tip">
      <span className="tip-last">
        {children}
        {button}
      </span>
    </span>
  )
}
