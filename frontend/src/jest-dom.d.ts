// jest-dom's own vitest typings augment `Assertion<T>`, which no longer merges
// with vitest 5's `Assertion<R, T>`. Register its matchers on `Matchers`, the
// interface vitest 5 provides for custom matchers, instead.
import type { TestingLibraryMatchers } from '@testing-library/jest-dom/matchers';

declare module 'vitest' {
  interface Matchers<R, T> extends TestingLibraryMatchers<T, R> {}
}
