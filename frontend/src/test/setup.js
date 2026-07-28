import '@testing-library/jest-dom';
import { afterEach } from 'vitest';
import { cleanup } from '@testing-library/react';

// jsdom defaults isSecureContext to false, which makes navigator.clipboard
// unavailable and causes CopyButton to use the execCommand fallback instead.
// Tests mock navigator.clipboard and expect the clipboard path, so force it.
Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true });

afterEach(() => {
  cleanup();
});
