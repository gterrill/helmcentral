import { classTextVisitors, collectStrings, jsxAttrName, propertyName } from './shared.js';

const UTILITIES =
  'bg|text|border(?:-[xytrblse])?|fill|stroke|ring(?:-offset)?|from|via|to|outline|shadow|drop-shadow|decoration|divide|accent|caret|placeholder';
// An arbitrary value: utility-[...]. No whitespace inside (spaces are _).
const CLASS_TOKEN = new RegExp(`(?<![\\w-])(?:${UTILITIES})-\\[([^\\]\\s]*)\\]`, 'g');
// A hex colour anywhere inside an arbitrary value (bg-[color:#fff],
// shadow-[0_0_4px_#fff], bg-[radial-gradient(#fff_0%,...)]), but not url(#id).
const HEX_IN_VALUE = /(?:^|[_:,(])(?<!url\()#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})(?![0-9a-zA-Z])/;
const COLOR_FN_IN_VALUE = /(?:rgb|rgba|hsl|hsla|oklch)\(/i;

const COLOR_LITERAL = /^\s*(?:#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})|(?:rgb|rgba|hsl|hsla)\(.*\))\s*$/i;
const COLOR_ATTRS = new Set(['fill', 'stroke', 'color', 'stopColor']);
const COLOR_PROPS = new Set(['color', 'backgroundColor', 'background', 'borderColor', 'fill', 'stroke', 'outlineColor', 'stopColor']);

const MESSAGE =
  'Use a semantic token (bg-card, text-muted-foreground, text-gauge-primary, hsl(var(--...)) for SVG) or, for alert semantics only, the Tailwind palette (CODING_STANDARDS.md "Anti-Slop Constraints").';

function isRawColor(text) {
  return COLOR_LITERAL.test(text) && !text.includes('var(--');
}

/** @type {import('eslint').Rule.RuleModule} */
export default {
  meta: { type: 'problem', schema: [], messages: { color: MESSAGE } },
  create(context) {
    function report(node) {
      context.report({ node, messageId: 'color' });
    }
    return {
      ...classTextVisitors((node, text) => {
        for (const m of text.matchAll(CLASS_TOKEN)) {
          const v = m[1];
          if (HEX_IN_VALUE.test(v) || (COLOR_FN_IN_VALUE.test(v) && !v.includes('var(--'))) report(node);
        }
      }),
      JSXAttribute(node) {
        if (!COLOR_ATTRS.has(jsxAttrName(node)) || !node.value) return;
        for (const { node: n, value } of collectStrings(node.value)) if (isRawColor(value)) report(n);
      },
      Property(node) {
        if (!COLOR_PROPS.has(propertyName(node))) return;
        for (const { node: n, value } of collectStrings(node.value)) if (isRawColor(value)) report(n);
      },
    };
  },
};
