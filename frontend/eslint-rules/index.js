import textSizeScale from './text-size-scale.js';
import noArbitraryColor from './no-arbitrary-color.js';
import noWidgetInUiText from './no-widget-in-ui-text.js';
import noZonelessTime from './no-zoneless-time.js';

// Local ESLint plugin, registered as `helm` in eslint.config.js. Each rule
// turns a mechanical part of AGENTS.md into a lint error.
export default {
  rules: {
    'text-size-scale': textSizeScale,
    'no-arbitrary-color': noArbitraryColor,
    'no-widget-in-ui-text': noWidgetInUiText,
    'no-zoneless-time': noZonelessTime,
  },
};
