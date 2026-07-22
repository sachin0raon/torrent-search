// Shared Framer Motion presets so transitions feel consistent across the app.

// A soft, weighty spring (mirrors the CSS cubic-bezier feel).
export const spring = { type: 'spring', stiffness: 320, damping: 32, mass: 0.9 };
export const springSoft = { type: 'spring', stiffness: 220, damping: 30 };

// Fade-up used for cards/rows entering the viewport.
export const fadeUp = {
  initial: { opacity: 0, y: 16, filter: 'blur(6px)' },
  animate: { opacity: 1, y: 0, filter: 'blur(0px)' },
  exit: { opacity: 0, y: -10, filter: 'blur(4px)' },
};

// Staggered container for lists.
export const staggerContainer = {
  animate: { transition: { staggerChildren: 0.05, delayChildren: 0.02 } },
};

// Item used inside a staggered container.
export const staggerItem = {
  initial: { opacity: 0, y: 14, filter: 'blur(5px)' },
  animate: { opacity: 1, y: 0, filter: 'blur(0px)', transition: spring },
  exit: { opacity: 0, y: -8, filter: 'blur(3px)', transition: { duration: 0.18 } },
};
