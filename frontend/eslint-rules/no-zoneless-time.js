// Every time the operator reads is in the boat's time zone (ADR 0230), so a
// Date must never be written in the zone of whatever device is looking at it.
// This rule rejects the calls that do that:
//
//   - toLocaleDateString / toLocaleTimeString without an options object that
//     names a `timeZone`; toLocaleString the same way, when it is plainly on a
//     Date (`new Date(x).toLocaleString()`, a receiver named like a date, or
//     options holding date or time fields). Without type information a bare
//     `n.toLocaleString()` on a number cannot be told apart, and is left alone.
//   - `new Intl.DateTimeFormat(...)` / `Intl.DateTimeFormat(...)` whose options
//     do not name a `timeZone`.
//   - the local-zone getters getHours, getMinutes, getSeconds, getDate,
//     getDay, getMonth and getFullYear.
//
// A reading that is deliberately on the device's clock (a document timestamp
// records when the operator acted, not where the boat was) opts out with a
// comment on the line before the statement, and the comment must say why:
//
//   // helm-browser-zone: <reason>

const LOCAL_GETTERS = new Set(['getHours', 'getMinutes', 'getSeconds', 'getDate', 'getDay', 'getMonth', 'getFullYear']);
const ALWAYS_DATE = new Set(['toLocaleDateString', 'toLocaleTimeString']);
const DATE_OPTION_KEYS = new Set([
  'hour', 'minute', 'second', 'day', 'month', 'year', 'weekday', 'era', 'dateStyle', 'timeStyle', 'hour12', 'hourCycle',
  'timeZoneName', 'dayPeriod', 'fractionalSecondDigits',
]);
const DATE_NAME = /(date|time|stamp|parsed|seen|^at$|^d$)/i;
const OPT_OUT = /^\s*helm-browser-zone:\s*\S/;

function keyName(prop) {
  if (prop.type !== 'Property' || prop.computed) return undefined;
  if (prop.key.type === 'Identifier') return prop.key.name;
  if (prop.key.type === 'Literal') return String(prop.key.value);
  return undefined;
}

function optionKeys(args) {
  const keys = new Set();
  for (const arg of args) {
    if (arg.type !== 'ObjectExpression') continue;
    for (const prop of arg.properties) {
      const name = keyName(prop);
      if (name) keys.add(name);
    }
  }
  return keys;
}

// An options argument names a zone when it is an object with a `timeZone` key,
// or a conditional whose branches all are.
function namesTimeZone(arg) {
  if (!arg) return false;
  if (arg.type === 'ObjectExpression') return arg.properties.some((p) => keyName(p) === 'timeZone');
  if (arg.type === 'ConditionalExpression') return namesTimeZone(arg.consequent) && namesTimeZone(arg.alternate);
  return false;
}

function receiverLooksLikeDate(obj) {
  if (obj.type === 'NewExpression') return obj.callee.type === 'Identifier' && obj.callee.name === 'Date';
  if (obj.type === 'Identifier') return DATE_NAME.test(obj.name);
  if (obj.type === 'MemberExpression' && !obj.computed && obj.property.type === 'Identifier') return DATE_NAME.test(obj.property.name);
  return false;
}

function isIntlDateTimeFormat(callee) {
  return (
    callee.type === 'MemberExpression' &&
    !callee.computed &&
    callee.object.type === 'Identifier' &&
    callee.object.name === 'Intl' &&
    callee.property.type === 'Identifier' &&
    callee.property.name === 'DateTimeFormat'
  );
}

const STATEMENT_PARENTS = new Set(['Program', 'BlockStatement', 'SwitchCase', 'StaticBlock']);

/** @type {import('eslint').Rule.RuleModule} */
export default {
  meta: {
    type: 'problem',
    schema: [],
    messages: {
      zoneless:
        '{{what}} reads the device\'s clock, not the boat\'s (ADR 0230). Pass the boat\'s zone through lib/vessel-time, or put "// helm-browser-zone: <reason>" on the line before if this really is the device\'s time.',
    },
  },
  create(context) {
    const sourceCode = context.sourceCode;

    // The comment may sit before the statement holding the call or before any
    // statement around it (the function, the export), so one comment can cover
    // a helper that is device-clock throughout.
    function optedOut(node) {
      for (let n = node; n.parent; n = n.parent) {
        if (!STATEMENT_PARENTS.has(n.parent.type)) continue;
        const stmt = n.parent.type === 'ExportNamedDeclaration' || n.parent.type === 'ExportDefaultDeclaration' ? n.parent : n;
        if (sourceCode.getCommentsBefore(stmt).some((c) => OPT_OUT.test(c.value))) return true;
      }
      return false;
    }

    function report(node, what) {
      if (optedOut(node)) return;
      context.report({ node, messageId: 'zoneless', data: { what } });
    }

    function checkIntl(node) {
      if (!isIntlDateTimeFormat(node.callee)) return;
      if (!node.arguments.slice(1).some(namesTimeZone)) report(node, 'Intl.DateTimeFormat without a timeZone');
    }

    return {
      NewExpression: checkIntl,
      CallExpression(node) {
        const callee = node.callee;
        if (callee.type !== 'MemberExpression' || callee.computed || callee.property.type !== 'Identifier') return;
        const name = callee.property.name;
        if (isIntlDateTimeFormat(callee)) return checkIntl(node);

        if (LOCAL_GETTERS.has(name)) {
          if (node.arguments.length === 0) report(node, `${name}()`);
          return;
        }
        if (ALWAYS_DATE.has(name)) {
          if (!node.arguments.some(namesTimeZone)) report(node, `${name} without a timeZone`);
          return;
        }
        if (name === 'toLocaleString') {
          const keys = optionKeys(node.arguments);
          if (node.arguments.some(namesTimeZone)) return;
          const dateOptions = [...keys].some((k) => DATE_OPTION_KEYS.has(k));
          if (dateOptions || (node.arguments.length === 0 && receiverLooksLikeDate(callee.object))) {
            report(node, 'toLocaleString on a Date without a timeZone');
          }
        }
      },
    };
  },
};
