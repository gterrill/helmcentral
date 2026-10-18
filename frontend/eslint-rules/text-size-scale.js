import { classTextVisitors, collectLiterals, jsxAttrName, propertyName } from './shared.js';

// text-[Npx] and text-[Nrem]; em is relative to the parent and is skipped.
const ARBITRARY_SIZE = /(?<![\w-])text-\[(\d*\.?\d+)(px|rem)\]/g;
const SIZE_VALUE = /^(\d*\.?\d+)(px|rem)?$/;

const MESSAGE =
  'Values use text-xs or larger; 10px is for uppercase micro-labels and 9px for map/marker annotation. 11px is retired (AGENTS.md "Micro-Typography Scale").';

function toPx(num, unit) {
  return unit === 'rem' ? Number(num) * 16 : Number(num);
}

function allowed(px) {
  // 0 hides the text rather than sizing it.
  return px === 0 || px === 9 || px === 10 || px >= 12;
}

/** @type {import('eslint').Rule.RuleModule} */
export default {
  meta: { type: 'problem', schema: [], messages: { size: MESSAGE } },
  create(context) {
    function checkValue(node, value) {
      let px;
      if (typeof value === 'number') px = value;
      else {
        const m = SIZE_VALUE.exec(value.trim());
        if (m) px = toPx(m[1], m[2]);
      }
      if (px !== undefined && !allowed(px)) context.report({ node, messageId: 'size' });
    }

    return {
      ...classTextVisitors((node, text) => {
        for (const m of text.matchAll(ARBITRARY_SIZE)) {
          if (!allowed(toPx(m[1], m[2]))) context.report({ node, messageId: 'size' });
        }
      }),
      JSXAttribute(node) {
        if (jsxAttrName(node) !== 'fontSize' || !node.value) return;
        for (const { node: n, value } of collectLiterals(node.value)) checkValue(n, value);
      },
      Property(node) {
        if (propertyName(node) !== 'fontSize') return;
        // { fontSize: 11 } and the destructuring default { fontSize = 11 }.
        const v = node.value.type === 'AssignmentPattern' ? node.value.right : node.value;
        for (const { node: n, value } of collectLiterals(v)) checkValue(n, value);
      },
    };
  },
};
