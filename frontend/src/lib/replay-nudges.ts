/** The steps a sign or a camera shot can be moved in time by, earliest first. */
export const NUDGES: readonly { seconds: number; short: string; words: string }[] = [
  { seconds: -60, short: '−1 min', words: '1 minute earlier' },
  { seconds: -10, short: '−10 s', words: '10 seconds earlier' },
  { seconds: 10, short: '+10 s', words: '10 seconds later' },
  { seconds: 60, short: '+1 min', words: '1 minute later' },
]
