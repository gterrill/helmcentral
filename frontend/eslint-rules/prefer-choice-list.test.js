import rule from './prefer-choice-list.js';
import { ruleTester } from './tester.js';

ruleTester.run('helm/prefer-choice-list', rule, {
  valid: [
    { code: `const a = <input type="text" />` },
    { code: `const a = <input type="range" />` },
    { code: `const a = <input type={kind} />` },
    { code: `const a = <input />` },
    { code: `const a = <div type="checkbox" />` },
    { code: `const a = <ChoiceList choices={c} value={v} onChange={f} />` },
    { code: `const a = <ChoiceCheckbox label="Live" checked onChange={f} />` },
    { code: `import { Checkbox } from '@/components/ui/checkbox'; const a = 1` },
    // A Checkbox from somewhere else is not the UI primitive.
    { code: `import { Checkbox } from 'other-lib'; const a = <Checkbox />` },
    { code: `const a = <Checkbox />` },
    { code: `import { Switch } from '@base-ui/react/switch'; const a = <Switch.Root />` },
    { code: `import { Button } from '@base-ui/react'; const a = <Button />` },
    { code: `import * as C from '@/components/ui/dialog'; const a = <C.Root />` },
    { code: `import { DropdownMenuRadioGroup } from '@/components/ui/dropdown-menu'; const a = <DropdownMenuRadioGroup />` },
  ],
  invalid: [
    { code: `const a = <input type="checkbox" checked />`, errors: [{ messageId: 'input' }] },
    { code: `const a = <input type="radio" name="x" />`, errors: [{ messageId: 'input' }] },
    { code: `const a = <input type={'checkbox'} />`, errors: [{ messageId: 'input' }] },
    { code: 'const a = <input type={`radio`} />', errors: [{ messageId: 'input' }] },
    {
      code: `import { Checkbox } from '@/components/ui/checkbox'; const a = <Checkbox checked />`,
      errors: [{ messageId: 'primitive' }],
    },
    {
      code: `import { Checkbox as Box } from '@/components/ui/checkbox'; const a = <Box />`,
      errors: [{ messageId: 'primitive' }],
    },
    {
      code: `import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'; const a = <RadioGroup><RadioGroupItem value="a" /></RadioGroup>`,
      errors: [{ messageId: 'primitive' }, { messageId: 'primitive' }],
    },
    {
      code: `import { Checkbox } from '../ui/checkbox'; const a = <Checkbox />`,
      errors: [{ messageId: 'primitive' }],
    },
    {
      code: `import * as C from '@/components/ui/checkbox'; const a = <C.Checkbox />`,
      errors: [{ messageId: 'primitive' }],
    },
    {
      code: `import { Checkbox } from '@base-ui/react/checkbox'; const a = <Checkbox.Root><Checkbox.Indicator /></Checkbox.Root>`,
      errors: [{ messageId: 'primitive' }, { messageId: 'primitive' }],
    },
    {
      code: `import { Checkbox as Base } from '@base-ui/react/checkbox'; const a = <Base.Root />`,
      errors: [{ messageId: 'primitive' }],
    },
    {
      code: `import { Radio } from '@base-ui/react/radio'; import { RadioGroup } from '@base-ui/react/radio-group'; const a = <RadioGroup.Root><Radio.Root value="a" /></RadioGroup.Root>`,
      errors: [{ messageId: 'primitive' }, { messageId: 'primitive' }],
    },
    {
      code: `import { CheckboxGroup } from '@base-ui/react/checkbox-group'; const a = <CheckboxGroup />`,
      errors: [{ messageId: 'primitive' }],
    },
    {
      code: `import { Checkbox, Radio, RadioGroup, CheckboxGroup } from '@base-ui/react'; const a = [<Checkbox.Root />, <Radio.Root />, <RadioGroup />, <CheckboxGroup />]`,
      errors: [{ messageId: 'primitive' }, { messageId: 'primitive' }, { messageId: 'primitive' }, { messageId: 'primitive' }],
    },
  ],
});
