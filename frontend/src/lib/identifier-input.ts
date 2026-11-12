/**
 * Props for an input that holds an identifier (a URL, host, path, code, model
 * id), never prose. They stop a phone keyboard capitalising the first letter,
 * autocorrecting the value or underlining it as a spelling mistake.
 */
export const identifierInputProps = {
  spellCheck: false,
  autoCapitalize: 'none',
  autoCorrect: 'off',
} as const

/** Chrome ignores autocomplete="off" on password fields and would offer the saved sign-in; "new-password" stops that. */
export const secretInputProps = {
  autoComplete: 'new-password',
  spellCheck: false,
} as const
