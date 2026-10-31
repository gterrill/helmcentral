import { collectStrings, jsxAttrName, propertyName } from './shared.js';

const WIDGET = /\bwidgets?\b/i;
const TEXT_ATTRS = new Set([
  'aria-label',
  'aria-description',
  'aria-roledescription',
  'aria-valuetext',
  'title',
  'placeholder',
  'alt',
  'label',
  'description',
  'subtitle',
  'emptyText',
  'heading',
  'helperText',
]);
const PROMISE_KEYS = ['loading', 'success', 'error'];
const TOAST_METHODS = new Set(['success', 'error', 'warning', 'info', 'message', 'loading', 'promise']);

const MESSAGE =
  'The operator\'s word is "tile"; "widget" is the config-record layer only (CODING_STANDARDS.md "Tiles And Widgets").';

function isToastCall(callee) {
  if (callee.type === 'Identifier') return callee.name === 'toast';
  return (
    callee.type === 'MemberExpression' &&
    !callee.computed &&
    callee.object.type === 'Identifier' &&
    callee.object.name === 'toast' &&
    callee.property.type === 'Identifier' &&
    TOAST_METHODS.has(callee.property.name)
  );
}

/** @type {import('eslint').Rule.RuleModule} */
export default {
  meta: { type: 'problem', schema: [], messages: { widget: MESSAGE } },
  create(context) {
    function check(expr) {
      for (const { node, value } of collectStrings(expr)) {
        if (WIDGET.test(value)) context.report({ node, messageId: 'widget' });
      }
    }
    return {
      JSXText(node) {
        if (WIDGET.test(node.value)) context.report({ node, messageId: 'widget' });
      },
      JSXExpressionContainer(node) {
        // Children only; attribute values are handled by JSXAttribute.
        if (node.parent.type === 'JSXElement' || node.parent.type === 'JSXFragment') check(node.expression);
      },
      JSXAttribute(node) {
        if (TEXT_ATTRS.has(jsxAttrName(node)) && node.value) check(node.value);
      },
      CallExpression(node) {
        if (!isToastCall(node.callee)) return;
        const [first, options] = node.arguments;
        const isPromise = node.callee.type === 'MemberExpression' && node.callee.property.name === 'promise';
        // toast.promise's first argument is the promise, not text.
        if (!isPromise) check(first);
        if (options && options.type === 'ObjectExpression') {
          const keys = isPromise ? PROMISE_KEYS : ['description'];
          for (const p of options.properties) {
            if (p.type === 'Property' && keys.includes(propertyName(p))) check(p.value);
          }
        }
      },
    };
  },
};
