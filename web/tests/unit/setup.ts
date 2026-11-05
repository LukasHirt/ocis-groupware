import { config } from '@vue/test-utils'
import { designSystemStubs } from './stubs/designSystem'

// oCIS Web registers the design-system components globally. The tests
// register stand-ins with the same contract, so a component can use <oc-*>
// without the design system being part of the tests.
config.global.components = { ...config.global.components, ...designSystemStubs }
