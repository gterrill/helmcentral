// Helpers shared by the helm rules.

// Calls whose arguments are class lists: cn('a', cond && 'b').
const CLASS_FNS = new Set(['cn', 'clsx', 'classnames', 'classNames', 'twMerge', 'cva']);

function calleeName(callee) {
  return callee.type === 'Identifier' ? callee.name : undefined;
}

/**
 * Every string/number literal and template quasi reachable in an expression,
 * going through conditionals, logical and + expressions, arrays, and
 * cn()/clsx() call arguments. Identifiers and other calls are code and are
 * never entered. Object literals are entered (keys and values) only inside a
 * class-list call, where cn({ 'a b': cond }) is a class list.
 * Returns [{ node, value }] where value is a string, or a number for a
 * numeric Literal.
 */
export function collectLiterals(node) {
  const out = [];
  walk(node, out, false);
  return out;
}

function walk(node, out, inClassList) {
  if (!node) return;
  switch (node.type) {
    case 'JSXExpressionContainer':
    case 'TSAsExpression':
    case 'TSSatisfiesExpression':
    case 'TSNonNullExpression':
      walk(node.expression, out, inClassList);
      return;
    case 'Literal':
      if (typeof node.value === 'string' || typeof node.value === 'number') {
        out.push({ node, value: node.value });
      }
      return;
    case 'TemplateLiteral':
      for (const q of node.quasis) out.push({ node: q, value: q.value.cooked ?? q.value.raw });
      for (const e of node.expressions) walk(e, out, inClassList);
      return;
    case 'ConditionalExpression':
      walk(node.consequent, out, inClassList);
      walk(node.alternate, out, inClassList);
      return;
    case 'LogicalExpression':
      walk(node.left, out, inClassList);
      walk(node.right, out, inClassList);
      return;
    case 'BinaryExpression':
      if (node.operator === '+') {
        walk(node.left, out, inClassList);
        walk(node.right, out, inClassList);
      }
      return;
    case 'ArrayExpression':
      for (const el of node.elements) walk(el, out, inClassList);
      return;
    case 'ObjectExpression':
      if (!inClassList) return;
      for (const p of node.properties) {
        if (p.type !== 'Property') continue;
        if (p.key.type === 'Literal' && typeof p.key.value === 'string') {
          out.push({ node: p.key, value: p.key.value });
        }
        walk(p.value, out, true);
      }
      return;
    case 'CallExpression':
      if (CLASS_FNS.has(calleeName(node.callee))) {
        for (const arg of node.arguments) walk(arg, out, true);
      }
      return;
    default:
      return;
  }
}

/** String-valued entries only. */
export function collectStrings(node) {
  return collectLiterals(node).filter((e) => typeof e.value === 'string');
}

/**
 * Visitors that hand every string Literal and template quasi in the file to
 * check(node, text). Class lists can sit anywhere (cn(), const maps, props),
 * so the class-token rules look at all of them.
 */
export function classTextVisitors(check) {
  return {
    Literal(node) {
      if (typeof node.value === 'string') check(node, node.value);
    },
    TemplateElement(node) {
      check(node, node.value.cooked ?? node.value.raw);
    },
  };
}

export function jsxAttrName(attr) {
  return attr.name.type === 'JSXIdentifier' ? attr.name.name : undefined;
}

/** Name of an object property written as `name:` or `'name':`; undefined if computed. */
export function propertyName(node) {
  if (node.computed) return undefined;
  if (node.key.type === 'Identifier') return node.key.name;
  if (node.key.type === 'Literal') return String(node.key.value);
  return undefined;
}
