import { useState } from 'react';
import { Check, Copy } from 'lucide-react';

// Copies a magnet link to the clipboard and shows a transient "Copied" state.
export default function CopyButton({ value, label = 'Copy magnet', className = 'btn-solid' }) {
  const [copied, setCopied] = useState(false);

  async function onCopy() {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(value);
      } else {
        // Fallback for http:// (non-secure) contexts: create an off-screen
        // textarea, select its content, and use the legacy execCommand.
        const ta = document.createElement('textarea');
        ta.value = value;
        ta.style.cssText = 'position:fixed;top:0;left:0;opacity:0;pointer-events:none';
        document.body.appendChild(ta);
        ta.focus();
        ta.select();
        document.execCommand('copy');
        document.body.removeChild(ta);
      }
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Both paths failed — nothing we can do.
    }
  }

  return (
    <button className={className} onClick={onCopy} disabled={!value}>
      {copied ? <Check size={14} /> : <Copy size={14} />}
      {copied ? 'Copied' : label}
    </button>
  );
}
