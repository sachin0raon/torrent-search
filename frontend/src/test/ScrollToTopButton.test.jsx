import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ScrollToTopButton from '../components/ScrollToTopButton.jsx';

function setScrollY(y) {
  Object.defineProperty(window, 'scrollY', { value: y, writable: true, configurable: true });
  window.dispatchEvent(new Event('scroll'));
}

describe('ScrollToTopButton', () => {
  beforeEach(() => {
    setScrollY(0);
    window.scrollTo = vi.fn();
  });

  afterEach(() => {
    setScrollY(0);
  });

  it('is hidden until the page scrolls past the threshold', () => {
    render(<ScrollToTopButton />);
    expect(screen.queryByRole('button', { name: /scroll to top/i })).not.toBeInTheDocument();
  });

  it('appears once scrolled past the threshold and scrolls to top on click', async () => {
    render(<ScrollToTopButton />);
    setScrollY(500);

    const btn = await screen.findByRole('button', { name: /scroll to top/i });
    await userEvent.click(btn);
    expect(window.scrollTo).toHaveBeenCalledWith(expect.objectContaining({ top: 0 }));
  });

  it('hides again once scrolled back up', async () => {
    render(<ScrollToTopButton />);
    setScrollY(500);
    await screen.findByRole('button', { name: /scroll to top/i });

    setScrollY(0);
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: /scroll to top/i })).not.toBeInTheDocument(),
    );
  });
});
