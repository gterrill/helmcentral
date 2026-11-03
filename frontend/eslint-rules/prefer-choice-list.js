import { collectStrings, jsxAttrName } from './shared.js';

const CHOICE_TYPES = new Set(['checkbox', 'radio']);
// The ui wrappers, and the Base UI modules they wrap.
const PRIMITIVE_MODULE = /(^|\/)ui\/(checkbox|radio-group)$|^@base-ui\/react\/(checkbox|checkbox-group|radio|radio-group)$/;
// The Base UI barrel exports many primitives; only these are choices.
const BARREL_MODULE = '@base-ui/react';
const BARREL_CHOICES = new Set(['Checkbox', 'CheckboxGroup', 'Radio', 'RadioGroup']);

const MESSAGES = {
  input:
    'Build a choice from ChoiceList or ChoiceCheckbox (components/patterns/choice-list), not a hand-made <input type="checkbox|radio"> (CODING_STANDARDS.md "CRUD Pattern Library").',
  primitive:
    'Build a choice from ChoiceList or ChoiceCheckbox (components/patterns/choice-list), not a Checkbox or Radio primitive from components/ui or Base UI (CODING_STANDARDS.md "CRUD Pattern Library").',
};

function isChoiceInput(node) {
  if (node.name.type !== 'JSXIdentifier' || node.name.name !== 'input') return false;
  const type = node.attributes.find((a) => a.type === 'JSXAttribute' && jsxAttrName(a) === 'type');
  if (!type || !type.value) return false;
  return collectStrings(type.value).some(({ value }) => CHOICE_TYPES.has(value));
}

/** @type {import('eslint').Rule.RuleModule} */
export default {
  meta: { type: 'suggestion', schema: [], messages: MESSAGES },
  create(context) {
    // Local names bound to the choice primitives, directly or as a namespace.
    const primitives = new Set();
    return {
      ImportDeclaration(node) {
        const source = String(node.source.value);
        if (PRIMITIVE_MODULE.test(source)) {
          for (const s of node.specifiers) primitives.add(s.local.name);
        } else if (source === BARREL_MODULE) {
          for (const s of node.specifiers) {
            if (s.type === 'ImportSpecifier' && BARREL_CHOICES.has(s.imported.name)) primitives.add(s.local.name);
          }
        }
      },
      JSXOpeningElement(node) {
        if (isChoiceInput(node)) {
          context.report({ node, messageId: 'input' });
          return;
        }
        // <Checkbox />, or <Checkbox.Root /> / <C.Checkbox />: the root name decides.
        let name = node.name;
        while (name.type === 'JSXMemberExpression') name = name.object;
        if (name.type === 'JSXIdentifier' && primitives.has(name.name)) {
          context.report({ node, messageId: 'primitive' });
        }
      },
    };
  },
};
