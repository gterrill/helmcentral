import rule from './no-zoneless-time.js';
import { ruleTester } from './tester.js';

ruleTester.run('helm/no-zoneless-time', rule, {
  valid: [
    { code: `d.toLocaleTimeString('en-US', { hour: 'numeric', timeZone })` },
    { code: `d.toLocaleDateString('en-US', { weekday: 'long', timeZone: 'UTC' })` },
    { code: `d.toLocaleString('en-GB', { timeZone: tz, hour: '2-digit' })` },
    { code: `count.toLocaleString()` },
    { code: `tokens.toLocaleString('en-US', { maximumFractionDigits: 1 })` },
    { code: `new Intl.DateTimeFormat('en-GB', { timeZone, hour: '2-digit' })` },
    { code: `new Intl.DateTimeFormat('en-US', { ...options, timeZone })` },
    { code: `new Intl.DateTimeFormat('en-GB', a ? { timeZone, hour: '2-digit' } : { timeZone, day: 'numeric' })` },
    { code: `d.getUTCHours() + d.getUTCDate()` },
    { code: `map.get('a')` },
    { code: `// helm-browser-zone: a document records when the operator acted.\nconst t = d.getHours()` },
    { code: `// helm-browser-zone: device clock by design.\nexport function f(d) { return d.toLocaleTimeString() }` },
    { code: `function f(d) {\n  // helm-browser-zone: device clock by design.\n  return d.toLocaleTimeString([], { hour: '2-digit' })\n}` },
  ],
  invalid: [
    { code: `d.toLocaleTimeString('en-US', { hour: 'numeric' })`, errors: 1 },
    { code: `d.toLocaleTimeString()`, errors: 1 },
    { code: `d.toLocaleDateString(undefined, { day: 'numeric' })`, errors: 1 },
    { code: `new Date(x).toLocaleString()`, errors: 1 },
    { code: `parsed.toLocaleString()`, errors: 1 },
    { code: `row.seenAt.toLocaleString()`, errors: 1 },
    { code: `d.toLocaleString('en-GB', { hour: '2-digit', minute: '2-digit' })`, errors: 1 },
    { code: `new Intl.DateTimeFormat([], { hour: '2-digit' })`, errors: 1 },
    { code: `new Intl.DateTimeFormat('en-US', options)`, errors: 1 },
    { code: `Intl.DateTimeFormat('en-US')`, errors: 1 },
    { code: `new Intl.DateTimeFormat('en-GB', a ? { timeZone, hour: '2-digit' } : { day: 'numeric' })`, errors: 1 },
    { code: `d.getHours()`, errors: 1 },
    { code: `d.getMinutes() + d.getDate()`, errors: 2 },
    { code: `d.getDay()`, errors: 1 },
    { code: `d.getMonth() + d.getFullYear()`, errors: 2 },
    // The opt-out needs a reason, and sits on the line before the statement.
    { code: `// helm-browser-zone:\nconst t = d.getHours()`, errors: 1 },
    { code: `// helm-browser-zone: reason\n\nconst a = 1\nconst t = d.getHours()`, errors: 1 },
  ],
});
